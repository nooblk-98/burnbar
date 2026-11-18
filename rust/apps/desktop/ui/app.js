/* BurnBar compact tray UI. The large dashboard was intentionally removed. */

const tauri = window.__TAURI__;
const invoke = tauri?.core?.invoke;
const listen = tauri?.event?.listen;

const els = {
  popup: document.getElementById("compact-popup"),
  providers: document.getElementById("compact-providers"),
  settingsDialog: document.getElementById("settings-dialog"),
  settingsForm: document.getElementById("settings-form"),
  settingsError: document.getElementById("settings-error"),
  aboutDialog: document.getElementById("about-dialog"),
  aboutVersion: document.getElementById("about-version"),
  toast: document.getElementById("toast"),
};

let state = null;
let toastTimer = null;

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "dataset") Object.assign(node.dataset, value);
    else if (value !== undefined && value !== null && value !== false) {
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
  if (settings.theme === "light" || settings.theme === "dark") root.dataset.theme = settings.theme;
  else delete root.dataset.theme;
  const scale = [100, 125, 150].includes(settings.textScale) ? settings.textScale : 100;
  root.style.fontSize = `${14 * (scale / 100)}px`;
}

function providerName(provider) {
  if (provider === "codex") return "Codex";
  if (provider === "opencode") return "OpenCode";
  if (provider === "cursor") return "Cursor";
  return "Claude";
}

function percent(value) {
  return typeof value === "number" && !Number.isNaN(value) ? `${Math.round(value)}%` : "–";
}

function resetIn(iso) {
  if (!iso) return "";
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return "";
  const minutes = Math.max(0, Math.round((at - Date.now()) / 60_000));
  if (minutes < 1) return "Resets now";
  if (minutes < 60) return `Resets in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `Resets in ${hours}h${minutes % 60 ? `${String(minutes % 60).padStart(2, "0")}m` : ""}`;
  const days = Math.floor(hours / 24);
  return `Resets in ${days}d${hours % 24 ? `${hours % 24}h` : ""}`;
}

function elapsedFraction(window) {
  if (!window?.resetsAt || !window.lengthSecs) return null;
  const reset = new Date(window.resetsAt).getTime();
  if (Number.isNaN(reset)) return null;
  return Math.min(1, Math.max(0, 1 - (reset - Date.now()) / (window.lengthSecs * 1000)));
}

function shortWindowLabel(window, index) {
  const seconds = Number(window?.lengthSecs || 0);
  if (seconds > 0 && seconds % 86_400 === 0) return `${seconds / 86_400}d`;
  if (seconds > 0 && seconds % 3_600 === 0) return `${seconds / 3_600}h`;
  if (window?.key === "monthly") return "Month";
  return index === 0 ? "Session" : "Weekly";
}

function compactMeter(window, index) {
  const elapsed = elapsedFraction(window);
  const value = Math.min(100, Math.max(0, Number(window.percent) || 0));
  return el("div", { class: `compact-meter compact-meter-${index}` }, [
    el("div", { class: "compact-meter-line" }, [
      el("span", { class: "compact-meter-label", text: shortWindowLabel(window, index) }),
      el("span", { class: "compact-track" }, [
        el("span", { class: "compact-fill", style: `width:${value}%` }),
        elapsed === null ? null : el("span", { class: "compact-pace", style: `left:${Math.round(elapsed * 100)}%` }),
      ]),
      el("strong", { class: "compact-value", text: percent(window.percent) }),
    ]),
    window.resetsAt ? el("div", { class: "compact-reset", text: resetIn(window.resetsAt) }) : null,
  ]);
}

function compactProvider(view) {
  const usage = view.usage || {};
  const provider = usage.provider || view.provider;
  const windows = [usage.fiveHour, usage.sevenDay].filter(Boolean);
  const body = [
    el("header", { class: "compact-provider-head" }, [
      el("strong", { text: providerName(provider) }),
      usage.err ? el("span", { class: "compact-warning", text: "! Unavailable" }) : null,
    ]),
  ];
  if (windows.length) body.push(...windows.map(compactMeter));
  else body.push(el("p", { class: "compact-error", text: usage.err || "Waiting for the first successful reading…" }));
  return el("section", { class: "compact-provider", dataset: { provider } }, body);
}

function render(snapshot) {
  state = snapshot;
  applyAppearance(snapshot.settings);
  const providers = snapshot.providers || [];
  els.providers.replaceChildren(...providers.map(compactProvider));
  if (!providers.length) {
    els.providers.replaceChildren(el("p", { class: "compact-error", text: "Choose a provider in Settings." }));
  }
  els.aboutVersion.textContent = `Version ${snapshot.version}${snapshot.demo ? " · demo" : ""} · ${snapshot.platform}`;
  if (els.settingsDialog.open && !els.settingsForm.dataset.dirty) fillSettings(snapshot.settings);
}

async function load() {
  try {
    render(await invoke("get_state"));
  } catch (error) {
    els.providers.replaceChildren(el("p", { class: "compact-error", text: String(error) }));
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
    "milestonePopups", "alwaysShowPopup", "alerts", "nativeNotices", "quietHours",
    "startWithWindows", "checkUpdates", "hotkey",
  ]) form[key].checked = Boolean(settings[key]);
  form.dataset.dirty = "";
  showSettingsError("");
}

function showDialogMode() {
  document.body.classList.add("dialog-mode");
}

function openSettings() {
  showDialogMode();
  if (state) fillSettings(state.settings);
  if (!els.settingsDialog.open) els.settingsDialog.showModal();
}

function openAbout() {
  showDialogMode();
  if (!els.aboutDialog.open) els.aboutDialog.showModal();
}

async function closeDialog(dialog) {
  if (dialog.open) dialog.close();
  document.body.classList.remove("dialog-mode");
  await invoke("restore_compact_popup");
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
    respectWindowsQuiet: state?.settings?.respectWindowsQuiet ?? true,
    startWithWindows: form.startWithWindows.checked,
    showForecast: state?.settings?.showForecast ?? true,
    compactNotices: true,
    checkUpdates: form.checkUpdates.checked,
    hotkey: form.hotkey.checked,
  };
}

async function saveSettings(event) {
  event.preventDefault();
  showSettingsError("");
  try {
    const snapshot = await invoke("save_settings", { settings: collectSettings() });
    els.settingsForm.dataset.dirty = "saved";
    render(snapshot);
    await closeDialog(els.settingsDialog);
  } catch (error) {
    showSettingsError(error.message || String(error));
  }
}

function setPreset(name) {
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
    render(await invoke("snooze", { minutes }));
    toast(`Snoozed for ${minutes} minutes`);
  } catch (error) {
    showSettingsError(String(error));
  }
}

async function clearSnooze() {
  try {
    render(await invoke("clear_snooze"));
    toast("Snooze cleared");
  } catch (error) {
    showSettingsError(String(error));
  }
}

function showUpdate(info) {
  const status = document.getElementById("update-status");
  status.replaceChildren(
    document.createTextNode(`Version ${info.latest} is available (you have ${info.current}). `),
    el("button", { type: "button", class: "link-button", text: "Release notes" })
  );
  status.querySelector("button").addEventListener("click", () =>
    invoke("open_external", { url: info.url }).catch((error) => toast(String(error)))
  );
}

async function checkUpdates() {
  const status = document.getElementById("update-status");
  status.textContent = "Checking GitHub…";
  try {
    const info = await invoke("check_updates");
    if (info.available) showUpdate(info);
    else status.textContent = `You're on the latest stable release (${info.current}).`;
  } catch (error) {
    status.textContent = String(error);
  }
}

