//! Tauri shell for BurnBar.
//!
//! This is the only platform-aware crate. Windows, macOS and Linux share the
//! same commands, tray menu and UI; Tauri supplies the native window, tray and
//! notification implementations per platform.

use burnbar_core::alerts::reset_text;
use burnbar_core::config::Config;
use burnbar_core::service::{RefreshReport, Service, Settings};
use burnbar_core::{updates, UiState};
use chrono::Utc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc;
use std::sync::Mutex;
use std::time::Duration;
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager, State, WindowEvent};
use tauri_plugin_autostart::ManagerExt as AutostartExt;
use tauri_plugin_global_shortcut::{Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutState};
use tauri_plugin_notification::NotificationExt;

const MAIN_WINDOW: &str = "main";

pub struct AppState {
    service: Mutex<Service>,
    /// Bumped whenever a popup is shown manually; a stale auto-hide timer is
    /// ignored when the token no longer matches.
    popup_token: AtomicU64,
    milestones_item: Mutex<Option<CheckMenuItem<tauri::Wry>>>,
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
            show_window(app);
        }))
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        show_window(app);
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
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![
            get_state,
            refresh_now,
            save_settings,
            set_provider_mode,
            toggle_milestones,
            snooze,
            clear_snooze,
            hide_window,
            quit_app,
            check_updates,
            open_external,
            send_test_notification,
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
    let milestones = service.config.milestone_popups;
    let start_with_windows = service.config.start_with_windows.unwrap_or(true);
    let hotkey = service.config.hotkey;
    let check_updates = service.config.check_updates;
    let first_run = service.first_run();

    app.manage(AppState {
        service: Mutex::new(service),
        popup_token: AtomicU64::new(0),
        milestones_item: Mutex::new(None),
        scheduler_wake: Mutex::new(None),
    });

    build_tray(app, milestones)?;
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
        show_window(app.handle());
    }
    Ok(())
}

fn build_tray(app: &mut tauri::App, milestones: bool) -> Result<(), Box<dyn std::error::Error>> {
    let show = MenuItem::with_id(app, "show", "Show usage", true, None::<&str>)?;
    let refresh_item = MenuItem::with_id(app, "refresh", "Refresh now", true, None::<&str>)?;
    let milestones_item = CheckMenuItem::with_id(
        app,
        "milestones",
        "Automatic usage milestones",
        true,
        milestones,
        None::<&str>,
    )?;
    let settings = MenuItem::with_id(app, "settings", "Settings…", true, None::<&str>)?;
    let about = MenuItem::with_id(app, "about", "About BurnBar", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit BurnBar", true, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let menu = Menu::with_items(
        app,
        &[
            &show,
            &refresh_item,
            &separator,
            &milestones_item,
            &settings,
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
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => {
                show_window(app);
            }
            "refresh" => {
                let handle = app.clone();
                std::thread::spawn(move || refresh(&handle));
            }
            "milestones" => toggle_milestones_from_tray(app),
            "settings" => {
                show_window(app);
                let _ = app.emit("open-settings", ());
            }
            "about" => {
                show_window(app);
                let _ = app.emit("open-about", ());
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_window(tray.app_handle());
            }
        })
        .build(app)?;

    let state = app.state::<AppState>();
    *state
        .milestones_item
        .lock()
        .map_err(|_| "tray state unavailable")? = Some(milestones_item);
    Ok(())
}

fn toggle_milestones_from_tray(app: &AppHandle) {
    let state = app.state::<AppState>();
    let enabled = match state.service() {
        Ok(mut service) => match service.toggle_milestones() {
            Ok(enabled) => enabled,
            Err(err) => {
                eprintln!("could not toggle milestones: {err}");
                return;
            }
        },
        Err(err) => {
            eprintln!("{err}");
            return;
        }
    };
    if let Ok(item) = state.milestones_item.lock() {
        if let Some(item) = item.as_ref() {
            let _ = item.set_checked(enabled);
        }
    }
    emit_state(app);
}

