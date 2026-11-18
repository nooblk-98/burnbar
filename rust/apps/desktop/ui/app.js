/* BurnBar UI — one implementation for Windows, macOS and Linux.
   All data comes from the Rust core through Tauri commands. */

const tauri = window.__TAURI__;
const invoke = tauri?.core?.invoke;
const listen = tauri?.event?.listen;

const els = {
  providers: document.getElementById("providers"),
  empty: document.getElementById("empty"),
  emptyMessage: document.getElementById("empty-message"),
  status: document.getElementById("status-line"),
  refresh: document.getElementById("refresh"),
  settingsButton: document.getElementById("settings-button"),
  settingsDialog: document.getElementById("settings-dialog"),
  settingsForm: document.getElementById("settings-form"),
  settingsError: document.getElementById("settings-error"),
  aboutDialog: document.getElementById("about-dialog"),
  aboutVersion: document.getElementById("about-version"),
  updated: document.getElementById("updated-label"),
  toast: document.getElementById("toast"),
};

let state = null;
let pending = false;
let toastTimer = null;

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "dataset") Object.assign(node.dataset, value);
    else if (key.startsWith("on") && typeof value === "function") {
      node.addEventListener(key.slice(2), value);
    } else if (value !== undefined && value !== null && value !== false) {
      node.setAttribute(key, value === true ? "" : String(value));
    }
  }
  for (const child of [].concat(children)) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child.nodeType ? child : document.createTextNode(String(child)));
  }
  return node;
}

function toast(message) {
  els.toast.textContent = message;
  els.toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    els.toast.hidden = true;
  }, 2600);
}

function applyAppearance(settings) {
  const root = document.documentElement;
  if (settings.theme === "light" || settings.theme === "dark") {
    root.dataset.theme = settings.theme;
  } else {
    delete root.dataset.theme;
  }
  const scale = [100, 125, 150].includes(settings.textScale) ? settings.textScale : 100;
  root.style.fontSize = `${14 * (scale / 100)}px`;
}

function providerName(provider) {
  if (provider === "codex") return "Codex";
  if (provider === "opencode") return "OpenCode";
  if (provider === "cursor") return "Cursor";
  return "Claude";
}

function providerColor(provider) {
  if (provider === "codex") return "var(--codex)";
  if (provider === "opencode") return "var(--opencode)";
  if (provider === "cursor") return "var(--cursor)";
  return "var(--claude)";
}

function percent(value) {
  if (typeof value !== "number" || Number.isNaN(value)) return "—";
  return `${Math.round(value)}%`;
}

