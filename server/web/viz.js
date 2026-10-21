const KEY = "bm.viz";
const BARS = 16;
const LV = 10;
const BK = 3;
const RANGES = [[1, 1], [2, 2], [3, 3], [4, 4], [5, 5], [6, 6], [7, 8], [9, 10], [11, 12], [13, 14], [15, 17], [18, 20], [21, 23], [24, 26], [27, 29], [30, 31]];
const FRAME_MS = 50;
const CALM_MS = 140;

export const vizRisky = /iPhone|iPad|iPod/.test(navigator.userAgent || "") || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);

const subs = new Set();
export function vizEnabled() {
  let v = null;
  try { v = localStorage.getItem(KEY); } catch {}
  return v === null ? true : v === "1";
}
export function setVizEnabled(on) {
  try { localStorage.setItem(KEY, on ? "1" : "0"); } catch {}
  subs.forEach((f) => f());
}
export const vizRouted = () => !!graph.analyser;

const graph = { ctx: null, analyser: null, data: null, pending: null };
const AC = window.AudioContext || window.webkitAudioContext;
const running = (ctx) => ctx.state === "running";

function connect(ctx, audio) {
  const src = ctx.createMediaElementSource(audio);
  const an = ctx.createAnalyser();
  an.fftSize = 64;
  an.smoothingTimeConstant = 0.55;
  src.connect(an);
  an.connect(ctx.destination);
  graph.ctx = ctx;
  graph.analyser = an;
  graph.data = new Uint8Array(an.frequencyBinCount);
}

function ensureGraph(audio) {
  if (graph.analyser) return Promise.resolve(true);
  if (graph.pending) return graph.pending;
  if (!AC || document.hidden || !vizEnabled()) return Promise.resolve(false);
  let ctx;
  try { ctx = new AC(); } catch { return Promise.resolve(false); }
  const start = running(ctx) ? Promise.resolve() : Promise.race([ctx.resume().catch(() => {}), new Promise((r) => setTimeout(r, 400))]);
  graph.pending = start.then(() => {
    graph.pending = null;
    if (graph.analyser) { ctx.close().catch(() => {}); return true; }
    if (!running(ctx) || document.hidden) { ctx.close().catch(() => {}); return false; }
    try { connect(ctx, audio); } catch { ctx.close().catch(() => {}); return false; }
    return true;
  });
  return graph.pending;
}

window.addEventListener("pagehide", (e) => {
  const ctx = graph.ctx;
  if (!ctx || ctx.state === "closed") return;
  if (e.persisted) ctx.suspend().catch(() => {});
  else ctx.close().catch(() => {});
});

function resume() {
  const ctx = graph.ctx;
  if (ctx && !running(ctx) && ctx.state !== "closed") ctx.resume().catch(() => {});
}

