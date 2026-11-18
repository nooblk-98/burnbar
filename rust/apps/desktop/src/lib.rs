//! Tauri shell for BurnBar.
//!
//! This is the only platform-aware crate. Windows, macOS and Linux share the
//! same commands, tray menu and UI; Tauri supplies the native window, tray and
//! notification implementations per platform.

use burnbar_core::alerts::reset_text;
use burnbar_core::config::Config;
use burnbar_core::service::{RefreshReport, Service};
use burnbar_core::usage::{mode_for_providers, providers_for_mode, Provider};
use burnbar_core::{updates, UiState};
use chrono::Utc;
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc;
use std::sync::Mutex;
use std::time::Duration;
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, LogicalSize, Manager, PhysicalPosition, Rect, State, WindowEvent};
use tauri_plugin_autostart::ManagerExt as AutostartExt;
use tauri_plugin_global_shortcut::{Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutState};
use tauri_plugin_notification::NotificationExt;

const MAIN_WINDOW: &str = "main";

pub struct AppState {
    service: Mutex<Service>,
    /// Bumped whenever a popup is shown manually; a stale auto-hide timer is
    /// ignored when the token no longer matches.
    popup_token: AtomicU64,
    dialog_open: AtomicBool,
    tail_top: AtomicBool,
    tray_anchor: Mutex<Option<Rect>>,
    tray_checks: Mutex<HashMap<String, CheckMenuItem<tauri::Wry>>>,
    scheduler_wake: Mutex<Option<mpsc::SyncSender<()>>>,
}

impl AppState {
    fn service(&self) -> Result<std::sync::MutexGuard<'_, Service>, String> {
        self.service
            .lock()
            .map_err(|_| "BurnBar state is unavailable".to_string())
    }
}

#[derive(Clone, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateInfo {
    // The UI receives this with `update-available` and `check_updates`.
    pub current: String,
    pub latest: String,
    pub url: String,
    pub available: bool,
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let args: Vec<String> = std::env::args().collect();
    let demo = args.iter().any(|arg| arg == "--demo")
        || std::env::var("BURNBAR_DEMO")
            .map(|v| v == "1")
            .unwrap_or(false);
    let hidden = args.iter().any(|arg| arg == "--hidden");
    let providers = args
        .iter()
        .position(|arg| arg == "--providers")
        .and_then(|index| args.get(index + 1).cloned())
        .or_else(|| {
            args.iter()
                .find_map(|arg| arg.strip_prefix("--providers=").map(str::to_string))
        });

    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            show_manual_popup(app, None);
        }))
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        show_manual_popup(app, None);
                    }
                })
                .build(),
        )
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            Some(vec!["--hidden"]),
        ))
        .setup(move |app| {
            setup(app, demo, hidden, providers)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            // Closing the window keeps the tray app alive, matching the Go app.
            match event {
                WindowEvent::CloseRequested { api, .. } => {
                    api.prevent_close();
                    let _ = window.hide();
                }
                WindowEvent::Focused(false) => {
                    let state = window.state::<AppState>();
                    let keep_open = state.dialog_open.load(Ordering::SeqCst)
                        || state
                            .service()
                            .map(|service| service.config.always_show_popup)
                            .unwrap_or(false);
                    if !keep_open {
                        let _ = window.hide();
                    }
                }
                _ => {}
            }
        })
        .invoke_handler(tauri::generate_handler![
            get_state,
            refresh_now,
            hide_window,
            restore_compact_popup,
            resize_compact_popup,
            get_popup_tail_top,
            open_external,
        ])
        .run(tauri::generate_context!())
        .expect("error while running BurnBar");
}

fn setup(
    app: &mut tauri::App,
    demo: bool,
    hidden: bool,
    providers: Option<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    let mut service = if demo {
        Service::demo(std::env::temp_dir().join("burnbar-demo"))
    } else {
        Service::new(Config::dir())
    };
    // `--providers claude|codex|both` mirrors the Go development flag. It only
    // affects this process and is never written back to config.json.
    if let Some(mode) = providers {
        if burnbar_core::usage::valid_mode(&mode) {
            service.config.providers = mode;
        } else {
            eprintln!("ignoring --providers {mode}; use claude, codex or both");
        }
    }
    let start_with_windows = service.config.start_with_windows.unwrap_or(true);
    let hotkey = service.config.hotkey;
    let check_updates = service.config.check_updates;
    let first_run = service.first_run();

    app.manage(AppState {
        service: Mutex::new(service),
        popup_token: AtomicU64::new(0),
        dialog_open: AtomicBool::new(false),
        tail_top: AtomicBool::new(false),
        tray_anchor: Mutex::new(None),
        tray_checks: Mutex::new(HashMap::new()),
        scheduler_wake: Mutex::new(None),
    });

    build_tray(app)?;
    if !demo {
        apply_autostart(app.handle(), start_with_windows);
    }
    apply_hotkey(app.handle(), hotkey);

    #[cfg(target_os = "macos")]
    {
        let _ = app.notification().request_permission();
    }

    let handle = app.handle().clone();
    std::thread::spawn(move || refresh(&handle));
    start_scheduler(app.handle().clone());
    if check_updates && !demo {
        start_update_check(app.handle().clone());
    }

    if !hidden || first_run {
        show_manual_popup(app.handle(), None);
    }
    Ok(())
}