function resetIn(iso) {
  if (!iso) return "";
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return "";
  const seconds = Math.round((at - Date.now()) / 1000);
  if (seconds <= 0) return "resetting now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 1) return "resets in <1m";
  if (minutes < 60) return `resets in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return rest ? `resets in ${hours}h ${String(rest).padStart(2, "0")}m` : `resets in ${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return rest ? `resets in ${days}d ${rest}h` : `resets in ${days}d`;
}

function ageLabel(iso) {
  if (!iso) return "Never updated";
  const seconds = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return "Updated just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `Updated ${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `Updated ${hours}h ago`;
  return `Updated ${Math.floor(hours / 24)}d ago`;
}

function elapsedFraction(window) {
  if (!window?.resetsAt || !window.lengthSecs) return null;
  const reset = new Date(window.resetsAt).getTime();
  if (Number.isNaN(reset)) return null;
  const fraction = 1 - (reset - Date.now()) / (window.lengthSecs * 1000);
  return Math.min(1, Math.max(0, fraction));
}

function healthClass(health) {
  if (!health) return "ok";
  const text = health.toLowerCase();
  if (text.includes("rate limited") || text.includes("offline") || text.includes("unavailable") || text.includes("stale")) {
    return "warn";
  }
  if (text.includes("sign-in") || text.includes("login")) return "bad";
  return "warn";
}

function errorHint(provider, message) {
  const text = message.toLowerCase();
  if (provider === "opencode") {
    if (text.includes("subscription")) {
      return "Subscribe to OpenCode Go in the OpenCode Console, then refresh.";
    }
    return "Connect OpenCode Go in OpenCode, then refresh. BurnBar only reads the key it stores.";
  }
  if (provider === "cursor" && (text.includes("sign in") || text.includes("login") || text.includes("cursor is not"))) {
    return "Sign in to the Cursor app on this computer, then refresh.";
  }
  if (text.includes("sign in") || text.includes("login") || text.includes("token") || text.includes("credentials")) {
    return provider === "codex"
      ? "Open a terminal, run `codex login`, then refresh."
      : "Open a terminal, run `claude`, then refresh.";
  }
  if (text.includes("codex cli") || text.includes("install")) {
    return "Install the Codex CLI and run `codex login`.";
  }
  if (text.includes("network") || text.includes("timeout") || text.includes("connection")) {
    return "Check your connection. BurnBar backs off automatically.";
  }
  return "";
}

function meter(window, { forecast, hot }) {
  const fraction = elapsedFraction(window);
  const fillClass = window.percent >= 95 ? "fill critical" : window.percent >= 80 || hot ? "fill hot" : "fill";
  const track = el("div", { class: "track" }, [
    el("div", {
      class: fillClass,
      style: `width:${Math.min(100, Math.max(0, window.percent))}%`,
    }),
  ]);
  if (fraction !== null) {
    track.append(el("div", { class: "pace", style: `left:${(fraction * 100).toFixed(2)}%`, title: "Even pace" }));
  }
  return el("div", { class: "meter" }, [
    el("div", { class: "meter-head" }, [
      el("span", { class: "meter-label", text: window.label || window.key || "Limit" }),
      el("span", { class: "meter-value", text: percent(window.percent) }),
    ]),
    track,
    el("div", { class: "meter-foot" }, [
      el("span", { text: resetIn(window.resetsAt) }),
      el("span", { text: fraction === null ? "" : `${Math.round(fraction * 100)}% of window` }),
    ]),
    forecast ? el("div", { class: "forecast", text: forecast }) : null,
  ]);
}

function drawChart(canvas, samples, color) {
  const dpr = window.devicePixelRatio || 1;
  const width = canvas.clientWidth || 360;
  const height = canvas.clientHeight || 74;
  canvas.width = Math.round(width * dpr);
  canvas.height = Math.round(height * dpr);
  const ctx = canvas.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, width, height);
  const style = getComputedStyle(document.documentElement);
  const grid = style.getPropertyValue("--chart-grid") || "#e8e2d8";
  const muted = style.getPropertyValue("--muted") || "#6f675e";
  ctx.strokeStyle = grid;
  ctx.lineWidth = 1;
  for (const pct of [0, 50, 100]) {
    const y = 3 + ((100 - pct) / 100) * (height - 10);
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.lineTo(width, y);
    ctx.stroke();
  }
  if (!samples || samples.length < 2) {
    ctx.fillStyle = muted;
    ctx.font = "11px system-ui, sans-serif";
    ctx.fillText("Collecting readings…", 6, height / 2);
    return;
  }
  const times = samples.map((s) => s.t);
  const minT = Math.min(...times);
  const maxT = Math.max(...times);
  const span = Math.max(1, maxT - minT);
  const x = (t) => ((t - minT) / span) * width;
  const y = (v) => 3 + ((100 - Math.min(100, Math.max(0, v))) / 100) * (height - 10);
  const series = [
    { key: "s", color, dash: [] },
    { key: "w", color: muted, dash: [3, 3] },
  ];
  for (const line of series) {
    const points = samples.filter((s) => typeof s[line.key] === "number" && s[line.key] >= 0);
    if (points.length < 2) continue;
    ctx.save();
    ctx.beginPath();
    ctx.strokeStyle = line.color;
    ctx.lineWidth = 1.6;
    ctx.setLineDash(line.dash);
    points.forEach((sample, index) => {
      const px = x(sample.t);
      const py = y(sample[line.key]);
      if (index === 0) ctx.moveTo(px, py);
      else ctx.lineTo(px, py);
    });
    ctx.stroke();
    ctx.restore();
  }
}

function chartBlock(view, color) {
  const canvas = el("canvas", { "aria-label": `${view.usage.provider} usage history` });
  let mode = "24";
  const draw = () => {
    const samples = mode === "24" ? view.history24 : view.history7d;
    drawChart(canvas, samples, color);
  };
  const buttons = ["24", "7d"].map((value) =>
    el("button", {
      type: "button",
      "aria-pressed": value === mode ? "true" : "false",
      text: value === "24" ? "24h" : "7d",
      onclick: (event) => {
        mode = value;
        for (const button of event.currentTarget.parentElement.children) {
          button.setAttribute("aria-pressed", button === event.currentTarget ? "true" : "false");
        }
        draw();
      },
    })
  );
  const block = el("div", { class: "chart-wrap" }, [
    el("div", { class: "chart-head" }, [
      el("span", { text: "History — session, weekly" }),
      el("div", { class: "seg" }, buttons),
    ]),
    canvas,
  ]);
  requestAnimationFrame(draw);
  return block;
}