function bind() {
  els.popup.addEventListener("click", () => invoke("restore_compact_popup"));
  els.settingsForm.addEventListener("submit", saveSettings);
  els.settingsForm.addEventListener("input", () => { els.settingsForm.dataset.dirty = "dirty"; });
  document.getElementById("settings-close").addEventListener("click", () => closeDialog(els.settingsDialog));
  document.getElementById("settings-cancel").addEventListener("click", () => closeDialog(els.settingsDialog));
  document.getElementById("about-close").addEventListener("click", () => closeDialog(els.aboutDialog));
  document.getElementById("open-releases").addEventListener("click", () =>
    invoke("open_external", { url: "https://github.com/nooblk-98/burnbar/releases" }).catch((error) => toast(String(error)))
  );
  document.getElementById("open-repo").addEventListener("click", () =>
    invoke("open_external", { url: "https://github.com/nooblk-98/burnbar" }).catch((error) => toast(String(error)))
  );
  document.querySelectorAll("[data-preset]").forEach((button) =>
    button.addEventListener("click", () => setPreset(button.dataset.preset))
  );
  document.querySelectorAll("[data-snooze]").forEach((button) =>
    button.addEventListener("click", () => snooze(Number(button.dataset.snooze)))
  );
  document.getElementById("snooze-clear").addEventListener("click", clearSnooze);
  document.getElementById("test-notification").addEventListener("click", () =>
    invoke("send_test_notification").then(() => toast("Test notification sent")).catch((error) => showSettingsError(String(error)))
  );
  document.getElementById("check-updates").addEventListener("click", checkUpdates);
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      if (els.settingsDialog.open) closeDialog(els.settingsDialog);
      else if (els.aboutDialog.open) closeDialog(els.aboutDialog);
      else invoke("hide_window");
    } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r") {
      event.preventDefault();
      invoke("refresh_now").catch((error) => toast(String(error)));
    }
  });
}

async function start() {
  if (!invoke) {
    els.providers.replaceChildren(el("p", { class: "compact-error", text: "Run BurnBar from its desktop shell." }));
    return;
  }
  bind();
  document.body.dataset.tail = (await invoke("get_popup_tail_top")) ? "top" : "bottom";
  await load();
  try {
    await listen("state-changed", (event) => render(event.payload));
    await listen("popup-side", (event) => { document.body.dataset.tail = event.payload ? "top" : "bottom"; });
    await listen("popup-mode", (event) => {
      document.body.classList.toggle("dialog-mode", event.payload === "dialog");
    });
    await listen("open-settings", openSettings);
    await listen("open-about", openAbout);
    await listen("update-available", (event) => showUpdate(event.payload));
  } catch (error) {
    console.error("event bridge unavailable", error);
  }
}

start();