fn set_milestone_check(app: &AppHandle, enabled: bool) {
    let state = app.state::<AppState>();
    if let Ok(item) = state.milestones_item.lock() {
        if let Some(item) = item.as_ref() {
            let _ = item.set_checked(enabled);
        }
    };
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

fn show_window(app: &AppHandle) -> u64 {
    let state = app.state::<AppState>();
    let token = state.popup_token.fetch_add(1, Ordering::SeqCst) + 1;
    if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
    token
}

fn show_popup(app: &AppHandle, seconds: i32) {
    let token = show_window(app);
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
                "Version {} is available. Open Settings to see the release notes.",
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
fn save_settings(
    app: AppHandle,
    state: State<'_, AppState>,
    settings: Settings,
) -> Result<UiState, String> {
    let (snapshot, autostart, hotkey, providers_changed, milestones) = {
        let mut service = state.service()?;
        let old_providers = service.config.providers.clone();
        service.apply_settings(settings)?;
        let autostart = service.config.start_with_windows.unwrap_or(true);
        let hotkey = service.config.hotkey;
        let providers_changed = service.config.providers != old_providers;
        let milestones = service.config.milestone_popups;
        (
            service.snapshot(),
            autostart,
            hotkey,
            providers_changed,
            milestones,
        )
    };
    apply_autostart(&app, autostart);
    apply_hotkey(&app, hotkey);
    set_milestone_check(&app, milestones);
    wake_scheduler(&app);
    let _ = app.emit("state-changed", &snapshot);
    if providers_changed {
        let handle = app.clone();
        std::thread::spawn(move || refresh(&handle));
    }
    Ok(snapshot)
}

#[tauri::command]
fn set_provider_mode(
    app: AppHandle,
    state: State<'_, AppState>,
    mode: String,
) -> Result<UiState, String> {
    let snapshot = {
        let mut service = state.service()?;
        service.set_provider_mode(&mode)?;
        service.snapshot()
    };
    let _ = app.emit("state-changed", &snapshot);
    wake_scheduler(&app);
    let handle = app.clone();
    std::thread::spawn(move || refresh(&handle));
    Ok(snapshot)
}

#[tauri::command]
fn toggle_milestones(app: AppHandle, state: State<'_, AppState>) -> Result<UiState, String> {
    let snapshot = {
        let mut service = state.service()?;
        let enabled = service.toggle_milestones()?;
        set_milestone_check(&app, enabled);
        service.snapshot()
    };
    let _ = app.emit("state-changed", &snapshot);
    Ok(snapshot)
}

#[tauri::command]
fn snooze(app: AppHandle, state: State<'_, AppState>, minutes: i64) -> Result<UiState, String> {
    let snapshot = {
        let mut service = state.service()?;
        service.snooze(minutes)?;
        service.snapshot()
    };
    let _ = app.emit("state-changed", &snapshot);
    Ok(snapshot)
}

#[tauri::command]
fn clear_snooze(app: AppHandle, state: State<'_, AppState>) -> Result<UiState, String> {
    let snapshot = {
        let mut service = state.service()?;
        service.clear_snooze()?;
        service.snapshot()
    };
    let _ = app.emit("state-changed", &snapshot);
    Ok(snapshot)
}

#[tauri::command]
fn hide_window(app: AppHandle) {
    if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
        let _ = window.hide();
    }
}

#[tauri::command]
fn quit_app(app: AppHandle) {
    app.exit(0);
}

#[tauri::command]
fn check_updates(state: State<'_, AppState>) -> Result<UpdateInfo, String> {
    let current = state.service()?.snapshot().version;
    let release = updates::check()?;
    Ok(UpdateInfo {
        available: updates::newer(&current, &release.version),
        current,
        latest: release.version,
        url: release.url,
    })
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

#[tauri::command]
fn send_test_notification(app: AppHandle) -> Result<(), String> {
    let body = format!(
        "This is what a milestone looks like. {}",
        reset_text(Some(Utc::now() + chrono::Duration::hours(1)), Utc::now())
    );
    app.notification()
        .builder()
        .title("BurnBar test notification")
        .body(body)
        .show()
        .map_err(|e| e.to_string())
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
        let settings = Settings::from(&Config::default());
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
        let back: Settings = serde_json::from_value(json).unwrap();
        assert_eq!(back, settings);
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
}