function providerCard(view) {
  const usage = view.usage || {};
  const provider = usage.provider || view.provider;
  const color = providerColor(provider);
  const windows = [usage.fiveHour, usage.sevenDay].filter(Boolean);
  const extras = Array.isArray(usage.extra) ? usage.extra : [];
  const breakdown = Array.isArray(usage.breakdown) ? usage.breakdown : [];
  const hasData = windows.length > 0 || extras.length > 0;

  const children = [
    el("div", { class: "card-head" }, [
      el("h2", { class: "card-title" }, [
        el("span", { class: "dot" }),
        document.createTextNode(providerName(provider)),
      ]),
      el("div", { class: "card-meta" }, [
        view.health ? el("span", { class: `pill ${healthClass(view.health)}`, text: view.health }) : null,
        !view.health ? el("span", { text: view.freshness }) : null,
      ]),
    ]),
  ];

  if (usage.err) {
    children.push(
      el("div", { class: "error-box" }, [
        document.createTextNode(usage.err),
        errorHint(provider, usage.err)
          ? el("span", { class: "hint", text: errorHint(provider, usage.err) })
          : null,
      ])
    );
  }

  if (hasData) {
    if (usage.fiveHour) children.push(meter(usage.fiveHour, { forecast: view.primaryForecast }));
    if (usage.sevenDay) children.push(meter(usage.sevenDay, { forecast: view.secondaryForecast }));
    if (extras.length) {
      children.push(
        el("details", {}, [
          el("summary", { text: `More limits (${extras.length})` }),
          el("ul", { class: "extra-list" }, extras.map((window) => el("li", {}, meter(window, {})))),
        ])
      );
    }
    if (breakdown.length) {
      children.push(
        el("details", {}, [
          el("summary", { text: "This week by product" }),
          el("div", { class: "chips" }, breakdown.map((share) =>
            el("span", { class: "chip", text: `${share.name} ${Math.round(share.percent)}%` })
          )),
        ])
      );
    }
    children.push(chartBlock(view, color));
  } else if (!usage.err) {
    children.push(el("p", { class: "muted small", text: "Waiting for the first successful reading…" }));
  }

  return el(
    "article",
    { class: `card${usage.err ? " error" : ""}`, dataset: { provider } },
    children
  );
}

function renderStatus(snapshot) {
  if (snapshot.demo) return "Demo data — no account needed";
  const problems = snapshot.providers
    .filter((view) => view.health)
    .map((view) => `${providerName(view.usage.provider)}: ${view.health}`);
  if (problems.length) return problems.join(" · ");
  const names = snapshot.providers.map((view) => providerName(view.usage.provider));
  if (!names.length) return "No provider selected";
  return `${names.join(" and ")} ${names.length > 1 ? "are" : "is"} up to date`;
}

function render(snapshot) {
  state = snapshot;
  applyAppearance(snapshot.settings);

  els.status.textContent = renderStatus(snapshot);
  els.status.title = els.status.textContent;

  const providers = snapshot.providers || [];
  els.providers.replaceChildren(...providers.map(providerCard));

  const allEmpty = providers.every((view) => !view.usage?.fiveHour && !view.usage?.sevenDay && !view.usage?.err);
  els.empty.hidden = providers.length > 0 && !allEmpty;
  if (!providers.length) {
    els.emptyMessage.textContent = "Choose at least one provider in Settings.";
  }

  const stamps = providers
    .map((view) => view.usage?.updatedAt)
    .filter(Boolean)
    .sort();
  els.updated.textContent = stamps.length ? ageLabel(stamps[stamps.length - 1]) : "No successful reading yet";
  els.aboutVersion.textContent = `Version ${snapshot.version}${snapshot.demo ? " · demo" : ""} · ${snapshot.platform}`;

  if (els.settingsDialog.open && !els.settingsForm.dataset.dirty) {
    fillSettings(snapshot.settings);
  }
}

async function load() {
  try {
    const snapshot = await invoke("get_state");
    render(snapshot);
  } catch (error) {
    els.status.textContent = "BurnBar could not read its state";
    showSettingsError(String(error));
  }
}

