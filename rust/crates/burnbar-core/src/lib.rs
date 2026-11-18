//! Portable BurnBar core.
//!
//! Everything in this crate is platform neutral: the same code runs on
//! Windows, macOS and Linux. The Tauri shell in `apps/desktop` only supplies a
//! window, a tray icon and notifications.
//!
//! Module layout mirrors the original Go packages so behaviour can be compared
//! one unit at a time (see `rust/MIGRATION.md`):
//!
//! | Go package            | Rust module      |
//! | --------------------- | ---------------- |
//! | `internal/usage`      | [`usage`]        |
//! | `internal/config`     | [`config`]       |
//! | `internal/history`    | [`history`]      |
//! | `internal/alerts`     | [`alerts`]       |
//! | `internal/insights`   | [`insights`]     |
//! | `internal/updates`    | [`updates`]      |
//! | `internal/app` (logic)| [`service`]      |

pub mod alerts;
pub mod claude;
pub mod codex;
pub mod config;
pub mod cursor;
pub mod history;
pub mod insights;
pub mod opencode;
pub mod service;
pub mod updates;
pub mod usage;

pub use alerts::{Milestone, Tracker};
pub use config::{AlertState, Config};
pub use history::{History, Sample};
pub use service::{AlertNotice, RefreshReport, Service, Settings, UiState};
pub use usage::{Provider, Share, Usage, Window};

/// Version reported in User-Agent headers and update checks.
pub fn version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}
