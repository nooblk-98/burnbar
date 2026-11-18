//! End-to-end tests for the pieces that touch the network and the filesystem.
//!
//! A real one-request HTTP server stands in for the Claude usage API and the
//! GitHub releases API, so these tests exercise request building, status
//! handling, body limits, JSON decoding and the service loop without leaving
//! the machine or needing an account.

use burnbar_core::config::Config;
use burnbar_core::service::Service;
use burnbar_core::usage::{Provider, Window};
use burnbar_core::{claude, cursor, opencode, updates};
use std::io::{Read, Write};
use std::net::TcpListener;
use std::thread;

struct Response {
    status: &'static str,
    headers: &'static str,
    body: String,
}

fn serve_once(response: Response) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind local test server");
    let address = listener.local_addr().unwrap();
    thread::spawn(move || {
        let (mut stream, _) = listener.accept().unwrap();
        let mut request = [0_u8; 4096];
        let _ = stream.read(&mut request);
        let body = response.body;
        let reply = format!(
            "HTTP/1.1 {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\n{}\r\n{}",
            response.status,
            body.len(),
            response.headers,
            body
        );
        let _ = stream.write_all(reply.as_bytes());
        let _ = stream.flush();
    });
    format!("http://{address}/usage")
}

#[test]
fn claude_http_response_is_fetched_and_parsed() {
    let body = r#"{
        "five_hour": {"utilization": 42, "resets_at": "2026-01-02T03:04:05Z"},
        "seven_day": {"utilization": 12, "resets_at": "2026-01-05T03:04:05Z"}
    }"#;
    let url = serve_once(Response {
        status: "200 OK",
        headers: "",
        body: body.into(),
    });
    let raw = claude::fetch_raw_from(&url, "test-token").expect("fetch");
    let usage = claude::parse(&raw);
    assert_eq!(usage.provider, Provider::Claude);
    assert_eq!(usage.err, "");
    assert_eq!(usage.five_hour.unwrap().percent, 42.0);
    assert_eq!(usage.seven_day.unwrap().percent, 12.0);
}

#[test]
fn claude_unauthorized_and_rate_limited_are_explained() {
    let url = serve_once(Response {
        status: "401 Unauthorized",
        headers: "",
        body: "{}".into(),
    });
    let error = claude::fetch_raw_from(&url, "expired").unwrap_err();
    assert!(error.message.contains("run `claude` to sign in again"));

    let url = serve_once(Response {
        status: "429 Too Many Requests",
        headers: "Retry-After: 120\r\n",
        body: "{}".into(),
    });
    let error = claude::fetch_raw_from(&url, "token").unwrap_err();
    assert!(error.rate_limited);
    assert!(error.retry_at.is_some());
}

#[test]
fn opencode_and_cursor_responses_are_fetched_and_parsed() {
    let url = serve_once(Response {
        status: "200 OK",
        headers: "",
        body: r#"{"usage":{
            "rolling":{"status":"ok","percent":7,"resetsAt":"2026-11-18T08:57:48.000Z"},
            "weekly":{"status":"ok","percent":12,"resetsAt":"2026-11-23T00:00:00.000Z"},
            "monthly":{"status":"ok","percent":31,"resetsAt":"2026-12-07T11:05:00.000Z"}
        }}"#
        .into(),
    });
    let raw = opencode::fetch_raw_from(&url, "test-key").expect("opencode fetch");
    let usage = opencode::parse(&raw);
    assert_eq!(usage.provider, Provider::Opencode);
    assert_eq!(usage.err, "");
    assert_eq!(usage.five_hour.unwrap().percent, 7.0);
    assert_eq!(usage.extra[0].percent, 31.0);

    let url = serve_once(Response {
        status: "200 OK",
        headers: "",
        body: r#"{
            "billingCycleStart":"2026-11-01T00:00:00.000Z",
            "billingCycleEnd":"2026-12-01T00:00:00.000Z",
            "planUsage":{"limit":2000,"remaining":900,"totalPercentUsed":55,"autoPercentUsed":40}
        }"#
        .into(),
    });
    let raw = cursor::fetch_raw_from(&url, "test-token").expect("cursor fetch");
    let usage = cursor::parse(&raw);
    assert_eq!(usage.provider, Provider::Cursor);
    assert_eq!(usage.err, "");
    assert_eq!(usage.five_hour.unwrap().percent, 55.0);
    assert_eq!(usage.extra.len(), 1);
}