fn tray_check(
    app: &tauri::App,
    checks: &mut HashMap<String, CheckMenuItem<tauri::Wry>>,
    id: &str,
    label: &str,
    checked: bool,
) -> Result<CheckMenuItem<tauri::Wry>, tauri::Error> {
    let item = CheckMenuItem::with_id(app, id, label, true, checked, None::<&str>)?;
    checks.insert(id.to_string(), item.clone());
    Ok(item)
}

fn build_tray(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let config = app.state::<AppState>().service()?.config.clone();
    let mut checks = HashMap::new();
    let show = MenuItem::with_id(app, "show", "Show usage", true, None::<&str>)?;
    let refresh_item = MenuItem::with_id(app, "refresh", "Refresh now", true, None::<&str>)?;

    let enabled_providers = config.providers();
    let provider_claude = tray_check(
        app,
        &mut checks,
        "provider:claude",
        "Claude",
        enabled_providers.contains(&Provider::Claude),
    )?;
    let provider_codex = tray_check(
        app,
        &mut checks,
        "provider:codex",
        "Codex",
        enabled_providers.contains(&Provider::Codex),
    )?;
    let provider_opencode = tray_check(
        app,
        &mut checks,
        "provider:opencode",
        "OpenCode",
        enabled_providers.contains(&Provider::Opencode),
    )?;
    let provider_cursor = tray_check(
        app,
        &mut checks,
        "provider:cursor",
        "Cursor",
        enabled_providers.contains(&Provider::Cursor),
    )?;
    let providers = Submenu::with_items(
        app,
        "Providers",
        true,
        &[
            &provider_claude,
            &provider_codex,
            &provider_opencode,
            &provider_cursor,
        ],
    )?;

    let refresh_1 = tray_check(
        app,
        &mut checks,
        "refresh-rate:1",
        "Every minute",
        config.interval_minutes == 1,
    )?;
    let refresh_3 = tray_check(
        app,
        &mut checks,
        "refresh-rate:3",
        "Every 3 minutes",
        config.interval_minutes == 3,
    )?;
    let refresh_5 = tray_check(
        app,
        &mut checks,
        "refresh-rate:5",
        "Every 5 minutes",
        config.interval_minutes == 5,
    )?;
    let refresh_10 = tray_check(
        app,
        &mut checks,
        "refresh-rate:10",
        "Every 10 minutes",
        config.interval_minutes == 10,
    )?;
    let refresh_15 = tray_check(
        app,
        &mut checks,
        "refresh-rate:15",
        "Every 15 minutes",
        config.interval_minutes == 15,
    )?;
    let refresh_30 = tray_check(
        app,
        &mut checks,
        "refresh-rate:30",
        "Every 30 minutes",
        config.interval_minutes == 30,
    )?;
    let refresh_rate = Submenu::with_items(
        app,
        "Refresh interval",
        true,
        &[
            &refresh_1,
            &refresh_3,
            &refresh_5,
            &refresh_10,
            &refresh_15,
            &refresh_30,
        ],
    )?;

    let delay_3 = tray_check(
        app,
        &mut checks,
        "popup-delay:3",
        "3 seconds",
        config.popup_seconds == 3,
    )?;
    let delay_5 = tray_check(
        app,
        &mut checks,
        "popup-delay:5",
        "5 seconds",
        config.popup_seconds == 5,
    )?;
    let delay_6 = tray_check(
        app,
        &mut checks,
        "popup-delay:6",
        "6 seconds",
        config.popup_seconds == 6,
    )?;
    let delay_10 = tray_check(
        app,
        &mut checks,
        "popup-delay:10",
        "10 seconds",
        config.popup_seconds == 10,
    )?;
    let delay_15 = tray_check(
        app,
        &mut checks,
        "popup-delay:15",
        "15 seconds",
        config.popup_seconds == 15,
    )?;
    let delay_30 = tray_check(
        app,
        &mut checks,
        "popup-delay:30",
        "30 seconds",
        config.popup_seconds == 30,
    )?;
    let delay_60 = tray_check(
        app,
        &mut checks,
        "popup-delay:60",
        "1 minute",
        config.popup_seconds == 60,
    )?;
    let popup_delay = Submenu::with_items(
        app,
        "Popup duration",
        true,
        &[
            &delay_3, &delay_5, &delay_6, &delay_10, &delay_15, &delay_30, &delay_60,
        ],
    )?;

    let thresholds = threshold_preset(&config);
    let threshold_decades = tray_check(
        app,
        &mut checks,
        "thresholds:decades",
        "Every 10%",
        thresholds == Some("decades"),
    )?;
    let threshold_quarters = tray_check(
        app,
        &mut checks,
        "thresholds:quarters",
        "Every 25%",
        thresholds == Some("quarters"),
    )?;
    let threshold_critical = tray_check(
        app,
        &mut checks,
        "thresholds:critical",
        "50%, 80%, 95%",
        thresholds == Some("critical"),
    )?;
    let threshold_off = tray_check(
        app,
        &mut checks,
        "thresholds:off",
        "Off",
        thresholds == Some("off"),
    )?;
    let threshold_menu = Submenu::with_items(
        app,
        "Milestone levels",
        true,
        &[
            &threshold_decades,
            &threshold_quarters,
            &threshold_critical,
            &threshold_off,
        ],
    )?;

    let milestones = tray_check(
        app,
        &mut checks,
        "toggle:milestones",
        "Automatic milestone popups",
        config.milestone_popups,
    )?;
    let alerts = tray_check(
        app,
        &mut checks,
        "toggle:alerts",
        "80% / 95% and reset alerts",
        config.alerts,
    )?;
    let native = tray_check(
        app,
        &mut checks,
        "toggle:native",
        "Use native milestone notifications",
        config.native_notices,
    )?;
    let always_show = tray_check(
        app,
        &mut checks,
        "toggle:always-show",
        "Keep popup open",
        config.always_show_popup,
    )?;
    let notification_menu = Submenu::with_items(
        app,
        "Notifications",
        true,
        &[
            &milestones,
            &alerts,
            &native,
            &always_show,
            &PredefinedMenuItem::separator(app)?,
            &threshold_menu,
        ],
    )?;

    let theme_system = tray_check(
        app,
        &mut checks,
        "theme:system",
        "Follow system",
        config.theme == "system",
    )?;
    let theme_light = tray_check(
        app,
        &mut checks,
        "theme:light",
        "Light",
        config.theme == "light",
    )?;
    let theme_dark = tray_check(
        app,
        &mut checks,
        "theme:dark",
        "Dark",
        config.theme == "dark",
    )?;
    let theme = Submenu::with_items(
        app,
        "Theme",
        true,
        &[&theme_system, &theme_light, &theme_dark],
    )?;
    let text_100 = tray_check(
        app,
        &mut checks,
        "text-size:100",
        "100%",
        config.text_scale == 100,
    )?;
    let text_125 = tray_check(
        app,
        &mut checks,
        "text-size:125",
        "125%",
        config.text_scale == 125,
    )?;
    let text_150 = tray_check(
        app,
        &mut checks,
        "text-size:150",
        "150%",
        config.text_scale == 150,
    )?;
    let text_size =
        Submenu::with_items(app, "Text size", true, &[&text_100, &text_125, &text_150])?;
    let appearance = Submenu::with_items(app, "Appearance", true, &[&theme, &text_size])?;

    let quiet_off = tray_check(app, &mut checks, "quiet:off", "Off", !config.quiet_hours)?;
    let quiet_22 = tray_check(
        app,
        &mut checks,
        "quiet:22-7",
        "10 PM – 7 AM",
        config.quiet_hours && config.quiet_start == 1320 && config.quiet_end == 420,
    )?;
    let quiet_23 = tray_check(
        app,
        &mut checks,
        "quiet:23-7",
        "11 PM – 7 AM",
        config.quiet_hours && config.quiet_start == 1380 && config.quiet_end == 420,
    )?;
    let quiet_00 = tray_check(
        app,
        &mut checks,
        "quiet:0-8",
        "Midnight – 8 AM",
        config.quiet_hours && config.quiet_start == 0 && config.quiet_end == 480,
    )?;
    let quiet = Submenu::with_items(
        app,
        "Quiet hours",
        true,
        &[&quiet_off, &quiet_22, &quiet_23, &quiet_00],
    )?;

    let snooze_30 = MenuItem::with_id(app, "snooze:30", "30 minutes", true, None::<&str>)?;
    let snooze_60 = MenuItem::with_id(app, "snooze:60", "1 hour", true, None::<&str>)?;
    let snooze_clear = MenuItem::with_id(app, "snooze:clear", "Clear snooze", true, None::<&str>)?;
    let snooze = Submenu::with_items(
        app,
        "Snooze alerts",
        true,
        &[&snooze_30, &snooze_60, &snooze_clear],
    )?;

    let startup = tray_check(
        app,
        &mut checks,
        "toggle:startup",
        "Start when I sign in",
        config.start_with_windows.unwrap_or(true),
    )?;
    let hotkey = tray_check(
        app,
        &mut checks,
        "toggle:hotkey",
        "Enable Ctrl+Alt+B",
        config.hotkey,
    )?;
    let update_checks = tray_check(
        app,
        &mut checks,
        "toggle:update-checks",
        "Check for updates automatically",
        config.check_updates,
    )?;
    let check_updates_now = MenuItem::with_id(
        app,
        "check-updates",
        "Check for updates now",
        true,
        None::<&str>,
    )?;
    let preferences = Submenu::with_items(
        app,
        "Startup",
        true,
        &[
            &startup,
            &hotkey,
            &update_checks,
            &PredefinedMenuItem::separator(app)?,
            &check_updates_now,
        ],
    )?;

    let test_notification = MenuItem::with_id(
        app,
        "test-notification",
        "Send test notification",
        true,
        None::<&str>,
    )?;
    let about = MenuItem::with_id(app, "about", "About BurnBar", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit BurnBar", true, None::<&str>)?;
    let menu = Menu::with_items(
        app,
        &[
            &show,
            &refresh_item,
            &PredefinedMenuItem::separator(app)?,
            &providers,
            &refresh_rate,
            &popup_delay,
            &notification_menu,
            &appearance,
            &quiet,
            &snooze,
            &preferences,
            &test_notification,
            &about,
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )?;

    let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/32x32.png"))?;
    TrayIconBuilder::with_id("main")
        .icon(icon)
        .tooltip("BurnBar — Claude, Codex, OpenCode and Cursor usage")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| handle_tray_menu(app, event.id.as_ref()))
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                rect,
                ..
            } = event
            {
                show_manual_popup(tray.app_handle(), Some(rect));
            }
        })
        .build(app)?;

    *app.state::<AppState>()
        .tray_checks
        .lock()
        .map_err(|_| "tray state unavailable")? = checks;
    Ok(())
}