async function refresh() {
  if (pending) return;
  pending = true;
  els.refresh.classList.add("spinning");
  try {
    await invoke("refresh_now");
  } catch (error) {
    toast(String(error));
  } finally {
    setTimeout(() => {
      els.refresh.classList.remove("spinning");
      pending = false;
    }, 800);
  }
}

function showSettingsError(message) {
  els.settingsError.textContent = message || "";
}

function minutesToTime(minutes) {
  const h = Math.floor(minutes / 60) % 24;
  const m = minutes % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
}

function timeToMinutes(value) {
  const [h, m] = value.split(":").map(Number);
  if (!Number.isFinite(h) || !Number.isFinite(m)) throw new Error("Quiet hours need valid times");
  return h * 60 + m;
}

function parseThresholds(text) {
  const parts = text.split(/[,;\s]+/).filter(Boolean);
  if (!parts.length) return [];
  const values = [];
  for (const part of parts) {
    const number = Number(part.replace(/%$/, ""));
    if (!Number.isInteger(number) || number < 1 || number > 100) {
      throw new Error("Thresholds must be whole percentages from 1 to 100");
    }
    if (!values.includes(number)) values.push(number);
  }
  return values.sort((a, b) => a - b);
}

function fillSettings(settings) {
  const form = els.settingsForm;
  form.providers.value = settings.providers;
  form.intervalMinutes.value = settings.intervalMinutes;
  form.popupSeconds.value = String(settings.popupSeconds);
  form.primaryThresholds.value = settings.primaryThresholds.join(", ");
  form.secondaryThresholds.value = settings.secondaryThresholds.join(", ");
  form.quietStart.value = minutesToTime(settings.quietStart);
  form.quietEnd.value = minutesToTime(settings.quietEnd);
  form.theme.value = settings.theme;
  form.textScale.value = String(settings.textScale);
  for (const key of [
    "milestonePopups",
    "alwaysShowPopup",
    "alerts",
    "nativeNotices",
    "quietHours",
    "startWithWindows",
    "showForecast",
    "checkUpdates",
    "hotkey",
  ]) {
    form[key].checked = Boolean(settings[key]);
  }
  els.settingsForm.dataset.dirty = "";
  showSettingsError("");
}

function openSettings() {
  if (state) fillSettings(state.settings);
  els.settingsDialog.showModal();
}

function collectSettings() {
  const form = els.settingsForm;
  return {
    providers: form.providers.value,
    intervalMinutes: Number(form.intervalMinutes.value),
    popupSeconds: Number(form.popupSeconds.value),
    primaryThresholds: parseThresholds(form.primaryThresholds.value),
    secondaryThresholds: parseThresholds(form.secondaryThresholds.value),
    quietStart: timeToMinutes(form.quietStart.value),
    quietEnd: timeToMinutes(form.quietEnd.value),
    theme: form.theme.value,
    textScale: Number(form.textScale.value),
    milestonePopups: form.milestonePopups.checked,
    alwaysShowPopup: form.alwaysShowPopup.checked,
    alerts: form.alerts.checked,
    nativeNotices: form.nativeNotices.checked,
    quietHours: form.quietHours.checked,
    // Tauri delegates Focus / Do Not Disturb policy to native notifications.
    // Preserve the Go-only preference without exposing a non-functional control.
    respectWindowsQuiet: state?.settings?.respectWindowsQuiet ?? true,
    startWithWindows: form.startWithWindows.checked,
    showForecast: form.showForecast.checked,
    // The Tauri window always uses the compact card layout; keep the stored
    // preference intact instead of dropping it from the profile.
    compactNotices: state?.settings?.compactNotices ?? true,
    checkUpdates: form.checkUpdates.checked,
    hotkey: form.hotkey.checked,
  };
}

async function saveSettings(event) {
  event.preventDefault();
  showSettingsError("");
  let settings;
  try {
    settings = collectSettings();
  } catch (error) {
    showSettingsError(error.message);
    return;
  }
  try {
    const snapshot = await invoke("save_settings", { settings });
    els.settingsForm.dataset.dirty = "saved";
    render(snapshot);
    els.settingsDialog.close();
    toast("Settings saved");
  } catch (error) {
    showSettingsError(String(error));
  }
}

async function setPreset(name) {
  const values = {
    decades: "10, 20, 30, 40, 50, 60, 70, 80, 90, 100",
    quarters: "25, 50, 75, 100",
    critical: "50, 80, 95",
  }[name];
  els.settingsForm.primaryThresholds.value = values;
  els.settingsForm.secondaryThresholds.value = values;
}

