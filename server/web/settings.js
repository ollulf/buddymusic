import { settings, setQuality, setFormat, onChange, QUALITIES, FORMATS, qualityHint, opusSupported } from "./player.js";

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

const QUALITY_NAMES = { auto: "Auto", original: "Original", normal: "Normal", low: "Low" };
const FORMAT_NAMES = { flac: "FLAC (original file as stored)", mp3: "MP3 320 kbps" };

function radios(name, keys, label, get, set) {
  const inputs = keys.map((k) => h("input", { type: "radio", name, value: k, onchange: () => set(k) }));
  const sync = () => inputs.forEach((i) => { i.checked = i.value === get(); });
  sync();
  return { sync, el: h("div", { class: "settings-radios", role: "radiogroup" }, keys.map((k, i) => h("label", {}, inputs[i], " ", label(k)))) };
}

function playback() {
  const q = radios("quality", QUALITIES, (k) => [QUALITY_NAMES[k], " ", h("span", { class: "muted" }, qualityHint(k))], () => settings().quality, setQuality);
  const f = radios("origformat", FORMATS, (k) => FORMAT_NAMES[k], () => settings().format, setFormat);
  const off = onChange(() => { if (!q.el.isConnected) { off(); return; } q.sync(); f.sync(); });
  return [
    h("h3", {}, "Streaming quality"),
    h("p", { class: "muted" }, "Saved on this device. Auto picks Original, Normal or Low per song from how fast the music loads."),
    q.el,
    h("h3", {}, "Original format"),
    h("p", { class: "muted" }, "What Original streams."),
    f.el,
    opusSupported ? null : h("p", { class: "muted" }, "This browser can't play Opus, so Normal and Low use MP3 192 and MP3 128."),
  ];
}

const soon = () => [h("p", { class: "muted" }, "Coming soon")];

const SECTIONS = [
  ["account", "Account", soon],
  ["playback", "Playback", playback],
  ["privacy", "Privacy", soon],
];

export function renderSettings(el, { onClose }) {
  const close = h("button", { class: "ghost settings-close", type: "button", "aria-label": "Close settings", onclick: onClose }, "✕ Close");
  el.replaceChildren(
    h("div", { class: "settings-head" }, h("h1", {}, "Settings"), close),
    ...SECTIONS.map(([id, title, body]) =>
      h("section", { class: "settings-section", id: `settings-${id}` }, h("h2", {}, title), ...body())));
  close.focus();
}