fn threshold_preset(config: &Config) -> Option<&'static str> {
    if config.primary_thresholds != config.secondary_thresholds {
        return None;
    }
    match config.primary_thresholds.as_slice() {
        [10, 20, 30, 40, 50, 60, 70, 80, 90, 100] => Some("decades"),
        [25, 50, 75, 100] => Some("quarters"),
        [50, 80, 95] => Some("critical"),
        [] => Some("off"),
        _ => None,
    }
}

fn tray_item_checked(id: &str, config: &Config) -> bool {
    match id {
        id if id.starts_with("provider:") => Provider::from_key(&id[9..])
            .is_some_and(|provider| config.providers().contains(&provider)),
        id if id.starts_with("refresh-rate:") => id[13..].parse() == Ok(config.interval_minutes),
        id if id.starts_with("popup-delay:") => id[12..].parse() == Ok(config.popup_seconds),
        id if id.starts_with("thresholds:") => threshold_preset(config) == Some(&id[11..]),
        id if id.starts_with("theme:") => id[6..] == config.theme,
        id if id.starts_with("text-size:") => id[10..].parse() == Ok(config.text_scale),
        "quiet:off" => !config.quiet_hours,
        "quiet:22-7" => config.quiet_hours && config.quiet_start == 1320 && config.quiet_end == 420,
        "quiet:23-7" => config.quiet_hours && config.quiet_start == 1380 && config.quiet_end == 420,
        "quiet:0-8" => config.quiet_hours && config.quiet_start == 0 && config.quiet_end == 480,
        "toggle:milestones" => config.milestone_popups,
        "toggle:alerts" => config.alerts,
        "toggle:native" => config.native_notices,
        "toggle:always-show" => config.always_show_popup,
        "toggle:startup" => config.start_with_windows.unwrap_or(true),
        "toggle:hotkey" => config.hotkey,
        "toggle:update-checks" => config.check_updates,
        _ => false,
    }
}