export function renderViz(audio, bar) {
  const canvas = document.createElement("canvas");
  canvas.className = "vz-lcd";
  canvas.setAttribute("aria-hidden", "true");
  const house = document.createElement("div");
  house.className = "vz-house";
  house.append(canvas);

  const g = canvas.getContext("2d");
  const reduced = window.matchMedia ? window.matchMedia("(prefers-reduced-motion: reduce)") : { matches: false, addEventListener() {} };
  const mq = (q) => window.matchMedia ? window.matchMedia(q) : { matches: false, addEventListener() {} };
  const compact = mq("(max-width: 420px)");
  const tiny = mq("(max-width: 375px)");
  const now = new Array(BARS).fill(0);
  const peak = new Array(BARS).fill(0);
  const hold = new Array(BARS).fill(0);
  let colours = null;
  let geo = null;
  let raf = 0;
  let last = 0;
  let beat = 0;

  const readColours = () => {
    const cs = getComputedStyle(house);
    const v = (n, d) => cs.getPropertyValue(n).trim() || d;
    colours = { on: v("--accent", "#ffcd75"), hot: v("--orange", "#ef7d57"), ghost: v("--vz-ghost", "rgba(255,205,117,.12)"), bg: v("--vz-bg", "#0b0c10"), pk: v("--fg", "#ffffff") };
  };

  const layout = () => {
    const bw = tiny.matches ? 3 : compact.matches ? 4 : 7;
    const gap = compact.matches ? 1 : 2;
    const w = BARS * bw + (BARS - 1) * gap;
    const hgt = LV * BK + (LV - 1);
    const s = Math.max(1, Math.round(window.devicePixelRatio || 1));
    canvas.width = w * s;
    canvas.height = hgt * s;
    canvas.style.width = `${w}px`;
    canvas.style.height = `${hgt}px`;
    geo = { bw, gap, hgt, s };
  };

  const draw = (calm) => {
    if (!geo) layout();
    if (!colours) readColours();
    const { bw, gap, hgt, s } = geo;
    g.setTransform(s, 0, 0, s, 0, 0);
    g.fillStyle = colours.bg;
    g.fillRect(0, 0, canvas.width, canvas.height);
    for (let i = 0; i < BARS; i++) {
      const x = i * (bw + gap);
      const n = Math.round(now[i] * LV);
      const p = Math.min(LV - 1, Math.round(peak[i] * LV));
      for (let k = 0; k < LV; k++) {
        const y = hgt - (k + 1) * BK - k;
        g.fillStyle = k < n ? (k >= LV - 2 ? colours.hot : colours.on) : !calm && k === p && p >= n && p > 0 ? colours.pk : colours.ghost;
        g.fillRect(x, y, bw, BK);
      }
    }
  };

  const sample = (calm) => {
    const an = graph.analyser;
    if (!an) return;
    an.getByteFrequencyData(graph.data);
    for (let i = 0; i < BARS; i++) {
      const [a, b] = RANGES[i];
      let m = 0;
      for (let j = a; j <= b && j < graph.data.length; j++) m = Math.max(m, graph.data[j]);
      const lv = Math.min(1, Math.pow(m / 255, 1.4) * 1.15);
      now[i] = calm ? now[i] + (lv - now[i]) * 0.5 : lv;
      if (lv >= peak[i]) { peak[i] = lv; hold[i] = 8; } else if (hold[i] > 0) hold[i]--; else peak[i] = Math.max(0, peak[i] - 0.06);
    }
  };

  const clear = () => { now.fill(0); peak.fill(0); hold.fill(0); };

  const live = () => vizEnabled() && !!graph.analyser && !audio.paused && !document.hidden && !house.hidden;

  const tick = (ts) => {
    raf = 0;
    beat = performance.now();
    if (!live()) { clear(); draw(); return; }
    const calm = reduced.matches;
    if (graph.ctx && !running(graph.ctx)) resume();
    if (ts - last >= (calm ? CALM_MS : FRAME_MS)) { last = ts; sample(calm); draw(calm); }
    raf = requestAnimationFrame(tick);
  };

  const sync = () => {
    const on = vizEnabled();
    house.hidden = !on;
    if (on && graph.analyser && !audio.paused && !document.hidden) resume();
    if (live()) { if (!raf) { last = 0; beat = performance.now(); raf = requestAnimationFrame(tick); } }
    else { if (raf) cancelAnimationFrame(raf); raf = 0; clear(); if (on) draw(); }
  };

  const keep = () => {
    if (!live()) return;
    if (raf && performance.now() - beat > 1000) { cancelAnimationFrame(raf); raf = 0; }
    if (!raf) sync();
    else if (graph.ctx && !running(graph.ctx)) resume();
  };

  const wire = () => {
    if (!vizEnabled() || document.hidden) return;
    if (graph.analyser) { resume(); return; }
    ensureGraph(audio).then((ok) => { if (ok) { resume(); sync(); } });
  };

  for (const ev of ["pointerdown", "pointerup", "touchend", "click", "keydown"]) document.addEventListener(ev, wire, true);
  for (const ev of ["play", "playing"]) audio.addEventListener(ev, wire);
  document.addEventListener("visibilitychange", () => { if (!document.hidden && !audio.paused) wire(); sync(); });
  subs.add(() => { if (!audio.paused) wire(); });

  for (const ev of ["play", "playing", "pause", "ended", "emptied"]) audio.addEventListener(ev, sync);
  for (const ev of ["timeupdate", "loadeddata", "canplay"]) audio.addEventListener(ev, keep);
  reduced.addEventListener("change", sync);
  compact.addEventListener("change", () => { layout(); draw(); });
  tiny.addEventListener("change", () => { layout(); draw(); });
  new MutationObserver(sync).observe(bar, { attributes: true, attributeFilter: ["class"] });
  new MutationObserver(() => { colours = null; draw(); }).observe(document.documentElement, { attributes: true, attributeFilter: ["data-key", "data-base"] });
  subs.add(sync);

  sync();
  return house;
}
