import { settings, setQuality, setFormat, setPreload, PRELOADS, onChange, QUALITIES, FORMATS, opusSupported } from "./player.js";
import { t, lang, LANGS, setLang } from "./i18n.js";
import { apiJSON, Unauthorized } from "./dom.js";
import { pushSupported, needsHomeScreen, pushEnabled, enablePush, disablePush } from "./push.js";
import { vizEnabled, setVizEnabled, vizRisky, vizRouted } from "./viz.js";

const h = (tag, attrs = {}, ...kids) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, "");
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) if (kid != null) el.append(kid);
  return el;
};

export const GEAR = '<svg viewBox="0 0 8 8" width="16" height="16" shape-rendering="crispEdges" aria-hidden="true"><path fill="currentColor" fill-rule="evenodd" d="M3 0h2v1h1v1h1v1h1v2h-1v1h-1v1h-1v1h-2v-1h-1v-1h-1v-1h-1v-2h1v-1h1v-1h1zM3 3h2v2h-2z"/></svg>';

export const AVATAR = '<svg viewBox="0 0 8 8" width="20" height="20" shape-rendering="crispEdges" aria-hidden="true"><path fill="currentColor" d="M3 1h2v1h1v2h-1v1h-2v-1h-1v-2h1zM1 6h1v-1h4v1h1v2h-6z"/></svg>';
export const AVATAR_SIZE = 128;

async function squareAvatar(file) {
  const bmp = await createImageBitmap(file);
  const side = Math.min(bmp.width, bmp.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = AVATAR_SIZE;
  const ctx = canvas.getContext("2d");
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, AVATAR_SIZE, AVATAR_SIZE);
  bmp.close();
  const blob = (type, q) => new Promise((ok) => canvas.toBlob(ok, type, q));
  const png = await blob("image/png");
  return png && png.size <= 200 * 1024 ? png : blob("image/jpeg", 0.9);
}


function radios(name, keys, label, get, set) {
  const inputs = keys.map((k) => h("input", { type: "radio", name, value: k, onchange: () => set(k) }));
  const sync = () => inputs.forEach((i) => { i.checked = i.value === get(); });
  sync();
  const el = h("div", { class: "settings-radios", role: "radiogroup" }, keys.map((k, i) => h("label", {}, inputs[i], " ", label(k))));
  const off = onChange(() => { if (!el.isConnected) { off(); return; } sync(); });
  return el;
}

function qualityRow() {
  return h("div", {},
    radios("quality", QUALITIES, (k) => t(`quality.${k}`), () => settings().quality, setQuality),
    opusSupported ? null : h("p", { class: "muted" }, t("settings.noOpus")));
}

function formatRow() {
  return h("div", {},
    radios("origformat", FORMATS, (k) => t(`format.${k}`), () => settings().format, setFormat));
}


function preloadRow() {
  return h("div", {},
    radios("preload", PRELOADS, (k) => t(`preload.${k}`), () => settings().preload, setPreload));
}

export async function saveAvatar(file) {
  let blob;
  try {
    blob = await squareAvatar(file);
  } catch {}
  if (!blob) throw new Error(t("avatar.notPicture"));
  const res = await fetch("/api/avatar", { method: "PUT", body: blob, headers: { "Content-Type": blob.type } });
  if (res.status === 401) throw Object.assign(new Error("login"), { login: true });
  if (!res.ok) throw new Error(t("avatar.saveFailed"));
  return (await res.json()).avatar;
}

export async function resetAvatar() {
  const res = await fetch("/api/avatar", { method: "DELETE" });
  if (res.status === 401) throw Object.assign(new Error("login"), { login: true });
  if (!res.ok) throw new Error(t("avatar.resetFailed"));
  return 0;
}

export function paintAvatar(el, version) {
  el.replaceChildren();
  if (version) {
    const img = h("img", { src: `/api/avatar?v=${version}`, alt: "", width: AVATAR_SIZE, height: AVATAR_SIZE });
    img.addEventListener("error", () => { el.innerHTML = AVATAR; });
    el.append(img);
  } else el.innerHTML = AVATAR;
  el.classList.toggle("has-pic", !!version);
}

function profileHeader({ user, avatar, onAvatar, onLogin }) {
  let version = avatar || 0;
  const pic = h("button", { class: "settings-pic", type: "button", "aria-label": t("avatar.changeLabel"), title: t("avatar.change") });
  const picker = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp", hidden: true });
  const reset = h("button", { class: "linkish", type: "button" }, t("avatar.reset"));
  const status = h("div", { class: "profile-status", role: "status" });
  const paint = () => {
    paintAvatar(pic, version);
    reset.hidden = !version;
  };
  const run = async (job) => {
    status.textContent = t("common.saving");
    pic.disabled = reset.disabled = true;
    try {
      version = await job();
      status.textContent = "";
      paint();
      onAvatar(version);
    } catch (e) {
      if (e.login) return onLogin();
      status.textContent = e.message;
    } finally {
      pic.disabled = reset.disabled = false;
    }
  };
  pic.addEventListener("click", () => picker.click());
  picker.addEventListener("change", () => {
    const file = picker.files[0];
    picker.value = "";
    if (file) run(() => saveAvatar(file));
  });
  reset.addEventListener("click", () => run(resetAvatar));
  paint();
  return h("div", { class: "settings-profile" }, pic, picker, h("div", { class: "settings-user" }, user), reset, status);
}