fn toggled_provider_mode(mode: &str, provider: Provider) -> Result<String, String> {
    let mut providers = providers_for_mode(mode);
    if providers.contains(&provider) {
        if providers.len() == 1 {
            return Err("At least one provider must stay enabled".into());
        }
        providers.retain(|enabled| *enabled != provider);
    } else {
        providers.push(provider);
    }
    mode_for_providers(&providers).ok_or_else(|| "At least one provider must stay enabled".into())
}

fn sync_tray_checks(app: &AppHandle) {
    let state = app.state::<AppState>();
    let config = match state.service() {
        Ok(service) => service.config.clone(),
        Err(_) => return,
    };
    if let Ok(items) = state.tray_checks.lock() {
        for (id, item) in items.iter() {
            let _ = item.set_checked(tray_item_checked(id, &config));
        }
    };
}

fn apply_tray_setting(app: &AppHandle, id: &str) -> Result<(), String> {
    let (snapshot, old_autostart, old_hotkey, providers_changed) = {
        let state = app.state::<AppState>();
        let mut service = state.service()?;
        let old = service.settings();
        let mut settings = old.clone();
        match id {
            id if id.starts_with("provider:") => {
                let provider =
                    Provider::from_key(&id[9..]).ok_or_else(|| "Unknown provider".to_string())?;
                settings.providers = toggled_provider_mode(&old.providers, provider)?;
            }
            id if id.starts_with("refresh-rate:") => {
                settings.interval_minutes =
                    id[13..].parse().map_err(|_| "invalid refresh interval")?
            }
            id if id.starts_with("popup-delay:") => {
                settings.popup_seconds = id[12..].parse().map_err(|_| "invalid popup duration")?
            }
            "thresholds:decades" => {
                settings.primary_thresholds = (1..=10).map(|n| n * 10).collect()
            }
            "thresholds:quarters" => settings.primary_thresholds = vec![25, 50, 75, 100],
            "thresholds:critical" => settings.primary_thresholds = vec![50, 80, 95],
            "thresholds:off" => settings.primary_thresholds.clear(),
            id if id.starts_with("theme:") => settings.theme = id[6..].to_string(),
            id if id.starts_with("text-size:") => {
                settings.text_scale = id[10..].parse().map_err(|_| "invalid text size")?
            }
            "quiet:off" => settings.quiet_hours = false,
            "quiet:22-7" => {
                settings.quiet_hours = true;
                settings.quiet_start = 1320;
                settings.quiet_end = 420;
            }
            "quiet:23-7" => {
                settings.quiet_hours = true;
                settings.quiet_start = 1380;
                settings.quiet_end = 420;
            }
            "quiet:0-8" => {
                settings.quiet_hours = true;
                settings.quiet_start = 0;
                settings.quiet_end = 480;
            }
            "toggle:milestones" => settings.milestone_popups = !old.milestone_popups,
            "toggle:alerts" => settings.alerts = !old.alerts,
            "toggle:native" => settings.native_notices = !old.native_notices,
            "toggle:always-show" => settings.always_show_popup = !old.always_show_popup,
            "toggle:startup" => settings.start_with_windows = !old.start_with_windows,
            "toggle:hotkey" => settings.hotkey = !old.hotkey,
            "toggle:update-checks" => settings.check_updates = !old.check_updates,
            _ => return Err(format!("unknown tray setting: {id}")),
        }
        if id.starts_with("thresholds:") {
            settings.secondary_thresholds = settings.primary_thresholds.clone();
        }
        let providers_changed = settings.providers != old.providers;
        service.apply_settings(settings)?;
        (
            service.snapshot(),
            old.start_with_windows,
            old.hotkey,
            providers_changed,
        )
    };
    if snapshot.settings.start_with_windows != old_autostart {
        apply_autostart(app, snapshot.settings.start_with_windows);
    }
    if snapshot.settings.hotkey != old_hotkey {
        apply_hotkey(app, snapshot.settings.hotkey);
    }
    sync_tray_checks(app);
    wake_scheduler(app);
    let _ = app.emit("state-changed", &snapshot);
    if providers_changed {
        let handle = app.clone();
        std::thread::spawn(move || refresh(&handle));
    }
    Ok(())
}

