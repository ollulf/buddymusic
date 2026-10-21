const KEY = "bm.viz";
const BARS = 16;
const LV = 10;
const BK = 3;
const RANGES = [[1, 1], [2, 2], [3, 3], [4, 4], [5, 5], [6, 6], [7, 8], [9, 10], [11, 12], [13, 14], [15, 17], [18, 20], [21, 23], [24, 26], [27, 29], [30, 31]];
const FRAME_MS = 50;

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

const graph = { ctx: null, analyser: null, data: null };

function ensureGraph(audio) {
  if (graph.analyser) return true;
  const AC = window.AudioContext || window.webkitAudioContext;
  if (!AC) return false;
  try {
    const ctx = new AC();
    const src = ctx.createMediaElementSource(audio);
    const an = ctx.createAnalyser();
    an.fftSize = 64;
    an.smoothingTimeConstant = 0.55;
    src.connect(an);
    an.connect(ctx.destination);
    graph.ctx = ctx;
    graph.analyser = an;
    graph.data = new Uint8Array(an.frequencyBinCount);
  } catch {
    return false;
  }
  return true;
}

function resume() {
  if (graph.ctx && graph.ctx.state === "suspended") graph.ctx.resume().catch(() => {});
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

  const draw = () => {
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
        g.fillStyle = k < n ? (k >= LV - 2 ? colours.hot : colours.on) : k === p && p >= n && p > 0 ? colours.pk : colours.ghost;
        g.fillRect(x, y, bw, BK);
      }
    }
  };

  const sample = () => {
    const an = graph.analyser;
    if (!an) return;
    an.getByteFrequencyData(graph.data);
    for (let i = 0; i < BARS; i++) {
      const [a, b] = RANGES[i];
      let m = 0;
      for (let j = a; j <= b && j < graph.data.length; j++) m = Math.max(m, graph.data[j]);
      const lv = Math.min(1, Math.pow(m / 255, 1.4) * 1.15);
      now[i] = lv;
      if (lv >= peak[i]) { peak[i] = lv; hold[i] = 8; } else if (hold[i] > 0) hold[i]--; else peak[i] = Math.max(0, peak[i] - 0.06);
    }
  };

  const clear = () => { now.fill(0); peak.fill(0); hold.fill(0); };

  const live = () => vizEnabled() && !!graph.analyser && !audio.paused && !document.hidden && !bar.classList.contains("tucked") && !reduced.matches && !house.hidden;

  const tick = (ts) => {
    raf = 0;
    if (!live()) { clear(); draw(); return; }
    if (ts - last >= FRAME_MS) { last = ts; sample(); draw(); }
    raf = requestAnimationFrame(tick);
  };

  const sync = () => {
    const on = vizEnabled();
    house.hidden = !on;
    if (on && graph.analyser) resume();
    if (live()) { if (!raf) raf = requestAnimationFrame(tick); }
    else { if (raf) cancelAnimationFrame(raf); raf = 0; clear(); if (on) draw(); }
  };

  const gesture = () => {
    if (!vizEnabled()) return;
    if (ensureGraph(audio)) {
      resume();
      document.removeEventListener("pointerdown", gesture, true);
      document.removeEventListener("keydown", gesture, true);
      sync();
    }
  };
  document.addEventListener("pointerdown", gesture, true);
  document.addEventListener("keydown", gesture, true);

  for (const ev of ["play", "playing", "pause", "ended", "emptied"]) audio.addEventListener(ev, () => { if (ev === "play") resume(); sync(); });
  document.addEventListener("visibilitychange", sync);
  reduced.addEventListener("change", sync);
  compact.addEventListener("change", () => { layout(); draw(); });
  tiny.addEventListener("change", () => { layout(); draw(); });
  new MutationObserver(sync).observe(bar, { attributes: true, attributeFilter: ["class"] });
  new MutationObserver(() => { colours = null; draw(); }).observe(document.documentElement, { attributes: true, attributeFilter: ["data-key", "data-base"] });
  subs.add(sync);

  sync();
  return house;
}