async function snooze(minutes) {
  try {
    const snapshot = await invoke("snooze", { minutes });
    render(snapshot);
    toast(minutes ? `Snoozed for ${minutes} minutes` : "Snooze cleared");
  } catch (error) {
    showSettingsError(String(error));
  }
}

async function clearSnooze() {
  try {
    const snapshot = await invoke("clear_snooze");
    render(snapshot);
    toast("Snooze cleared");
  } catch (error) {
    showSettingsError(String(error));
  }
}

function showUpdate(info) {
  const status = document.getElementById("update-status");
  status.replaceChildren(
    document.createTextNode(`Version ${info.latest} is available (you have ${info.current}). `),
    el("button", {
      type: "button",
      class: "link-button",
      text: "Release notes",
      onclick: () => invoke("open_external", { url: info.url }).catch((e) => toast(String(e))),
    })
  );
}

async function checkUpdates() {
  const status = document.getElementById("update-status");
  status.textContent = "Checking GitHub…";
  try {
    const info = await invoke("check_updates");
    if (info.available) {
      showUpdate(info);
    } else {
      status.textContent = `You're on the latest stable release (${info.current}).`;
    }
  } catch (error) {
    status.textContent = String(error);
  }
}

async function testNotification() {
  try {
    await invoke("send_test_notification");
    toast("Test notification sent");
  } catch (error) {
    showSettingsError(String(error));
  }
}

function bind() {
  els.refresh.addEventListener("click", refresh);
  els.settingsButton.addEventListener("click", openSettings);
  els.settingsForm.addEventListener("submit", saveSettings);
  els.settingsForm.addEventListener("input", () => {
    els.settingsForm.dataset.dirty = "dirty";
  });
  document.getElementById("settings-close").addEventListener("click", () => els.settingsDialog.close());
  document.getElementById("settings-cancel").addEventListener("click", () => els.settingsDialog.close());
  document.getElementById("about-button").addEventListener("click", () => {
    els.aboutVersion.textContent = state
      ? `Version ${state.version}${state.demo ? " · demo" : ""} · ${state.platform}`
      : "";
    els.aboutDialog.showModal();
  });
  document.getElementById("about-close").addEventListener("click", () => els.aboutDialog.close());
  document.getElementById("open-releases").addEventListener("click", () =>
    invoke("open_external", { url: "https://github.com/nooblk-98/burnbar/releases" }).catch((e) => toast(String(e)))
  );
  document.getElementById("open-repo").addEventListener("click", () =>
    invoke("open_external", { url: "https://github.com/nooblk-98/burnbar" }).catch((e) => toast(String(e)))
  );
  document.querySelectorAll("[data-preset]").forEach((button) =>
    button.addEventListener("click", () => setPreset(button.dataset.preset))
  );
  document.querySelectorAll("[data-snooze]").forEach((button) =>
    button.addEventListener("click", () => snooze(Number(button.dataset.snooze)))
  );
  document.getElementById("snooze-clear").addEventListener("click", clearSnooze);
  document.getElementById("test-notification").addEventListener("click", testNotification);
  document.getElementById("check-updates").addEventListener("click", checkUpdates);

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      if (els.settingsDialog.open) els.settingsDialog.close();
      else if (els.aboutDialog.open) els.aboutDialog.close();
      else invoke("hide_window");
    } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r") {
      event.preventDefault();
      refresh();
    } else if ((event.ctrlKey || event.metaKey) && event.key === ",") {
      event.preventDefault();
      openSettings();
    }
  });

  window.addEventListener("resize", () => {
    // Charts redraw from the latest state.
    if (state) render(state);
  });
}

async function start() {
  if (!invoke) {
    els.status.textContent = "Run BurnBar from its desktop shell";
    return;
  }
  bind();
  await load();
  try {
    await listen("state-changed", (event) => render(event.payload));
    await listen("open-settings", openSettings);
    await listen("open-about", () => document.getElementById("about-button").click());
    await listen("update-available", (event) => {
      showUpdate(event.payload);
      toast(`BurnBar ${event.payload.latest} is available`);
    });
  } catch (error) {
    console.error("event bridge unavailable", error);
  }
  // Keep relative times honest without polling the backend.
  setInterval(() => {
    if (state) {
      els.updated.textContent = (() => {
        const stamps = state.providers.map((view) => view.usage?.updatedAt).filter(Boolean).sort();
        return stamps.length ? ageLabel(stamps[stamps.length - 1]) : "No successful reading yet";
      })();
    }
  }, 30_000);
}

start();