const BASES = { classic: ["#1a1c2c", "#29366f"], violet: ["#221a2e", "#4a2a74"], slate: ["#171b22", "#262c37"], graphite: ["#1c1b19", "#2b2926"], dusk: ["#1b1824", "#2a2538"], moss: ["#151c16", "#223024"], night: ["#0e0f12", "#1a1c21"] };
const KEYS = { yellow: ["#ffcd75", "#ef7d57"], coral: ["#ff8a6b", "#ffcd75"], pink: ["#ff6fc8", "#4fe0f0"], purple: ["#b98cff", "#ff8ad8"], blue: ["#6fa8ff", "#ff9e6b"], cyan: ["#4fe0f0", "#ff6fc8"], green: ["#5ee08a", "#c39bff"], lime: ["#c8f54a", "#6fa8ff"] };

function colourGetter(attr, all, def) {
  return () => {
    const v = document.documentElement.getAttribute(attr);
    return v in all ? v : def;
  };
}

function colourSetter(attr, all, def, store) {
  return (k) => {
    if (!(k in all)) return;
    if (k === def) document.documentElement.removeAttribute(attr);
    else document.documentElement.setAttribute(attr, k);
    try { localStorage.setItem(store, k); } catch {}
  };
}

function swatches(name, all, label, paint, get, set) {
  const btns = Object.keys(all).map((k) => {
    const b = h("button", { type: "button", role: "radio", class: `swatch swatch-${name}`, title: label(k), "aria-label": label(k), "data-value": k });
    paint(b, all[k]);
    return b;
  });
  const sync = () => btns.forEach((b) => {
    const on = b.dataset.value === get();
    b.setAttribute("aria-checked", on ? "true" : "false");
    b.tabIndex = on ? 0 : -1;
  });
  const pick = (i) => {
    set(btns[i].dataset.value);
    sync();
    btns[i].focus();
  };
  btns.forEach((b, i) => {
    b.addEventListener("click", () => pick(i));
    b.addEventListener("keydown", (e) => {
      const n = btns.length;
      const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
      if (step) { e.preventDefault(); pick((i + step + n) % n); }
      else if (e.key === "Home") { e.preventDefault(); pick(0); }
      else if (e.key === "End") { e.preventDefault(); pick(n - 1); }
    });
  });
  sync();
  return btns;
}

function themeRow() {
  const baseGet = colourGetter("data-base", BASES, "classic");
  const keyGet = colourGetter("data-key", KEYS, "yellow");
  const bases = swatches("base", BASES, (k) => t(`base.${k}`), (b, [bg, card]) => { b.style.setProperty("--sw-a", bg); b.style.setProperty("--sw-b", card); }, baseGet, colourSetter("data-base", BASES, "classic", "bm.base"));
  const keys = swatches("key", KEYS, (k) => t(`key.${k}`), (b, [key, second]) => { b.style.setProperty("--sw-a", key); b.style.setProperty("--sw-b", second); }, keyGet, colourSetter("data-key", KEYS, "yellow", "bm.key"));
  return h("div", {},
    h("div", { class: "swatch-label", id: "swatch-base-label" }, t("settings.base")),
    h("div", { class: "swatch-row", role: "radiogroup", "aria-labelledby": "swatch-base-label" }, bases),
    h("div", { class: "swatch-label", id: "swatch-key-label" }, t("settings.key")),
    h("div", { class: "swatch-row", role: "radiogroup", "aria-labelledby": "swatch-key-label" }, keys));
}

function vizRow() {
  const box = h("input", { type: "checkbox", onchange: () => { setVizEnabled(box.checked); note.hidden = box.checked || !vizRouted(); } });
  box.checked = vizEnabled();
  const note = h("p", { class: "muted", hidden: box.checked || !vizRouted() }, t("settings.vizOffNote"));
  return h("div", {},
    vizRisky ? h("p", { class: "muted" }, t("settings.vizIos")) : null,
    h("div", { class: "settings-radios" }, h("label", {}, box, " ", t("settings.vizOn"))),
    note);
}