fn notify(app: &AppHandle, title: &str, body: &str) {
    let _ = app.notification().builder().title(title).body(body).show();
}

fn check_updates_from_tray(app: AppHandle) {
    std::thread::spawn(move || match updates::check() {
        Ok(release) => {
            let current = burnbar_core::version();
            if updates::newer(current, &release.version) {
                notify(
                    &app,
                    "BurnBar update available",
                    &format!("Version {} is available.", release.version),
                );
            } else {
                notify(
                    &app,
                    "BurnBar is up to date",
                    &format!("You are using version {current}."),
                );
            }
        }
        Err(err) => notify(&app, "Update check failed", &err),
    });
}

fn handle_tray_menu(app: &AppHandle, id: &str) {
    match id {
        "show" => show_manual_popup(app, None),
        "refresh" => {
            let handle = app.clone();
            std::thread::spawn(move || refresh(&handle));
        }
        "about" => show_dialog_window(app, "open-about", 380.0, 360.0),
        "check-updates" => check_updates_from_tray(app.clone()),
        "test-notification" => notify(
            app,
            "BurnBar test notification",
            "Notifications are working.",
        ),
        "snooze:30" | "snooze:60" => {
            let minutes = id[7..].parse().unwrap_or(30);
            if let Ok(mut service) = app.state::<AppState>().service() {
                let _ = service.snooze(minutes);
            }
            notify(
                app,
                "BurnBar alerts snoozed",
                &format!("Alerts are paused for {minutes} minutes."),
            );
        }
        "snooze:clear" => {
            if let Ok(mut service) = app.state::<AppState>().service() {
                let _ = service.clear_snooze();
            }
            notify(
                app,
                "BurnBar snooze cleared",
                "Usage alerts are active again.",
            );
        }
        "quit" => app.exit(0),
        _ => {
            if let Err(err) = apply_tray_setting(app, id) {
                eprintln!("could not update {id}: {err}");
                sync_tray_checks(app);
                notify(app, "BurnBar settings", &err);
            }
        }
    }
}

fn apply_autostart(app: &AppHandle, enabled: bool) {
    let autolaunch = app.autolaunch();
    let result = if enabled {
        autolaunch.enable()
    } else {
        autolaunch.disable()
    };
    if let Err(err) = result {
        // Headless CI and some Linux sessions have no autostart backend; the
        // setting still persists and is retried on the next launch.
        eprintln!("autostart could not be updated: {err}");
    }
}

