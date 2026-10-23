import { h, fmtTime } from "./dom.js";
import { t } from "./i18n.js";
import { onChange, queue, current, jump, move, removeAt, clearManual, entryInfo } from "./player.js";

const SPINES = ["var(--accent)", "var(--orange)", "var(--accent-lo)", "var(--muted)", "var(--accent-hi)", "var(--fg)", "var(--steel)", "color-mix(in srgb, var(--accent) 50%, var(--orange))", "color-mix(in srgb, var(--orange) 55%, var(--fg))", "color-mix(in srgb, var(--accent-hi) 50%, var(--steel))"];
const caseColors = new Map();

export const caseColor = (key, below) => {
  if (caseColors.has(key)) return caseColors.get(key);
  let x = 0;
  for (const c of key) x = (x * 131 + c.charCodeAt(0)) >>> 0;
  let i = x % SPINES.length;
  if (SPINES[i] === below) i = (i + 1) % SPINES.length;
  caseColors.set(key, SPINES[i]);
  if (caseColors.size > 500) caseColors.delete(caseColors.keys().next().value);
  return SPINES[i];
};

export const spine = (song) => {
  const k = String(song.albumId || song.id || "");
  let x = 0;
  for (const c of k) x = (x * 31 + c.charCodeAt(0)) >>> 0;
  return SPINES[x % SPINES.length];
};

const calm = () => !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
const touchy = () => !!(window.matchMedia && window.matchMedia("(hover: none)").matches);