function pushRow() {
  if (needsHomeScreen) return h("p", { class: "muted" }, t("settings.pushHomeScreen"));
  if (!pushSupported) return h("p", { class: "muted" }, t("settings.pushUnsupported"));
  const note = h("p", { class: "muted", hidden: true });
  const box = h("input", { type: "checkbox", disabled: true, onchange: async () => {
    box.disabled = true;
    note.hidden = true;
    try {
      if (box.checked) await enablePush();
      else await disablePush();
    } catch (err) {
      box.checked = !box.checked;
      note.textContent = t(err.message === "denied" ? "settings.pushDenied" : "settings.pushFailed");
      note.hidden = false;
    }
    box.disabled = false;
  } });
  pushEnabled().then((on) => { box.checked = on; box.disabled = false; }, () => { box.disabled = false; });
  return h("div", {},
    h("div", { class: "settings-radios" }, h("label", {}, box, " ", t("settings.pushOn"))),
    note);
}

function languageRow() {
  return h("div", {},
    radios("language", Object.keys(LANGS), (k) => LANGS[k], () => lang, setLang));
}

const FEEDBACK_MAX = 2000;
const FEEDBACK_KINDS = ["bug", "idea", "other"];
let lastPage = location.hash;
window.addEventListener("hashchange", (e) => {
  const old = new URL(e.oldURL).hash;
  if (!old.startsWith("#/settings")) lastPage = old;
});

function feedbackForm(onSent) {
  let kind = "bug";
  const text = h("textarea", { class: "feedback-text", rows: "5", maxlength: String(FEEDBACK_MAX), "aria-label": t("feedback.textLabel"), placeholder: t("feedback.placeholder") });
  const send = h("button", { class: "primary", type: "submit", disabled: true }, t("feedback.send"));
  const status = h("div", { class: "profile-status", role: "status" });
  const sync = () => { send.disabled = !text.value.trim(); };
  text.addEventListener("input", () => { sync(); if (status.dataset.ok) { status.textContent = ""; delete status.dataset.ok; } });
  const form = h("form", { class: "feedback-form" },
    radios("feedbackkind", FEEDBACK_KINDS, (k) => t(`feedback.kind.${k}`), () => kind, (k) => { kind = k; }),
    text,
    h("div", { class: "feedback-actions" }, send, status));
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const body = text.value.trim();
    if (!body) return;
    send.disabled = text.disabled = true;
    delete status.dataset.ok;
    status.textContent = t("feedback.sending");
    try {
      await apiJSON("POST", "/api/feedback", { category: kind, text: body, lang, page: lastPage });
      text.value = "";
      status.dataset.ok = "1";
      status.textContent = t("feedback.thanks");
      onSent?.();
    } catch (err) {
      status.textContent = err instanceof Unauthorized ? t("feedback.login") : err.message === "rate-limited" ? t("feedback.tooMany") : err.message === "save-failed" ? t("feedback.saveFailed") : t("feedback.failed");
    } finally {
      text.disabled = false;
      sync();
    }
  });
  return form;
}

export function openFeedback(returnTo) {
  const closeBtn = h("button", { class: "ghost news-close", type: "button", "aria-label": t("news.close"), title: t("news.close"), onclick: () => dlg.close() }, "✕");
  let timer = 0;
  const form = feedbackForm(() => { timer = setTimeout(() => dlg.close(), 1500); });
  const dlg = h("dialog", { class: "sheet card news feedback-dialog", "aria-labelledby": "feedback-title" },
    h("div", { class: "news-head" }, h("h2", { id: "feedback-title" }, t("settings.feedback")), closeBtn),
    form);
  dlg.addEventListener("click", (e) => {
    if (e.target !== dlg) return;
    const r = dlg.getBoundingClientRect();
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) dlg.close();
  });
  dlg.addEventListener("close", () => { clearTimeout(timer); dlg.remove(); returnTo?.focus(); });
  document.body.append(dlg);
  dlg.showModal();
  form.querySelector("textarea").focus();
}

const ITEMS = [
  { id: "language", label: () => t("settings.language"), render: languageRow },
  { id: "push", label: () => t("settings.push"), render: pushRow },
  { id: "theme", label: () => t("settings.theme"), render: themeRow },
  { id: "viz", label: () => t("settings.viz"), render: vizRow },
  { id: "quality", label: () => t("settings.quality"), render: qualityRow },
  { id: "format", label: () => t("settings.format"), render: formatRow },
  { id: "preload", label: () => t("settings.preload"), render: preloadRow },
];

export function renderSettings(el, { onClose, ...profile }) {
  const close = h("button", { class: "ghost settings-close", type: "button", "aria-label": t("settings.closeLabel"), onclick: onClose }, t("settings.close"));
  el.replaceChildren(
    h("div", { class: "settings-head" }, h("h1", {}, t("settings.title")), close),
    profileHeader(profile),
    h("ul", { class: "settings-list" }, ...ITEMS.map((item) =>
      h("li", { class: "settings-row", id: `setting-${item.id}` }, h("span", { class: "settings-label" }, item.label()), item.render()))));
  close.focus();
}
