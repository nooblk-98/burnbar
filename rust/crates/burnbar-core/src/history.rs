//! A week of usage readings for the popup's charts (Go: `internal/history/`).

use crate::usage::{Provider, Usage};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::time::Duration;

const KEEP: Duration = Duration::from_secs(8 * 24 * 3600);
const COMPACT_EVERY: Duration = Duration::from_secs(3600);
const MAX_LINE: usize = 64 * 1024;

/// One fetched reading; `s` and `w` are session and weekly percentages
/// (-1 when unknown).
#[derive(Clone, Debug, Copy, PartialEq, Serialize, Deserialize)]
pub struct Sample {
    pub t: i64,
    pub s: f64,
    pub w: f64,
}

pub struct History {
    path: Option<PathBuf>,
    samples: Vec<Sample>,
    next_compact: DateTime<Utc>,
    read_failed: bool,
}

impl History {
    pub fn load(dir: &Path, provider: Provider) -> Self {
        let path = dir.join(provider.history_file());
        let mut history = Self {
            path: Some(path.clone()),
            samples: Vec::new(),
            next_compact: Utc::now() + chrono::Duration::from_std(COMPACT_EVERY).unwrap(),
            read_failed: false,
        };
        let file = match std::fs::File::open(&path) {
            Ok(file) => file,
            Err(err) => {
                history.read_failed = err.kind() != std::io::ErrorKind::NotFound;
                return history;
            }
        };
        let cutoff = (Utc::now() - chrono::Duration::from_std(KEEP).unwrap()).timestamp();
        history.read_failed = history.read(BufReader::new(file), cutoff).is_err();
        if !history.read_failed {
            let _ = history.rewrite();
        }
        history
    }

    /// An in-memory history for demo and screenshot runs; nothing is written.
    pub fn demo() -> Self {
        let now = Utc::now();
        let mut samples = Vec::new();
        let mut t = now - chrono::Duration::from_std(KEEP - Duration::from_secs(86_400)).unwrap();
        while t < now {
            let hours = (now - t).num_minutes() as f64 / 60.0;
            let session = (60.0 * (hours / 5.0 * std::f64::consts::PI).sin()).max(0.0);
            let weekly = 17.0 * (1.0 - hours / (7.0 * 24.0));
            samples.push(Sample {
                t: t.timestamp(),
                s: session,
                w: weekly,
            });
            t += chrono::Duration::minutes(10);
        }
        Self {
            path: None,
            samples,
            next_compact: Utc::now() + chrono::Duration::hours(1),
            read_failed: false,
        }
    }

    /// Bound memory for damaged lines, then continue with later readings.
    /// I/O failures are returned so the caller can preserve the original file.
    fn read(&mut self, reader: BufReader<std::fs::File>, cutoff: i64) -> std::io::Result<()> {
        let mut line = Vec::new();
        let mut reader = reader;
        loop {
            line.clear();
            let read = reader.read_until(b'\n', &mut line)?;
            if read == 0 {
                return Ok(());
            }
            if line.len() > MAX_LINE {
                continue;
            }
            if let Ok(sample) = serde_json::from_slice::<Sample>(&line) {
                if sample.t >= cutoff
                    && sample.s.is_finite()
                    && sample.w.is_finite()
                    && (-1.0..=100.0).contains(&sample.s)
                    && (-1.0..=100.0).contains(&sample.w)
                {
                    self.samples.push(sample);
                }
            }
        }
    }

    /// Replace the file only after every retained sample was safely written.
    fn rewrite(&mut self) -> std::io::Result<()> {
        let Some(path) = self.path.clone() else {
            return Ok(());
        };
        let dir = path.parent().unwrap_or_else(|| Path::new("."));
        let tmp = dir.join(format!(".history-{}.tmp", std::process::id()));
        {
            let mut file = std::fs::File::create(&tmp)?;
            for sample in &self.samples {
                let line = serde_json::to_vec(sample)
                    .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;
                file.write_all(&line)?;
                file.write_all(b"\n")?;
            }
            file.sync_all()?;
        }
        let result = std::fs::rename(&tmp, &path);
        if result.is_err() {
            let _ = std::fs::remove_file(&tmp);
        }
        result
    }