export function renderQueue() {
  const stack = h("button", { type: "button", class: "cdq", "aria-haspopup": "dialog", "aria-expanded": "false" });
  const cases = h("span", { class: "cdq-cases", "aria-hidden": "true" });
  stack.append(cases);

  const list = h("ol", { class: "qp-list" });
  const count = h("small");
  const closeB = h("button", { type: "button", class: "qp-x", "aria-label": t("queue.close"), title: t("queue.close") }, "✕");
  const clearB = h("button", { type: "button", class: "qp-clear" }, t("queue.clear"));
  const panel = h("div", { class: "qpanel", role: "dialog", "aria-label": t("queue.title"), hidden: true },
    h("div", { class: "qp-head" }, h("strong", {}, t("queue.title")), count, clearB, closeB),
    list);
  const root = h("div", { class: "cdq-wrap" }, stack, panel);

  let lastSig = "";
  let lastRuns = null;
  let lastUp = 0;
  const upcoming = () => {
    const q = queue();
    const c = current();
    const i = c ? q.indexOf(c) : -1;
    const up = [];
    for (let n = i + 1; n < q.length && entryInfo(q[n]).manual; n++) up.push(q[n]);
    return { q, i, up };
  };

  const setLift = (on) => root.classList.toggle("lift", on);
  const isOpen = () => !panel.hidden;
  const close = () => {
    if (!isOpen() && !root.classList.contains("lift")) return;
    panel.hidden = true;
    root.classList.remove("open");
    setLift(false);
    stack.setAttribute("aria-expanded", "false");
    fit();
  };
  const topEdge = () => {
    const top = document.querySelector("header.top");
    return top ? Math.max(0, top.getBoundingClientRect().bottom) : 0;
  };
  const fit = () => {
    if (!stack.isConnected) return;
    const room = Math.max(60, Math.floor(stack.getBoundingClientRect().bottom - topEdge() - 6));
    const avail = isOpen() ? Math.min(room, 132) : room;
    stack.style.setProperty("--cdq-max", `${avail}px`);
    stack.classList.toggle("deep", cases.offsetHeight + 2 > avail);
    panel.style.setProperty("--cdq-h", `${Math.min(cases.offsetHeight, avail)}px`);
  };
  const open = () => {
    panel.style.setProperty("--qp-top", `${topEdge() + 2}px`);
    panel.hidden = false;
    root.classList.add("open");
    stack.setAttribute("aria-expanded", "true");
    fit();
    paintList();
    list.scrollTop = 0;
    closeB.focus({ preventScroll: true });
  };

  const opB = (label, glyph, fn, off = false) => h("button", { type: "button", class: "qp-op", disabled: off, "aria-label": label, title: label, onclick: (e) => { e.stopPropagation(); fn(); } }, glyph);

  const row = (q, i, s, n, ops) => {
    const li = h("li", {},
      h("span", { class: "s-no" }, String(n - i)),
      h("span", { class: "s-meta" },
        h("span", { class: "s-title" }, s.title || t("player.unknownTitle")),
        s.artist ? h("span", { class: "s-sub" }, s.artist) : null),
      h("span", { class: "s-dur" }, s.duration ? fmtTime(+s.duration) : ""),
      h("span", { class: "ops" }, ops));
    li.tabIndex = 0;
    li.addEventListener("click", () => jump(n));
    li.addEventListener("keydown", (e) => { if (e.key === "Enter" && e.target === li) jump(n); });
    return li;
  };
  const paintList = () => {
    const { q, i, up } = upcoming();
    const first = i + 1;
    const last = i + up.length;
    count.textContent = t("queue.tracks", { n: up.length });
    clearB.disabled = !up.length;
    const kids = [];
    const rm = (n) => opB(t("queue.remove"), "✕", () => removeAt([n]));
    q.forEach((s, n) => {
      if (n < first || n > last) return;
      kids.push(row(q, i, s, n, [
        opB(t("queue.up"), "▲", () => move(n, n - 1), n === first),
        opB(t("queue.down"), "▼", () => move(n, n + 1), n === last),
        rm(n),
      ]));
    });
    if (!up.length) kids.push(h("li", { class: "qp-sec qp-empty" }, t("queue.empty")));
    list.replaceChildren(...kids);
  };

  const tilt = (k) => {
    let x = 0;
    for (const ch of k) x = (x * 31 + ch.charCodeAt(0)) >>> 0;
    return ["", " alt", " alt2"][x % 3];
  };

  const paint = () => {
    const { q, i, up } = upcoming();
    const sig = q.map((s) => s.id + (entryInfo(s).manual ? "+" + entryInfo(s).batch : "")).join(",") + "|" + i;
    if (sig === lastSig) return;
    lastSig = sig;
    const runs = [];
    for (const s of up) {
      const { batch: b, size } = entryInfo(s);
      const last = runs[runs.length - 1];
      if (last && last.batch === b) last.n++;
      else runs.push({ batch: b, song: s, n: 1, size, key: b + ":" + s.id });
    }
    const room = Math.max(6, Math.ceil(window.innerHeight / 7) + 4);
    const shown = Math.min(runs.length, room);
    const kids = [];
    let below = "";
    if (!shown) kids.push(h("span", { class: "case empty" }));
    for (let n = 0; n < shown; n++) {
      const r = runs[shown - 1 - n];
      const c = h("span", { class: "case" + (r.size > 1 ? " album" : " single") + tilt(r.key) + (n === shown - 1 ? " top" : "") });
      below = caseColor(r.key, below);
      c.style.setProperty("--c", below);
      c.dataset.key = r.key;
      kids.push(c);
    }
    cases.replaceChildren(...kids);
    fit();
    const label = up.length ? t("queue.open", { n: up.length }) : `${t("queue.title")}: ${t("queue.empty")}`;
    stack.setAttribute("aria-label", label);
    stack.title = label;
    if (lastRuns && up.length > lastUp && !calm()) {
      const added = runs.slice(0, shown).find((r) => !lastRuns.has(r.key));
      const el = added && [...cases.children].find((c) => c.dataset.key === added.key);
      if (el) { el.classList.add("drop"); el.addEventListener("animationend", () => el.classList.remove("drop"), { once: true }); }
    }
    lastRuns = new Set(runs.map((r) => r.key));
    lastUp = up.length;
    if (!q.length) close();
    else if (isOpen()) paintList();
  };

  stack.addEventListener("click", (e) => {
    e.stopPropagation();
    if (isOpen()) { close(); return; }
    open();
  });
  closeB.addEventListener("click", (e) => { e.stopPropagation(); close(); stack.focus({ preventScroll: true }); });
  clearB.addEventListener("click", (e) => {
    e.stopPropagation();
    clearManual();
  });
  panel.addEventListener("click", (e) => e.stopPropagation());
  document.addEventListener("click", (e) => { if (!root.contains(e.target)) close(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && (isOpen() || root.classList.contains("lift"))) { close(); stack.focus({ preventScroll: true }); } });

  onChange(paint);
  paint();
  window.addEventListener("resize", fit);
  requestAnimationFrame(fit);
  return root;
}
