import { settings, setQuality, setFormat, setPreload, PRELOADS, onChange, QUALITIES, FORMATS, qualityHint, opusSupported } from "./player.js";
import { t, lang, LANGS, setLang } from "./i18n.js";

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
    h("p", { class: "muted" }, t("settings.qualityHint")),
    radios("quality", QUALITIES, (k) => [t(`quality.${k}`), " ", h("span", { class: "muted" }, qualityHint(k))], () => settings().quality, setQuality),
    opusSupported ? null : h("p", { class: "muted" }, t("settings.noOpus")));
}

function formatRow() {
  return h("div", {},
    h("p", { class: "muted" }, t("settings.formatHint")),
    radios("origformat", FORMATS, (k) => t(`format.${k}`), () => settings().format, setFormat));
}


function preloadRow() {
  return h("div", {},
    h("p", { class: "muted" }, t("settings.preloadHint")),
    radios("preload", PRELOADS, (k) => [t(`preload.${k}`), " ", h("span", { class: "muted" }, t(`preload.${k}Hint`))], () => settings().preload, setPreload));
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

const THEME_KEY = "bm.theme";
const THEMES = ["classic", "slate", "lagoon", "dusk", "graphite", "moss", "frost", "ember", "terminal", "paper"];

function currentTheme() {
  const v = document.documentElement.getAttribute("data-theme");
  return THEMES.includes(v) ? v : "classic";
}

function setTheme(k) {
  if (!THEMES.includes(k)) return;
  if (k === "classic") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", k);
  try { localStorage.setItem(THEME_KEY, k); } catch {}
}

function themeRow() {
  return h("div", {},
    h("p", { class: "muted" }, t("settings.themeHint")),
    radios("theme", THEMES, (k) => t(`theme.${k}`), currentTheme, setTheme));
}

function languageRow() {
  return h("div", {},
    h("p", { class: "muted" }, t("settings.languageHint")),
    radios("language", Object.keys(LANGS), (k) => LANGS[k], () => lang, setLang));
}

const ITEMS = [
  { id: "language", label: () => t("settings.language"), render: languageRow },
  { id: "theme", label: () => t("settings.theme"), render: themeRow },
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