fn apply_hotkey(app: &AppHandle, enabled: bool) {
    let shortcuts = app.global_shortcut();
    let _ = shortcuts.unregister_all();
    if !enabled {
        return;
    }
    let shortcut = Shortcut::new(Some(Modifiers::CONTROL | Modifiers::ALT), Code::KeyB);
    if let Err(err) = shortcuts.register(shortcut) {
        // Another application may already own the combination.
        eprintln!("Ctrl+Alt+B could not be registered: {err}");
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct PopupPlacement {
    x: i32,
    y: i32,
    tail_top: bool,
}

fn place_popup(
    work: (i32, i32, u32, u32),
    anchor: (f64, f64, u32, u32),
    popup: (u32, u32),
    gap: i32,
) -> PopupPlacement {
    let (work_x, work_y, work_width, work_height) = work;
    let (popup_width, popup_height) = popup;
    let (anchor_x, anchor_y, anchor_width, anchor_height) = anchor;
    let anchor_center_x = anchor_x.round() as i32 + anchor_width as i32 / 2;
    let anchor_top = anchor_y.round() as i32;
    let anchor_bottom = anchor_top + anchor_height as i32;
    let max_x = work_x + work_width as i32 - popup_width as i32;
    let max_y = work_y + work_height as i32 - popup_height as i32;
    let x = (anchor_center_x - popup_width as i32 / 2).clamp(work_x, max_x.max(work_x));
    let tail_top = anchor_top <= work_y + work_height as i32 / 2;
    let preferred_y = if tail_top {
        anchor_bottom + gap
    } else {
        anchor_top - popup_height as i32 - gap
    };
    PopupPlacement {
        x,
        y: preferred_y.clamp(work_y, max_y.max(work_y)),
        tail_top,
    }
}

fn compact_size(app: &AppHandle) -> (f64, f64) {
    let (providers, scale) = app
        .state::<AppState>()
        .service()
        .map(|service| {
            (
                burnbar_core::usage::providers_for_mode(&service.config.providers).len(),
                (service.config.text_scale as f64 / 100.0).clamp(1.0, 1.5),
            )
        })
        .unwrap_or((2, 1.0));
    let providers = providers.clamp(1, 4);
    (340.0 * scale, (58.0 + providers as f64 * 106.0) * scale)
}

fn position_compact_window(app: &AppHandle, anchor: Option<Rect>, width: f64, height: f64) {
    let Some(window) = app.get_webview_window(MAIN_WINDOW) else {
        return;
    };
    let state = app.state::<AppState>();
    if let Some(anchor) = anchor {
        if let Ok(mut stored) = state.tray_anchor.lock() {
            *stored = Some(anchor);
        }
    }
    let anchor = anchor.or_else(|| state.tray_anchor.lock().ok().and_then(|stored| *stored));
    let _ = window.set_size(LogicalSize::new(width, height));

    let monitors = window.available_monitors().unwrap_or_default();
    let physical_anchor = anchor.map(|anchor| {
        let position = anchor.position.to_physical::<f64>(1.0);
        let size = anchor.size.to_physical::<u32>(1.0);
        (position.x, position.y, size.width, size.height)
    });
    let monitor = physical_anchor
        .and_then(|(x, y, width, height)| {
            let center_x = x + width as f64 / 2.0;
            let center_y = y + height as f64 / 2.0;
            monitors.iter().find(|monitor| {
                let position = monitor.position();
                let size = monitor.size();
                center_x >= position.x as f64
                    && center_x < (position.x + size.width as i32) as f64
                    && center_y >= position.y as f64
                    && center_y < (position.y + size.height as i32) as f64
            })
        })
        .or_else(|| monitors.first());
    let Some(monitor) = monitor else {
        let _ = window.center();
        return;
    };
    let scale = monitor.scale_factor();
    let popup = (
        (width * scale).round() as u32,
        (height * scale).round() as u32,
    );
    let work = monitor.work_area();
    let fallback = (
        (work.position.x + work.size.width as i32 - 24) as f64,
        (work.position.y + work.size.height as i32) as f64,
        24,
        0,
    );
    let placement = place_popup(
        (
            work.position.x,
            work.position.y,
            work.size.width,
            work.size.height,
        ),
        physical_anchor.unwrap_or(fallback),
        popup,
        (8.0 * scale).round() as i32,
    );
    state.tail_top.store(placement.tail_top, Ordering::SeqCst);
    let _ = app.emit("popup-side", placement.tail_top);
    let _ = window.set_position(PhysicalPosition::new(placement.x, placement.y));
}

fn show_compact_window(app: &AppHandle, anchor: Option<Rect>) -> u64 {
    let state = app.state::<AppState>();
    state.dialog_open.store(false, Ordering::SeqCst);
    let token = state.popup_token.fetch_add(1, Ordering::SeqCst) + 1;
    let (width, height) = compact_size(app);
    position_compact_window(app, anchor, width, height);
    let _ = app.emit("popup-mode", "compact");
    if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
    token
}

fn show_popup_at(app: &AppHandle, seconds: i32, anchor: Option<Rect>) {
    let token = show_compact_window(app, anchor);
    let handle = app.clone();
    let seconds = seconds.clamp(1, 300) as u64;
    std::thread::spawn(move || {
        std::thread::sleep(Duration::from_secs(seconds));
        let state = handle.state::<AppState>();
        if state.popup_token.load(Ordering::SeqCst) != token {
            return; // a manual interaction restarted the lifetime
        }
        let always_show = state
            .service()
            .map(|service| service.config.always_show_popup)
            .unwrap_or(false);
        if !always_show {
            if let Some(window) = handle.get_webview_window(MAIN_WINDOW) {
                let _ = window.hide();
            }
        }
    });
}

fn show_popup(app: &AppHandle, seconds: i32) {
    show_popup_at(app, seconds, None);
}

fn show_manual_popup(app: &AppHandle, anchor: Option<Rect>) {
    let seconds = app
        .state::<AppState>()
        .service()
        .map(|service| service.config.popup_seconds)
        .unwrap_or(6);
    show_popup_at(app, seconds, anchor);
}

fn show_dialog_window(app: &AppHandle, event: &str, width: f64, height: f64) {
    let state = app.state::<AppState>();
    state.dialog_open.store(true, Ordering::SeqCst);
    state.popup_token.fetch_add(1, Ordering::SeqCst);
    if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
        let _ = window.set_size(LogicalSize::new(width, height));
        let _ = window.center();
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
    let _ = app.emit("popup-mode", "dialog");
    let _ = app.emit(event, ());
}

fn emit_state(app: &AppHandle) {
    let state = app.state::<AppState>();
    let snapshot = match state.service() {
        Ok(service) => service.snapshot(),
        Err(_) => return,
    };
    let _ = app.emit("state-changed", snapshot);
}

fn refresh(app: &AppHandle) {
    let report: RefreshReport = {
        let state = app.state::<AppState>();
        let result = match state.service() {
            Ok(mut service) => service.refresh(),
            Err(err) => {
                eprintln!("{err}");
                return;
            }
        };
        result
    };

    let (quiet, milestones_enabled, always_show, native_notices, popup_seconds, alerts_enabled) = {
        let state = app.state::<AppState>();
        let values = match state.service() {
            Ok(service) => {
                let config = &service.config;
                (
                    config.quiet_now(),
                    config.milestone_popups,
                    config.always_show_popup,
                    config.native_notices,
                    config.popup_seconds,
                    config.alerts,
                )
            }
            Err(_) => (true, false, false, false, 6, false),
        };
        values
    };

    if alerts_enabled && !quiet {
        for notice in &report.alerts {
            let _ = app
                .notification()
                .builder()
                .title(&notice.title)
                .body(&notice.body)
                .show();
        }
    }

    if milestones_enabled && !always_show && !quiet && !report.milestones.is_empty() {
        let now = Utc::now();
        let lines: Vec<String> = report
            .milestones
            .iter()
            .map(|event| {
                format!(
                    "{} {}: {:.0}% used ({}% crossed). {}",
                    event.provider.name(),
                    event.label,
                    event.percent,
                    event.level,
                    reset_text(event.resets_at, now)
                )
            })
            .collect();
        if native_notices {
            let _ = app
                .notification()
                .builder()
                .title("BurnBar usage milestone")
                .body(lines.join("\n"))
                .show();
        } else {
            show_popup(app, popup_seconds);
        }
    }

    emit_state(app);
}

fn start_scheduler(app: AppHandle) {
    let (tx, rx) = mpsc::sync_channel(1);
    if let Ok(mut wake) = app.state::<AppState>().scheduler_wake.lock() {
        *wake = Some(tx);
    }
    std::thread::spawn(move || loop {
        let minutes = app
            .state::<AppState>()
            .service()
            .map(|service| service.config.interval_minutes.max(1) as u64)
            .unwrap_or(3);
        match rx.recv_timeout(Duration::from_secs(minutes * 60)) {
            Err(mpsc::RecvTimeoutError::Timeout) => refresh(&app),
            Ok(()) => continue,
            Err(mpsc::RecvTimeoutError::Disconnected) => break,
        }
    });
}

fn wake_scheduler(app: &AppHandle) {
    let state = app.state::<AppState>();
    if let Ok(wake) = state.scheduler_wake.lock() {
        if let Some(tx) = wake.as_ref() {
            let _ = tx.try_send(());
        }
    };
}

fn start_update_check(app: AppHandle) {
    std::thread::spawn(move || {
        let release = match updates::check() {
            Ok(release) => release,
            Err(err) => {
                eprintln!("update check failed: {err}");
                return;
            }
        };
        let current = burnbar_core::version().to_string();
        if !updates::newer(&current, &release.version) {
            return;
        }
        let info = UpdateInfo {
            current,
            latest: release.version.clone(),
            url: release.url,
            available: true,
        };
        let _ = app
            .notification()
            .builder()
            .title("BurnBar update available")
            .body(format!(
                "Version {} is available. Open the tray menu for options.",
                release.version
            ))
            .show();
        let _ = app.emit("update-available", info);
    });
}

#[tauri::command]
fn get_state(state: State<'_, AppState>) -> Result<UiState, String> {
    Ok(state.service()?.snapshot())
}

#[tauri::command]
fn refresh_now(app: AppHandle) -> Result<(), String> {
    std::thread::spawn(move || refresh(&app));
    Ok(())
}

#[tauri::command]
fn hide_window(app: AppHandle) {
    if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
        let _ = window.hide();
    }
}

#[tauri::command]
fn restore_compact_popup(app: AppHandle) {
    show_manual_popup(&app, None);
}

#[tauri::command]
fn resize_compact_popup(app: AppHandle, height: f64) {
    if app.state::<AppState>().dialog_open.load(Ordering::SeqCst) {
        return;
    }
    let (width, _) = compact_size(&app);
    position_compact_window(&app, None, width, height.clamp(96.0, 720.0));
}

#[tauri::command]
fn get_popup_tail_top(state: State<'_, AppState>) -> bool {
    state.tail_top.load(Ordering::SeqCst)
}

#[tauri::command]
fn open_external(url: String) -> Result<(), String> {
    let allowed = url.starts_with("https://github.com/nooblk-98/burnbar")
        || url.starts_with("https://www.npmjs.com/package/burnbar");
    if !allowed {
        return Err("that link is not part of BurnBar".into());
    }
    tauri_plugin_opener::open_url(url, None::<&str>).map_err(|e| e.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn update_info_marks_a_newer_release() {
        let info = UpdateInfo {
            current: "0.4.1".into(),
            latest: "0.5.0".into(),
            url: updates::RELEASES.into(),
            available: updates::newer("0.4.1", "0.5.0"),
        };
        assert!(info.available);
        assert!(!updates::newer("0.5.0", "0.5.0"));
    }

    #[test]
    fn milestone_message_matches_the_go_wording() {
        let now = Utc::now();
        let text = format!(
            "Claude 5h: {:.0}% used ({}% crossed). {}",
            62.4,
            60,
            reset_text(Some(now + chrono::Duration::minutes(42)), now)
        );
        assert!(text.contains("62% used (60% crossed)"));
        assert!(text.contains("Resets in 42m"));
    }

    #[test]
    fn settings_dto_matches_the_web_ui_contract() {
        let settings = burnbar_core::service::Settings::from(&Config::default());
        let json = serde_json::to_value(&settings).unwrap();
        for key in [
            "popupSeconds",
            "milestonePopups",
            "primaryThresholds",
            "quietStart",
            "startWithWindows",
            "textScale",
        ] {
            assert!(json.get(key).is_some(), "missing {key} in {json}");
        }
        let back: burnbar_core::service::Settings = serde_json::from_value(json).unwrap();
        assert_eq!(back, settings);
    }

    #[test]
    fn native_tray_marks_exactly_the_saved_choice() {
        let config = Config {
            providers: "claude,opencode".into(),
            interval_minutes: 5,
            theme: "dark".into(),
            ..Config::default()
        };

        assert!(tray_item_checked("provider:claude", &config));
        assert!(tray_item_checked("provider:opencode", &config));
        assert!(!tray_item_checked("provider:codex", &config));
        assert!(!tray_item_checked("provider:cursor", &config));
        assert!(tray_item_checked("refresh-rate:5", &config));
        assert!(!tray_item_checked("refresh-rate:3", &config));
        assert!(tray_item_checked("theme:dark", &config));
        assert!(!tray_item_checked("theme:system", &config));
    }

    #[test]
    fn provider_toggle_supports_arbitrary_combinations() {
        let mode = toggled_provider_mode("both", Provider::Opencode).unwrap();
        assert_eq!(mode, "claude,codex,opencode");
    }

    #[test]
    fn provider_toggle_rejects_disabling_the_last_provider() {
        let result = toggled_provider_mode("cursor", Provider::Cursor);
        assert_eq!(
            result,
            Err("At least one provider must stay enabled".into())
        );
    }

    #[test]
    fn milestone_presets_are_detected_only_when_both_windows_match() {
        let mut config = Config::default();
        assert_eq!(threshold_preset(&config), Some("decades"));
        config.primary_thresholds = vec![50, 80, 95];
        config.secondary_thresholds = config.primary_thresholds.clone();
        assert_eq!(threshold_preset(&config), Some("critical"));
        config.secondary_thresholds = vec![25, 50, 75, 100];
        assert_eq!(threshold_preset(&config), None);
    }

    #[test]
    fn ui_snapshot_uses_the_names_the_ui_reads() {
        let dir = std::env::temp_dir().join(format!("burnbar-dto-{}", std::process::id()));
        let service = Service::demo(dir);
        let json = serde_json::to_value(service.snapshot()).unwrap();
        assert!(json.get("settings").is_some());
        assert!(json.get("providers").is_some());
        let provider = &json["providers"][0];
        assert!(provider.get("history24").is_some());
        assert!(provider.get("primaryForecast").is_some());
        assert!(provider["usage"].get("fiveHour").is_some());
        assert!(provider["usage"].get("updatedAt").is_some());
    }

    #[test]
    fn popup_is_placed_above_a_bottom_tray_and_clamped_to_work_area() {
        let placement = place_popup((0, 0, 1920, 1040), (1900.0, 1040.0, 20, 40), (340, 270), 8);
        assert_eq!(
            placement,
            PopupPlacement {
                x: 1580,
                y: 762,
                tail_top: false,
            }
        );
    }

    #[test]
    fn popup_is_placed_below_a_top_tray() {
        let placement = place_popup(
            (-1920, 0, 1920, 1040),
            (-1000.0, 0.0, 24, 32),
            (340, 270),
            8,
        );
        assert_eq!(placement.y, 40);
        assert!(placement.tail_top);
    }
}
