import { t } from "./i18n.js";
import { h, apiSend, Unauthorized } from "./dom.js";

const overrides = new Map();
const listeners = new Set();
const key = (kind, id) => `${kind}:${id}`;

export const onFavChange = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };

export function isStarred(item, kind = "song") {
  if (!item) return false;
  const k = key(kind, item.id);
  return overrides.has(k) ? overrides.get(k) : Boolean(item.starred);
}

function paint(kind, id) {
  const on = overrides.get(key(kind, id));
  for (const b of document.querySelectorAll(`button.heart[data-kind="${kind}"]`)) {
    if (b.dataset.id === id) setPressed(b, on);
  }
  listeners.forEach((fn) => fn(kind, id, on));
}

function setPressed(b, on) {
  b.setAttribute("aria-pressed", on ? "true" : "false");
  const what = b.dataset.kind === "album" ? "album" : "song";
  const label = t(`favs.${on ? "remove" : "add"}.${what}`);
  b.setAttribute("aria-label", label);
  b.title = label;
}

export async function toggleStar(item, kind = "song", opts = {}) {
  const was = isStarred(item, kind);
  const k = key(kind, item.id);
  overrides.set(k, !was);
  paint(kind, item.id);
  try {
    await apiSend(was ? "DELETE" : "POST", `/api/music/star/${encodeURIComponent(item.id)}${kind === "album" ? "?kind=album" : ""}`);
  } catch (e) {
    overrides.set(k, was);
    paint(kind, item.id);
    if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return; }
    alert(t("favs.failed"));
  }
}

export function heartButton(item, kind = "song", opts = {}) {
  const b = h("button", { class: `icon heart${opts.cls ? ` ${opts.cls}` : ""}`, type: "button", "data-kind": kind, "data-id": item?.id || "" });
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    const cur = opts.get ? opts.get() : item;
    if (cur?.id) toggleStar(cur, kind, opts);
  });
  setPressed(b, isStarred(item, kind));
  return b;
}

export function retarget(b, item, kind = "song") {
  b.dataset.id = item?.id || "";
  setPressed(b, isStarred(item, kind));
}
