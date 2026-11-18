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

// Simplified brand marks, drawn inline so they follow the theme and need no assets.
const LOGOS = {
  claude: '<svg viewBox="0 0 24 24" fill="none" stroke="#d97757" stroke-width="2.6" stroke-linecap="round"><path d="M12 2.5v19M2.5 12h19M5.3 5.3l13.4 13.4M18.7 5.3L5.3 18.7M8 3.3l8 17.4M16 3.3L8 20.7"/></svg>',
  codex: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><ellipse cx="12" cy="7" rx="3.2" ry="5"/><ellipse cx="12" cy="7" rx="3.2" ry="5" transform="rotate(60 12 12)"/><ellipse cx="12" cy="7" rx="3.2" ry="5" transform="rotate(120 12 12)"/><ellipse cx="12" cy="7" rx="3.2" ry="5" transform="rotate(180 12 12)"/><ellipse cx="12" cy="7" rx="3.2" ry="5" transform="rotate(240 12 12)"/><ellipse cx="12" cy="7" rx="3.2" ry="5" transform="rotate(300 12 12)"/></svg>',
  opencode: '<svg viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M4 2h16v20H4zM8 6v12h8V6z"/><path d="M8 11h8v7H8z" opacity=".45"/></svg>',
  cursor: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 1.5l9.5 5.5v11L12 23.5 2.5 18V7z" opacity=".3"/><path d="M12 12L21.5 7 12 1.5 2.5 7zM12 12v11.5l9.5-5.5V7z"/></svg>',
};

function providerLogo(provider) {
  const key = LOGOS[provider] ? provider : "claude";
  const badge = el("span", { class: `provider-logo provider-logo-${key}`, role: "img", "aria-label": providerName(provider), title: providerName(provider) });
  badge.innerHTML = LOGOS[key];
  return badge;
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
  const reset = resetIn(window.resetsAt).replace("Resets in ", "");
  return el("div", { class: `compact-quota compact-quota-${index}` }, [
    el("div", { class: "compact-quota-head" }, [
      el("span", { class: "compact-meter-label", text: shortWindowLabel(window, index) }),
      el("strong", { class: "compact-value", text: percent(window.percent) }),
    ]),
    el("span", { class: "compact-track" }, [
      el("span", { class: "compact-fill", style: `width:${value}%` }),
      elapsed === null ? null : el("span", { class: "compact-pace", style: `left:${Math.round(elapsed * 100)}%` }),
    ]),
    window.resetsAt ? el("div", { class: "compact-reset", text: reset === "Resets now" ? "Resets now" : `${reset} left` }) : null,
  ]);
}

function compactProvider(view) {
  const usage = view.usage || {};
  const provider = usage.provider || view.provider;
  const windows = [usage.fiveHour, usage.sevenDay].filter(Boolean);
  const body = [el("header", { class: "compact-provider-head" }, [
    providerLogo(provider),
    usage.err && windows.length ? el("span", { class: "compact-warning", title: "Stale · last refresh failed", "aria-label": "Stale" }) : null,
  ])];
  if (windows.length) body.push(...windows.map(compactMeter));
  else body.push(el("p", { class: "compact-error", text: usage.err ? "Unavailable · sign in, then refresh" : "Waiting for the first reading…" }));
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
  els.providers.replaceChildren(
    el("div", { class: "compact-column-heads" }, [
      el("span"),
      el("span", { text: "Primary" }),
      el("span", { text: "Secondary" }),
    ]),
    ...providers.map(compactProvider),
  );
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
