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

const soon = () => [h("p", { class: "muted" }, "Coming soon")];

const SECTIONS = [
  ["account", "Account", soon],
  ["playback", "Playback", soon],
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
