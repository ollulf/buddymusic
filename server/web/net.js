import { t, relTime } from "./i18n.js";
import { h } from "./dom.js";

export const NET_LEVELS = ["good", "slow", "vslow", "offline"];
const RECOVER_MS = 30000;
const WINDOW_MS = 60000;

export function netMonitor(audio, info) {
  const s = { level: 0, calmSince: Date.now(), stalls: [], stallAt: 0, lastStall: 0, errors: 0, startMs: 0, startAt: 0 };
  const subs = new Set();
  const idle = () => audio.paused || !audio.getAttribute("src");

  const compute = () => {
    const now = Date.now();
    s.stalls = s.stalls.filter((x) => now - x < WINDOW_MS);
    const i = info();
    const speed = i.speedKbps || 0;
    const kbps = i.kbps || 0;
    if (navigator.onLine === false || s.errors >= 2) return 3;
    const conn = navigator.connection && navigator.connection.effectiveType;
    const longStall = s.stallAt && now - s.stallAt > 3000;
    if (longStall || s.stalls.length >= 2 || (speed && kbps && speed < kbps) || conn === "slow-2g" || conn === "2g") return 2;
    const thin = !audio.paused && (audio.currentTime || 0) > 15 && !i.full && i.ahead < 10;
    if (s.stalls.length >= 1 || thin || (speed && kbps && speed < 1.5 * kbps) || i.down || s.startMs > 3000 || conn === "3g") return 1;
    return 0;
  };

  const update = () => {
    const now = Date.now();
    if (idle() && !s.errors && navigator.onLine !== false) {
      s.calmSince = now;
      subs.forEach((fn) => fn());
      return;
    }
    const raw = compute();
    if (raw >= s.level || s.level === 3) {
      s.level = raw;
      s.calmSince = now;
    } else if (now - s.calmSince >= RECOVER_MS) {
      s.level -= 1;
      s.calmSince = now;
    }
    if (s.startMs && now - s.startAt > WINDOW_MS) s.startMs = 0;
    subs.forEach((fn) => fn());
  };

  audio.addEventListener("waiting", () => {
    if (audio.seeking || audio.paused || !(audio.currentTime > 0)) return;
    s.stallAt = Date.now();
    s.lastStall = s.stallAt;
    s.stalls.push(s.stallAt);
    update();
  });
  audio.addEventListener("playing", () => { s.stallAt = 0; s.errors = 0; update(); });
  audio.addEventListener("pause", () => { s.stallAt = 0; update(); });
  audio.addEventListener("error", () => { if (audio.getAttribute("src")) { s.errors += 1; update(); } });
  window.addEventListener("online", update);
  window.addEventListener("offline", update);
  setInterval(update, 3000);

  return {
    snapshot: () => ({ level: NET_LEVELS[s.level], idle: idle(), lastStall: s.lastStall, ...info() }),
    noteStart: (ms) => { s.startMs = ms; s.startAt = Date.now(); update(); },
    onChange: (fn) => { subs.add(fn); return () => subs.delete(fn); },
  };
}

const SVG = "http://www.w3.org/2000/svg";
const ARCS = [
  [[4, 7, 4, 1], [3, 8, 1, 1], [8, 8, 1, 1]],
  [[3, 4, 6, 1], [2, 5, 1, 1], [9, 5, 1, 1], [1, 6, 1, 1], [10, 6, 1, 1]],
  [[2, 1, 8, 1], [1, 2, 1, 1], [10, 2, 1, 1], [0, 3, 1, 1], [11, 3, 1, 1]],
];
const CROSS = [[8, 8], [11, 8], [9, 9], [10, 9], [9, 10], [10, 10], [8, 11], [11, 11]];
const ON = { good: 3, slow: 2, vslow: 1, offline: 0 };

function wifiIcon() {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "0 0 12 12");
  svg.setAttribute("shape-rendering", "crispEdges");
  svg.setAttribute("aria-hidden", "true");
  const rect = ([x, y, w, hh], cls) => {
    const r = document.createElementNS(SVG, "rect");
    r.setAttribute("x", x); r.setAttribute("y", y); r.setAttribute("width", w); r.setAttribute("height", hh);
    if (cls) r.setAttribute("class", cls);
    svg.append(r);
  };
  ARCS.forEach((parts, i) => parts.forEach((p) => rect(p, `a${i + 1}`)));
  rect([5, 10, 2, 2]);
  CROSS.forEach(([x, y]) => rect([x, y, 1, 1], "x"));
  return svg;
}

export function netChip(mon) {
  const word = h("span", { class: "w" });
  const btn = h("button", { type: "button", class: "pb-net", "aria-haspopup": "true", "aria-expanded": "false" }, wifiIcon(), word);
  const rows = h("dl");
  const pop = h("div", { class: "pb-net-pop", hidden: true, role: "dialog" }, rows);
  const wrap = h("div", { class: "pb-net-wrap" }, btn, pop);

  const fill = () => {
    const n = mon.snapshot();
    const quality = n.format ? `${n.format.toUpperCase()} ${n.kbps}` : n.kbps ? `${t("quality.originalFile")} ${n.known ? n.kbps : ""}`.trim() : "–";
    const row = (k, v) => [h("dt", {}, t(k)), h("dd", {}, v)];
    rows.replaceChildren(
      ...row("net.state", t(`net.${n.level}`)),
      ...row("net.speed", n.speedKbps ? t("net.kbps", { v: Math.round(n.speedKbps) }) : t("net.notMeasured")),
      ...row("net.quality", quality),
      ...row("net.lastStall", n.lastStall ? relTime(n.lastStall / 1000) : t("net.never")));
  };
  const paint = () => {
    const n = mon.snapshot();
    btn.dataset.level = n.level;
    btn.dataset.on = String(ON[n.level]);
    btn.classList.toggle("idle", n.idle);
    word.textContent = n.level === "good" ? "" : t(`net.short.${n.level}`);
    const label = t("net.label", { state: t(`net.${n.level}`) });
    btn.title = label;
    btn.setAttribute("aria-label", label);
    if (!pop.hidden) fill();
  };
  const open = (on) => {
    pop.hidden = !on;
    btn.setAttribute("aria-expanded", String(on));
    if (on) { fill(); document.addEventListener("pointerdown", outside, true); document.addEventListener("keydown", esc, true); }
    else { document.removeEventListener("pointerdown", outside, true); document.removeEventListener("keydown", esc, true); }
  };
  const outside = (e) => { if (!wrap.contains(e.target)) open(false); };
  const esc = (e) => { if (e.key === "Escape") { e.preventDefault(); open(false); btn.focus(); } };
  btn.addEventListener("click", (e) => { e.stopPropagation(); open(pop.hidden); });
  mon.onChange(paint);
  paint();
  return wrap;
}