#[test]
fn all_provider_mode_polls_every_source() {
    let dir = std::env::temp_dir().join(format!(
        "burnbar-all-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    let mut service = Service::new(&dir);
    service.config.providers = "all".into();
    let seen = std::sync::Mutex::new(Vec::new());
    let report = service.refresh_with(|provider| {
        seen.lock().unwrap().push(provider);
        burnbar_core::usage::demo_usage(provider)
    });
    assert_eq!(report.fetched.len(), 4);
    assert_eq!(service.snapshot().providers.len(), 4);
    let mut seen = seen.into_inner().unwrap();
    seen.sort();
    assert_eq!(
        seen,
        vec![
            Provider::Claude,
            Provider::Codex,
            Provider::Opencode,
            Provider::Cursor
        ]
    );
    for provider in Provider::ALL {
        assert!(dir.join(provider.history_file()).exists());
    }
    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn update_check_reads_a_mocked_github_release() {
    let url = serve_once(Response {
        status: "200 OK",
        headers: "",
        body: r#"{
            "tag_name": "v9.8.7",
            "html_url": "https://github.com/nooblk-98/burnbar/releases/tag/v9.8.7"
        }"#
        .into(),
    });
    let release = updates::check_from(&url).expect("release");
    assert_eq!(release.version, "9.8.7");
    assert!(updates::newer(burnbar_core::version(), &release.version));
}

#[test]
fn full_refresh_cycle_survives_a_provider_that_misses_a_fetch() {
    let dir = std::env::temp_dir().join(format!(
        "burnbar-e2e-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    let mut service = Service::new(&dir);
    let now = chrono::Utc::now();
    let claude_calls = std::sync::atomic::AtomicU64::new(0);
    let fetch = |provider: Provider| {
        if provider == Provider::Codex {
            return burnbar_core::usage::Usage::failed(
                Provider::Codex,
                "Codex usage request timed out",
            );
        }
        let call = claude_calls.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        let percent = if call == 0 { 70.0 } else { 81.0 };
        let mut usage = burnbar_core::usage::Usage::new(Provider::Claude);
        usage.updated_at = Some(now);
        usage.five_hour = Some(
            Window::new("session", "Session (5h)", percent)
                .with_reset(Some(now + chrono::Duration::hours(1)))
                .with_length(std::time::Duration::from_secs(5 * 3600)),
        );
        usage
    };
    let first = service.refresh_with(fetch);
    let second = service.refresh_with(fetch);
    let third = service.refresh_with(fetch);
    assert_eq!(first.milestones.len(), 0, "the first reading is a baseline");
    assert!(first.alerts.is_empty());
    assert_eq!(second.milestones.len(), 1, "81% crosses the 80% milestone");
    assert!(second
        .alerts
        .iter()
        .any(|alert| alert.title.contains("at 81%")));
    assert_eq!(
        third.milestones.len(),
        0,
        "the same milestone is not replayed"
    );
    assert!(third.alerts.is_empty(), "the same alert is not repeated");

    let snapshot = service.snapshot();
    let claude = snapshot
        .providers
        .iter()
        .find(|p| p.provider == Provider::Claude)
        .unwrap();
    let codex = snapshot
        .providers
        .iter()
        .find(|p| p.provider == Provider::Codex)
        .unwrap();
    assert_eq!(claude.usage.five_hour.as_ref().unwrap().percent, 81.0);
    assert_eq!(
        claude.usage.five_hour.as_ref().unwrap().label,
        "Session (5h)"
    );
    assert!(!claude.history24.is_empty());
    assert_eq!(codex.health, "Offline / connection error");

    let saved = Config::load_from(&dir);
    assert!(saved.milestone_state.contains_key("claude/session"));
    assert!(saved.alert_state.contains_key("session"));
    let _ = std::fs::remove_dir_all(&dir);
}
