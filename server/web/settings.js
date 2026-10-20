import { settings, setQuality, setFormat, setPreload, PRELOADS, onChange, QUALITIES, FORMATS, qualityHint, opusSupported } from "./player.js";

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

const QUALITY_NAMES = { auto: "Auto", original: "Original", normal: "Normal", low: "Low" };
const FORMAT_NAMES = { flac: "FLAC (original file as stored)", mp3: "MP3 320 kbps" };

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
    h("p", { class: "muted" }, "Saved on this device. Auto picks Original, Normal or Low per song from how fast the music loads."),
    radios("quality", QUALITIES, (k) => [QUALITY_NAMES[k], " ", h("span", { class: "muted" }, qualityHint(k))], () => settings().quality, setQuality),
    opusSupported ? null : h("p", { class: "muted" }, "This browser can't play Opus, so Normal and Low use MP3 192 and MP3 128."));
}

function formatRow() {
  return h("div", {},
    h("p", { class: "muted" }, "What Original streams."),
    radios("origformat", FORMATS, (k) => FORMAT_NAMES[k], () => settings().format, setFormat));
}

const PRELOAD_NAMES = { auto: "Auto", always: "Always", off: "Off" };
const PRELOAD_HINTS = {
  auto: "Only on fast connections.",
  always: "Next song is ready instantly, but uses more data.",
  off: "Saves data, short pause between songs.",
};

function preloadRow() {
  return h("div", {},
    h("p", { class: "muted" }, "Saved on this device. Loads the next song before the current one ends."),
    radios("preload", PRELOADS, (k) => [PRELOAD_NAMES[k], " ", h("span", { class: "muted" }, PRELOAD_HINTS[k])], () => settings().preload, setPreload));
}

export async function saveAvatar(file) {
  let blob;
  try {
    blob = await squareAvatar(file);
  } catch {}
  if (!blob) throw new Error("That file is not a picture");
  const res = await fetch("/api/avatar", { method: "PUT", body: blob, headers: { "Content-Type": blob.type } });
  if (res.status === 401) throw Object.assign(new Error("login"), { login: true });
  if (!res.ok) throw new Error("Could not save the picture");
  return (await res.json()).avatar;
}

export async function resetAvatar() {
  const res = await fetch("/api/avatar", { method: "DELETE" });
  if (res.status === 401) throw Object.assign(new Error("login"), { login: true });
  if (!res.ok) throw new Error("Could not reset the picture");
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
  const pic = h("button", { class: "settings-pic", type: "button", "aria-label": "Change profile picture", title: "Change picture" });
  const picker = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp", hidden: true });
  const reset = h("button", { class: "linkish", type: "button" }, "Reset picture");
  const status = h("div", { class: "profile-status", role: "status" });
  const paint = () => {
    paintAvatar(pic, version);
    reset.hidden = !version;
  };
  const run = async (job) => {
    status.textContent = "Saving…";
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

const ITEMS = [
  { id: "quality", label: "Streaming quality", render: qualityRow },
  { id: "format", label: "Original format", render: formatRow },
  { id: "preload", label: "Preload next song", render: preloadRow },
];

export function renderSettings(el, { onClose, ...profile }) {
  const close = h("button", { class: "ghost settings-close", type: "button", "aria-label": "Close settings", onclick: onClose }, "✕ Close");
  el.replaceChildren(
    h("div", { class: "settings-head" }, h("h1", {}, "Settings"), close),
    profileHeader(profile),
    h("ul", { class: "settings-list" }, ...ITEMS.map((item) =>
      h("li", { class: "settings-row", id: `setting-${item.id}` }, h("span", { class: "settings-label" }, item.label), item.render()))));
  close.focus();
}