    fn prune(&mut self, cutoff: i64) {
        self.samples.retain(|sample| sample.t >= cutoff);
    }

    pub fn add(&mut self, usage: &Usage) {
        let sample = Sample {
            t: usage.updated_at.map(|at| at.timestamp()).unwrap_or(0),
            s: usage.five_hour.as_ref().map(|w| w.percent).unwrap_or(-1.0),
            w: usage.seven_day.as_ref().map(|w| w.percent).unwrap_or(-1.0),
        };
        let now = Utc::now();
        let cutoff = (now - chrono::Duration::from_std(KEEP).unwrap()).timestamp();
        self.prune(cutoff);
        if sample.t >= cutoff {
            self.samples.push(sample);
        }
        let Some(path) = self.path.clone() else {
            return;
        };
        // Compact at most hourly instead of rewriting eight days of data per
        // fetch. A failed read must never replace unseen readings with our cache.
        if !self.read_failed && now >= self.next_compact {
            self.next_compact = now + chrono::Duration::from_std(COMPACT_EVERY).unwrap();
            if self.rewrite().is_ok() {
                return; // the replacement already contains this reading
            }
        }
        if sample.t < cutoff {
            return;
        }
        if let Ok(mut file) = std::fs::OpenOptions::new()
            .append(true)
            .create(true)
            .open(path)
        {
            if let Ok(line) = serde_json::to_vec(&sample) {
                let _ = file.write_all(&line);
                let _ = file.write_all(b"\n");
            }
        }
    }

    pub fn since(&self, t: DateTime<Utc>) -> Vec<Sample> {
        self.samples
            .iter()
            .copied()
            .filter(|sample| sample.t >= t.timestamp())
            .collect()
    }

    pub fn all(&self) -> &[Sample] {
        &self.samples
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("burnbar-history-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    fn usage(percent: f64) -> Usage {
        let mut usage = Usage::new(Provider::Claude);
        usage.updated_at = Some(Utc::now());
        usage.five_hour = Some(crate::usage::Window::new("session", "Session", percent));
        usage
    }

    #[test]
    fn appends_reads_and_prunes() {
        let dir = temp_dir("append");
        let mut history = History::load(&dir, Provider::Claude);
        history.add(&usage(10.0));
        history.add(&usage(20.0));
        assert_eq!(
            history.since(Utc::now() - chrono::Duration::hours(1)).len(),
            2
        );
        let reloaded = History::load(&dir, Provider::Claude);
        assert_eq!(reloaded.all().len(), 2);
        assert_eq!(reloaded.all()[1].s, 20.0);
        assert!(dir.join("history.jsonl").exists());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn codex_has_its_own_file() {
        let dir = temp_dir("codex");
        let mut claude = History::load(&dir, Provider::Claude);
        claude.add(&usage(10.0));
        let mut codex = History::load(&dir, Provider::Codex);
        codex.add(&usage(20.0));
        assert!(dir.join("history.jsonl").exists());
        assert!(dir.join("history-codex.jsonl").exists());
        assert_eq!(History::load(&dir, Provider::Codex).all().len(), 1);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn damaged_lines_are_skipped_and_old_samples_dropped() {
        let dir = temp_dir("damaged");
        let old = Utc::now().timestamp() - 9 * 86_400;
        let fresh = Utc::now().timestamp();
        let body = format!(
            "not json\n{{\"t\":{old},\"s\":5,\"w\":5}}\n{{\"t\":{fresh},\"s\":10,\"w\":20}}\n"
        );
        std::fs::write(dir.join("history.jsonl"), body).unwrap();
        let history = History::load(&dir, Provider::Claude);
        assert_eq!(history.all().len(), 1);
        assert_eq!(history.all()[0].s, 10.0);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn demo_has_a_week_of_samples() {
        let demo = History::demo();
        assert!(demo.all().len() > 900);
        assert!(demo.all().iter().all(|s| s.s <= 100.0 && s.w >= 0.0));
    }
}
