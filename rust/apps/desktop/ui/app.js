/* BurnBar compact tray UI. Preferences live in the native tray menu. */

const tauri = window.__TAURI__;
const invoke = tauri?.core?.invoke;
const listen = tauri?.event?.listen;
const els = {
  popup: document.getElementById("compact-popup"),
  providers: document.getElementById("compact-providers"),
  aboutDialog: document.getElementById("about-dialog"),
  aboutVersion: document.getElementById("about-version"),
  toast: document.getElementById("toast"),
};
let resizeFrame = 0;
let toastTimer = 0;

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "dataset") Object.assign(node.dataset, value);
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(key, value === true ? "" : String(value));
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
  toastTimer = setTimeout(() => { els.toast.hidden = true; }, 2600);
}

function applyAppearance(settings) {
  const root = document.documentElement;
  if (settings.theme === "light" || settings.theme === "dark") root.dataset.theme = settings.theme;
  else delete root.dataset.theme;
  const scale = [100, 125, 150].includes(settings.textScale) ? settings.textScale : 100;
  root.style.fontSize = `${14 * (scale / 100)}px`;
}

function providerName(provider) {
  return { codex: "Codex", opencode: "OpenCode", cursor: "Cursor" }[provider] || "Claude";
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
  const body = [el("header", { class: "compact-provider-head" }, [
    el("strong", { text: providerName(provider) }),
    usage.err ? el("span", { class: "compact-warning", text: "Unavailable" }) : null,
  ])];
  if (windows.length) body.push(...windows.map(compactMeter));
  else body.push(el("p", { class: "compact-error", text: usage.err || "Waiting for the first reading…" }));
  return el("section", { class: "compact-provider", dataset: { provider } }, body);
}

function resizePopup() {
  cancelAnimationFrame(resizeFrame);
  resizeFrame = requestAnimationFrame(() => {
    if (document.body.classList.contains("dialog-mode")) return;
    const style = getComputedStyle(document.body);
    const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const height = Math.ceil(els.popup.getBoundingClientRect().height + padding);
    invoke("resize_compact_popup", { height }).catch(console.error);
  });
}

function render(snapshot) {
  applyAppearance(snapshot.settings);
  const providers = snapshot.providers || [];
  els.providers.replaceChildren(...providers.map(compactProvider));
  if (!providers.length) els.providers.replaceChildren(el("p", { class: "compact-error", text: "Choose a provider from the tray menu." }));
  els.aboutVersion.textContent = `Version ${snapshot.version}${snapshot.demo ? " · demo" : ""} · ${snapshot.platform}`;
  resizePopup();
}

async function load() {
  try {
    render(await invoke("get_state"));
  } catch (error) {
    els.providers.replaceChildren(el("p", { class: "compact-error", text: String(error) }));
    resizePopup();
  }
}

function openAbout() {
  document.body.classList.add("dialog-mode");
  if (!els.aboutDialog.open) els.aboutDialog.showModal();
}

async function closeAbout() {
  if (els.aboutDialog.open) els.aboutDialog.close();
  document.body.classList.remove("dialog-mode");
  await invoke("restore_compact_popup");
}

function bind() {
  els.popup.addEventListener("click", () => invoke("restore_compact_popup"));
  document.getElementById("about-close").addEventListener("click", closeAbout);
  document.getElementById("open-releases").addEventListener("click", () => invoke("open_external", { url: "https://github.com/nooblk-98/burnbar/releases" }).catch((error) => toast(String(error))));
  document.getElementById("open-repo").addEventListener("click", () => invoke("open_external", { url: "https://github.com/nooblk-98/burnbar" }).catch((error) => toast(String(error))));
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      if (els.aboutDialog.open) closeAbout();
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
      const dialog = event.payload === "dialog";
      document.body.classList.toggle("dialog-mode", dialog);
      if (!dialog) resizePopup();
    });
    await listen("open-about", openAbout);
  } catch (error) {
    console.error("event bridge unavailable", error);
  }
}

start();
