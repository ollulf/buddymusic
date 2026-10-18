export const h = (tag, attrs = {}, ...kids) => {
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

export class Unauthorized extends Error {}

export async function api(path) {
  const res = await fetch(path);
  if (res.status === 401) throw new Unauthorized();
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `http ${res.status}`);
  return res.json();
}

export async function apiSend(method, path) {
  const res = await fetch(path, { method });
  if (res.status === 401) throw new Unauthorized();
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `http ${res.status}`);
  return res.json();
}

export const coverURL = (id, size = 300) => (id ? `/api/music/cover/${encodeURIComponent(id)}?size=${size}` : "");

export function cover(id, size = 300, cls = "art") {
  const box = h("div", { class: cls });
  if (id) {
    const img = h("img", { src: coverURL(id, size), alt: "", loading: "lazy", decoding: "async" });
    img.addEventListener("error", () => img.remove());
    box.append(img);
  }
  return box;
}

export const fmtTime = (s) => {
  if (!isFinite(s) || s < 0) s = 0;
  s = Math.floor(s);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};
