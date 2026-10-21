import { t } from "./i18n.js";
import { h, coverURL, fmtTime } from "./dom.js";
import { heartButton, retarget } from "./favs.js";
import { netMonitor, netChip } from "./net.js";
import { renderViz } from "./viz.js";

const SAVER_KEY = "bm.dataSaver";
const QUALITY_KEY = "bm.quality";
const FORMAT_KEY = "bm.originalFormat";
const PRELOAD_KEY = "bm.preload";
export const PRELOADS = ["on", "off"];
export const QUALITIES = ["auto", "original", "normal", "low"];
export const FORMATS = ["flac", "mp3"];
const TIERS = ["original", "normal", "low"];

const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch {} },
};

const audio = new Audio();
audio.preload = "auto";
const preloader = new Audio();
preloader.preload = "auto";
preloader.muted = true;

const state = {
  queue: [],
  index: -1,
  shuffle: false,
  repeat: "off",
  quality: (() => {
    const q = store.get(QUALITY_KEY);
    if (QUALITIES.includes(q)) return q;
    return store.get(SAVER_KEY) === "1" ? "normal" : "auto";
  })(),
  format: FORMATS.includes(store.get(FORMAT_KEY)) ? store.get(FORMAT_KEY) : "flac",
  preload: store.get(PRELOAD_KEY) === "off" ? "off" : "on",
  scrobbled: false,
  order: [],
};

const listeners = new Set();
const emit = () => listeners.forEach((fn) => fn());
export const onChange = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };
export const nowAlbum = () => (state.queue[state.index] || {}).albumId || "";
const syncNow = () => {
  const id = nowAlbum();
  document.documentElement.classList.toggle("is-playing", !audio.paused && !!id);
  document.querySelectorAll(".jc[data-album]").forEach((el) => el.classList.toggle("now", !!id && el.dataset.album === id));
};
listeners.add(syncNow);
for (const ev of ["play", "pause", "emptied", "ended"]) audio.addEventListener(ev, syncNow);

export const opusSupported = (() => {
  try { return audio.canPlayType("audio/ogg; codecs=opus") !== ""; } catch { return false; }
})();

const auto = { tier: 0, speed: 0, measuredKbps: 0, ups: 0, stalls: 0, next: null };

function streamSpec(song, tier) {
  if (tier === "normal") return opusSupported ? { format: "opus", kbps: 128 } : { format: "mp3", kbps: 192 };
  if (tier === "low") return opusSupported ? { format: "opus", kbps: 80 } : { format: "mp3", kbps: 128 };
  if (state.format === "mp3") return { format: "mp3", kbps: 320 };
  const known = +song.bitRate > 0;
  return { format: "", kbps: known ? +song.bitRate : 1000, known, suffix: song.suffix || "" };
}

export function qualityHint(q) {
  if (q === "auto") return t("quality.byConnection");
  if (q === "original") return state.format === "mp3" ? "MP3 320" : t("quality.originalFile");
  const spec = streamSpec({}, q);
  return `${spec.format === "opus" ? "Opus" : "MP3"} ${spec.kbps}`;
}

function streamURL(song, spec) {
  const base = `/api/music/stream/${encodeURIComponent(song.id)}`;
  return spec.format ? `${base}?format=${spec.format}&maxBitRate=${spec.kbps}` : base;
}

function decideAuto(song) {
  let t = auto.tier;
  if (auto.stalls > 0) {
    auto.stalls = 0;
    auto.ups = 0;
    return Math.min(t + 1, TIERS.length - 1);
  }
  if (!auto.speed) return t;
  const throughput = auto.speed * auto.measuredKbps;
  if (throughput < 1.5 * streamSpec(song, TIERS[t]).kbps && t < TIERS.length - 1) {
    auto.ups = 0;
    return t + 1;
  }
  if (t > 0 && throughput > 4 * streamSpec(song, TIERS[t - 1]).kbps) {
    auto.ups += 1;
    if (auto.ups >= 2) { auto.ups = 0; return t - 1; }
  } else auto.ups = 0;
  return t;
}

function pickTier(song, commit) {
  if (state.quality !== "auto") return state.quality;
  if (auto.next && auto.next.id === song.id) {
    const t = auto.next.tier;
    if (commit) { auto.tier = t; auto.next = null; }
    return TIERS[t];
  }
  const t = decideAuto(song);
  if (commit) auto.tier = t;
  else auto.next = { id: song.id, tier: t };
  return TIERS[t];
}

const meter = { t0: 0, b0: 0, done: false, playing: null, started: false };
const play = { offset: 0, tries: 0, blob: "", heard: 0, heardAt: 0, gen: 0, okSince: 0, triedAt: 0 };
const RETRY_DELAYS = [0, 1500, 4000, 8000];
const STALL_MS = 15000;
const stall = { t: 0, b: 0, at: 0 };
const watch = { t: -1, at: 0, stalled: false, sent: 0 };
const pre = { url: "", blob: "", ctrl: null, speed: 0 };
const startT = { at: 0, gen: 0, waits: 0, preload: false, auto: false, live: false };
let autoAdvance = false;
const adv = { gen: 0, stuck: false, retried: false, logged: false, fails: 0 };

function bufferedEnd() {
  const b = audio.buffered;
  return b.length ? b.end(b.length - 1) : 0;
}

function measure() {
  if (meter.done || !meter.playing || !meter.t0) return;
  const now = performance.now();
  const secs = (now - meter.t0) / 1000;
  const ahead = bufferedEnd() - meter.b0;
  const d = duration();
  const full = d > 0 && play.offset + bufferedEnd() >= d - 0.5;
  if (secs >= 2 || full) {
    auto.speed = Math.max(0.01, ahead / Math.max(secs, 0.25));
    auto.measuredKbps = meter.playing.kbps;
  }
  if (full) meter.done = true;
}

export const current = () => state.queue[state.index] || null;
const net = netMonitor(audio, () => {
  const p = meter.playing || {};
  return {
    speedKbps: auto.speed && auto.measuredKbps ? auto.speed * auto.measuredKbps : 0,
    kbps: p.kbps || 0, format: p.format || "", known: !!p.known, full: meter.done,
    ahead: Math.max(0, bufferedEnd() - (audio.currentTime || 0)),
    down: state.quality === "auto" && !!p.tier && p.tier !== "original",
  };
});
export const queue = () => state.queue.slice();
export const isPlaying = () => !audio.paused;
export const duration = () => {
  const d = audio.duration;
  if (isFinite(d) && d > 0) return play.offset + d;
  const song = current();
  return song && song.duration > 0 ? +song.duration : 0;
};
export const position = () => play.offset + (audio.currentTime || 0);

const tags = new WeakMap();
let batches = 0;
export const entryInfo = (song) => tags.get(song) || { manual: false, batch: 0, size: 0 };
const isManual = (i) => !!(state.queue[i] && tags.has(state.queue[i]));
const tagged = (songs) => {
  const batch = ++batches;
  return songs.map((s) => { const e = { ...s }; tags.set(e, { manual: true, batch, size: songs.length }); return e; });
};

function rebuildOrder() {
  const n = state.queue.length;
  state.order = [...Array(n).keys()];
  if (!state.shuffle) return;
  const head = [];
  for (let i = state.index + 1; isManual(i); i++) head.push(i);
  const rest = state.order.filter((i) => i !== state.index && !head.includes(i));
  for (let i = rest.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [rest[i], rest[j]] = [rest[j], rest[i]];
  }
  state.order = state.index >= 0 ? [state.index, ...head, ...rest] : rest;
}

function step(dir) {
  const pos = state.order.indexOf(state.index);
  let next = pos + dir;
  if (next >= state.order.length || next < 0) {
    if (state.repeat !== "all") return -1;
    next = (next + state.order.length) % state.order.length;
  }
  return state.order[next];
}

const listen = { song: null, secs: 0, last: -1, sent: 0 };

function report(playing) {
  const song = listen.song;
  if (!song) return;
  listen.sent = Date.now();
  const body = JSON.stringify({ seconds: Math.floor(listen.secs), playing, title: song.title || "", artist: song.artist || "" });
  const url = `/api/music/progress/${encodeURIComponent(song.id)}`;
  if (!playing && document.visibilityState === "hidden" && navigator.sendBeacon) {
    navigator.sendBeacon(url, new Blob([body], { type: "application/json" }));
    return;
  }
  fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
}

function startStream(song, tier, from = 0) {
  const spec = streamSpec(song, tier);
  const base = streamURL(song, spec);
  const off = spec.format && from >= 1 ? Math.floor(from) : 0;
  let blob = "";
  if (!off && pre.blob && pre.url === base) {
    blob = pre.blob;
    Object.assign(pre, { url: "", blob: "" });
  }
  const stale = play.blob && play.blob !== blob ? play.blob : "";
  play.blob = blob;
  play.offset = off;
  play.heard = Math.max(0, from - off);
  play.heardAt = Date.now();
  play.gen += 1;
  watch.t = -1;
  listen.last = -1;
  const seekTo = from - off;
  Object.assign(meter, { t0: performance.now(), b0: Math.max(0, seekTo), done: !!blob, started: false, playing: { ...spec, tier } });
  if (blob && pre.speed) { auto.speed = pre.speed; auto.measuredKbps = spec.kbps; }
  audio.src = blob || (off ? `${base}&timeOffset=${off}` : base);
  if (stale) URL.revokeObjectURL(stale);
  const gen = play.gen;
  if (seekTo > 0.5) audio.addEventListener("loadedmetadata", () => { if (play.gen === gen) audio.currentTime = seekTo; }, { once: true });
}

function load(i, autoplay = true) {
  if (listen.song) report(false);
  state.index = i;
  Object.assign(listen, { song: current(), secs: 0, last: -1, sent: 0 });
  state.scrobbled = false;
  play.tries = 0;
  play.triedAt = 0;
  const song = current();
  if (!song) { audio.removeAttribute("src"); audio.load(); emit(); return; }
  startStream(song, pickTier(song, true));
  const preloaded = !!play.blob || (!!preloader.src && preloader.src === audio.src);
  dropPreload(audio.src);
  startT.live = false;
  if (autoplay) {
    Object.assign(startT, { at: performance.now(), gen: play.gen, waits: 0, preload: preloaded, auto: autoAdvance, live: true });
    adv.stuck = false;
    adv.retried = false;
    adv.gen = autoAdvance ? play.gen : 0;
    const hidden = document.visibilityState === "hidden";
    audio.play().catch((e) => {
      startT.live = false;
      if (adv.gen !== play.gen || !adv.gen) return;
      adv.stuck = true;
      if (hidden) logEvent("advance-refused", e && e.name);
    });
  }
  fetch(`/api/music/scrobble/${encodeURIComponent(song.id)}?submission=false`, { method: "POST" }).catch(() => {});
  updateSession();
  syncSession();
  emit();
}

export function playList(songs, start = 0) {
  state.queue = songs.slice();
  state.index = start;
  rebuildOrder();
  load(start);
}

function enqueue(songs, atEnd) {
  if (!songs || !songs.length) return;
  const items = tagged(songs);
  if (state.index < 0) {
    state.queue = items;
    state.index = 0;
    rebuildOrder();
    load(0);
    return queued(songs);
  }
  let at = state.index + 1;
  if (atEnd) while (isManual(at)) at++;
  state.queue.splice(at, 0, ...items);
  rebuildOrder();
  emit();
  queued(songs);
}

export function playNext(songs) { enqueue(songs, false); }
export function addToQueue(songs) { enqueue(songs, true); }

const queued = (songs) => document.dispatchEvent(new CustomEvent("queueadd", { detail: { songs } }));

function removeWhere(drop) {
  const gone = state.queue.map((s, i) => drop(s, i));
  if (!gone.some(Boolean)) return;
  const cur = current();
  const curGone = state.index >= 0 && gone[state.index];
  const kept = gone.slice(0, Math.max(state.index, 0)).filter((g) => !g).length;
  state.queue = state.queue.filter((s, i) => !gone[i]);
  if (curGone) {
    const playing = !audio.paused;
    state.index = kept < state.queue.length ? kept : -1;
    rebuildOrder();
    load(state.index, playing);
    return;
  }
  state.index = state.queue.indexOf(cur);
  rebuildOrder();
  emit();
}

export function removeSongs(ids) {
  const gone = new Set(ids);
  removeWhere((s) => gone.has(s.id));
}
export function removeAt(indices) {
  const gone = new Set(indices);
  removeWhere((s, i) => gone.has(i));
}
export function clearManual() {
  removeWhere((s, i) => i > state.index && tags.has(s));
}

export function jump(i) { if (state.queue[i]) load(i); }
export function move(from, to) {
  const n = state.queue.length;
  if (from === to || from < 0 || to < 0 || from >= n || to >= n) return;
  const [song] = state.queue.splice(from, 1);
  state.queue.splice(to, 0, song);
  const i = state.index;
  if (i === from) state.index = to;
  else if (from < i && to >= i) state.index = i - 1;
  else if (from > i && to <= i) state.index = i + 1;
  rebuildOrder();
  emit();
}
export function toggle() { if (!current()) return; if (audio.paused) audio.play().catch(() => {}); else { startT.live = false; audio.pause(); } }
export function next() { const i = step(1); if (i >= 0) load(i); else { audio.pause(); emit(); } }
export function prev() {
  if (position() > 3) { seek(0); return; }
  const i = step(-1);
  if (i >= 0) load(i); else seek(0);
}
export function seek(t) {
  if (!isFinite(t)) return;
  const song = current();
  const local = t - play.offset;
  if (song && meter.playing && meter.playing.format && !play.blob && (local < 0 || local > bufferedEnd() + 1)) {
    const playing = !audio.paused;
    startStream(song, meter.playing.tier, Math.max(0, t));
    if (playing) audio.play().catch(() => {});
    emit();
    return;
  }
  audio.currentTime = Math.max(0, local);
}
export function toggleShuffle() { state.shuffle = !state.shuffle; rebuildOrder(); emit(); }
export function cycleRepeat() { state.repeat = { off: "all", all: "one", one: "off" }[state.repeat]; emit(); }
export const settings = () => ({ shuffle: state.shuffle, repeat: state.repeat, quality: state.quality, format: state.format, preload: state.preload });
export const streamInfo = () => (meter.playing && current() ? { ...meter.playing, auto: state.quality === "auto" } : null);
export function setFormat(f) {
  if (!FORMATS.includes(f)) return;
  state.format = f;
  store.set(FORMAT_KEY, f);
  if (meter.playing && meter.playing.tier === "original") restream("original");
  emit();
}
export function setPreload(m) {
  if (!PRELOADS.includes(m)) return;
  state.preload = m;
  store.set(PRELOAD_KEY, m);
  if (m === "off") dropPreload();
  emit();
}
function dropPreload(keep = "") {
  if (pre.ctrl) pre.ctrl.abort();
  if (pre.blob) URL.revokeObjectURL(pre.blob);
  Object.assign(pre, { url: "", blob: "", ctrl: null });
  if (preloader.src && preloader.src !== keep) { preloader.removeAttribute("src"); preloader.load(); }
}
export function stopAll() {
  listen.song = null;
  autoAdvance = false;
  play.gen += 1;
  Object.assign(adv, { gen: 0, stuck: false, retried: false, logged: false, fails: 0 });
  Object.assign(startT, { gen: 0, live: false });
  meter.playing = null;
  stall.at = 0;
  audio.pause();
  audio.removeAttribute("src");
  audio.load();
  if (play.blob) URL.revokeObjectURL(play.blob);
  Object.assign(play, { offset: 0, tries: 0, blob: "", heard: 0 });
  dropPreload();
  Object.assign(state, { queue: [], index: -1, order: [], scrobbled: false });
  if ("mediaSession" in navigator) { navigator.mediaSession.metadata = null; navigator.mediaSession.playbackState = "none"; }
  emit();
}
export function setQuality(q) {
  if (!QUALITIES.includes(q)) return;
  state.quality = q;
  store.set(QUALITY_KEY, q);
  auto.next = null;
  if (q !== "auto") restream(q);
  emit();
}
function restream(tier) {
  const song = current();
  if (song) {
    const t = position();
    const playing = !audio.paused;
    startStream(song, tier, t);
    if (playing) audio.play().catch(() => {});
  }
  emit();
}

function preloadNext() {
  const i = state.repeat === "one" ? -1 : step(1);
  const song = state.queue[i];
  if (!song || state.preload === "off") return;
  const spec = streamSpec(song, pickTier(song, false));
  const url = streamURL(song, spec);
  if (!spec.format) {
    if (!preloader.src.endsWith(url)) { preloader.src = url; preloader.load(); }
    return;
  }
  if (pre.url === url) return;
  if (pre.ctrl) pre.ctrl.abort();
  if (pre.blob) URL.revokeObjectURL(pre.blob);
  const ctrl = new AbortController();
  Object.assign(pre, { url, blob: "", ctrl });
  const t0 = performance.now();
  fetch(url, { signal: ctrl.signal })
    .then((res) => { if (!res.ok) throw new Error(String(res.status)); return res.blob(); })
    .then((b) => {
      if (pre.ctrl !== ctrl) return;
      if (b.size < 4096) throw new Error("short");
      const secs = Math.max((performance.now() - t0) / 1000, 0.25);
      Object.assign(pre, { ctrl: null, blob: URL.createObjectURL(b), speed: Math.max(0.01, (b.size * 8 / 1000 / secs) / spec.kbps) });
    })
    .catch(() => { if (pre.ctrl === ctrl) pre.ctrl = null; });
}

function updateSession() {
  if (!("mediaSession" in navigator)) return;
  const song = current();
  if (!song) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: song.title || "", artist: song.artist || "", album: song.album || "",
    artwork: (song.coverArt || song.albumId) ? [96, 256, 512].map((s) => ({ src: coverURL(song.coverArt || song.albumId, s), sizes: `${s}x${s}` })) : [],
  });
}

if ("mediaSession" in navigator) {
  const ms = navigator.mediaSession;
  const set = (a, fn) => { try { ms.setActionHandler(a, fn); } catch {} };
  set("play", () => audio.play().catch(() => {}));
  set("pause", () => { startT.live = false; audio.pause(); });
  set("previoustrack", prev);
  set("nexttrack", next);
  set("seekto", (d) => seek(d.seekTime));
}

function retryStream(song, at) {
  const delay = RETRY_DELAYS[play.tries] || 0;
  play.tries += 1;
  const gen = play.gen;
  let done = false;
  const go = () => {
    if (done || play.gen !== gen || current() !== song) return;
    done = true;
    const back = play.okSince >= play.triedAt ? 0.5 : 0;
    play.triedAt = Date.now();
    startStream(song, meter.playing.tier, Math.max(0, at - back));
    audio.play().catch(() => {});
    emit();
  };
  if (!delay) { go(); return; }
  setTimeout(() => {
    if (navigator.onLine !== false) { go(); return; }
    window.addEventListener("online", go, { once: true });
    setTimeout(go, 20000);
  }, delay);
}

function streamOver(failed, why = "") {
  const song = current();
  if (!song) return;
  const known = +song.duration > 0 ? +song.duration : 0;
  const t = audio.currentTime || 0;
  const at = play.offset + (heardOK(t) ? t : play.heard);
  const missing = known ? known - at > 3 : failed;
  if (why) logEvent(why, `ready ${audio.readyState} net ${audio.networkState} try ${play.tries}`);
  else if (failed && document.visibilityState === "hidden") logEvent("error", audio.error ? `code ${audio.error.code}` : "");
  if (meter.playing && missing && play.tries < RETRY_DELAYS.length) {
    retryStream(song, at);
    return;
  }
  if (!failed && state.repeat === "one") { seek(0); audio.play().catch(() => {}); return; }
  if (failed && ++adv.fails > 3) { adv.fails = 0; audio.pause(); emit(); return; }
  autoAdvance = true;
  try { next(); } finally { autoAdvance = false; }
}
audio.addEventListener("ended", () => streamOver(false));
audio.addEventListener("error", () => { if (audio.getAttribute("src")) streamOver(true); });
audio.addEventListener("canplay", () => {
  if (!adv.stuck || adv.retried || adv.gen !== play.gen || !audio.paused) return;
  adv.retried = true;
  audio.play().then(() => { adv.stuck = false; }).catch(() => {});
});
audio.addEventListener("playing", () => {
  adv.fails = 0;
  play.okSince = Date.now();
  if (!adv.gen || adv.gen !== play.gen) return;
  adv.gen = 0;
  adv.stuck = false;
  if (!adv.logged && document.visibilityState === "hidden") { adv.logged = true; logEvent("advance-ok", ""); }
});
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible" || !adv.stuck || adv.gen !== play.gen || !audio.paused) return;
  adv.stuck = false;
  audio.play().catch(() => {});
});
setInterval(() => {
  if (audio.paused || audio.seeking || play.blob || !current() || !audio.getAttribute("src") || audio.readyState >= 3) {
    stall.at = 0;
    return;
  }
  const t = audio.currentTime || 0;
  const b = bufferedEnd();
  if (!stall.at || t !== stall.t || b > stall.b + 0.05) { Object.assign(stall, { t, b, at: Date.now() }); return; }
  if (Date.now() - stall.at < STALL_MS) return;
  stall.at = 0;
  streamOver(true, "stall");
}, 2000);

function heardOK(t) {
  return t >= play.heard && t - play.heard < 2 + Math.max(0, Date.now() - play.heardAt) / 1000 * (audio.playbackRate || 1);
}

function agentFamily() {
  const ua = navigator.userAgent || "";
  const standalone = navigator.standalone === true || (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches);
  const os = /iPhone|iPad|iPod/.test(ua) ? "ios" : /Android/.test(ua) ? "android" : /Mac OS X/.test(ua) ? "mac" : /Windows/.test(ua) ? "windows" : /Linux/.test(ua) ? "linux" : "other";
  const br = /CriOS|Chrome\//.test(ua) ? "chrome" : /FxiOS|Firefox\//.test(ua) ? "firefox" : /Safari\//.test(ua) ? "safari" : "webview";
  return `${os}-${br}${standalone ? "-app" : ""}`;
}

function logEvent(event, detail) {
  const t = audio.currentTime || 0;
  reportJump(t, t, t, event, detail || "");
}

function reportJump(from, to, expected, event = "", detail = "") {
  if (watch.sent >= 20) return;
  watch.sent += 1;
  const song = current();
  const spec = meter.playing || {};
  const body = JSON.stringify({
    track: song ? song.id : "", format: spec.format || "original", kbps: spec.kbps || 0, quality: state.quality,
    from: play.offset + from, to: play.offset + to, expected: play.offset + expected, gap: to - expected,
    visibility: document.visibilityState, stalled: watch.stalled, buffered: play.offset + bufferedEnd(),
    ready: audio.readyState, blob: !!play.blob, offset: play.offset, agent: agentFamily(), event, detail,
  });
  fetch("/api/music/jumplog", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
}

function reportStart(ms) {
  const spec = meter.playing || {};
  const body = JSON.stringify({
    ms: Math.round(ms), waits: startT.waits, tier: spec.tier || "", format: spec.format || spec.suffix || "",
    kbps: spec.kbps || 0, quality: state.quality, preload: startT.preload, auto: startT.auto,
  });
  fetch("/api/music/startlog", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
}

audio.addEventListener("waiting", () => { if (startT.live) startT.waits += 1; });
audio.addEventListener("playing", () => {
  if (!startT.live) return;
  startT.live = false;
  if (startT.gen === play.gen) { const ms = performance.now() - startT.at; net.noteStart(ms); reportStart(ms); }
});

function checkJump() {
  const t = audio.currentTime || 0;
  const now = Date.now();
  if (audio.paused || audio.seeking) { watch.t = -1; return; }
  if (watch.t >= 0) {
    const expected = watch.t + Math.max(0, now - watch.at) / 1000 * (audio.playbackRate || 1);
    if (t < watch.t - 0.75 || t > expected + 0.75) reportJump(watch.t, t, expected);
  }
  watch.t = t;
  watch.at = now;
  watch.stalled = false;
}

function syncSession() {
  if (!("mediaSession" in navigator) || !current()) return;
  const ms = navigator.mediaSession;
  const stalled = audio.readyState < 3 && !audio.paused;
  try { ms.playbackState = audio.paused || stalled ? "paused" : "playing"; } catch {}
  const d = duration();
  if (!d || !ms.setPositionState) return;
  try { ms.setPositionState({ duration: d, position: Math.min(Math.max(0, position()), d), playbackRate: audio.playbackRate || 1 }); } catch {}
}

const rebase = () => { watch.t = -1; };
for (const ev of ["play", "pause", "seeking", "seeked", "emptied", "loadstart", "ratechange"]) audio.addEventListener(ev, rebase);
audio.addEventListener("playing", () => { watch.t = audio.currentTime || 0; watch.at = Date.now(); });
for (const ev of ["play", "pause", "playing", "waiting", "seeked", "ratechange", "loadedmetadata", "durationchange"]) audio.addEventListener(ev, syncSession);

audio.addEventListener("pause", () => { listen.last = -1; report(false); });
audio.addEventListener("play", () => { listen.last = audio.currentTime; report(true); });
audio.addEventListener("seeking", () => { listen.last = -1; });
audio.addEventListener("seeked", () => { play.heard = audio.currentTime; play.heardAt = Date.now(); });
audio.addEventListener("progress", measure);
audio.addEventListener("playing", () => { meter.started = true; });
audio.addEventListener("waiting", () => {
  watch.stalled = true;
  if (!meter.started || audio.seeking || state.quality !== "auto") return;
  auto.stalls += 1;
  auto.next = null;
});
const life = { closing: false };
function shutdown(e) {
  report(false);
  life.closing = true;
  play.gen += 1;
  autoAdvance = false;
  startT.live = false;
  adv.stuck = false;
  stall.at = 0;
  audio.pause();
  preloader.pause();
  if (!e.persisted) {
    audio.removeAttribute("src");
    audio.load();
    dropPreload();
  }
  if ("mediaSession" in navigator) {
    try { navigator.mediaSession.metadata = null; navigator.mediaSession.playbackState = "none"; } catch {}
  }
}
window.addEventListener("pagehide", shutdown);
window.addEventListener("pageshow", (e) => { if (e.persisted) life.closing = false; });
audio.addEventListener("play", () => { if (life.closing) audio.pause(); });
audio.addEventListener("timeupdate", () => {
  const song = current();
  if (!song) return;
  const t = audio.currentTime;
  checkJump();
  if (heardOK(t)) play.heard = t;
  if (play.tries && play.okSince && !audio.paused && Date.now() - play.okSince > 30000) play.tries = 0;
  play.heardAt = Date.now();
  if (listen.last >= 0 && t > listen.last && t - listen.last < 2) listen.secs += t - listen.last;
  listen.last = t;
  if (!audio.paused && Date.now() - listen.sent > 30000) report(true);
  const d = duration();
  const p = position();
  if (!state.scrobbled && d > 30 && (p >= d / 2 || p >= 240)) {
    state.scrobbled = true;
    fetch(`/api/music/scrobble/${encodeURIComponent(song.id)}`, { method: "POST" }).catch(() => {});
  }
  if (d && d - p < 30) preloadNext();
});
for (const ev of ["play", "pause", "loadedmetadata", "durationchange", "seeking", "seeked", "emptied"]) audio.addEventListener(ev, emit);

const vol = (() => {
  let v = 80, m = false;
  try { const n = parseInt(localStorage.getItem("bm.volume"), 10); if (n >= 0 && n <= 100) v = n; m = localStorage.getItem("bm.muted") === "1"; } catch {}
  return { v, m };
})();
function applyVolume() {
  audio.volume = vol.v / 100;
  audio.muted = vol.m;
  preloader.volume = vol.v / 100;
  try { localStorage.setItem("bm.volume", String(vol.v)); localStorage.setItem("bm.muted", vol.m ? "1" : "0"); } catch {}
}
applyVolume();
const volumeSettable = (() => {
  if (/iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1)) return false;
  try { const t = new Audio(); t.volume = 0.5; return t.volume === 0.5; } catch { return false; }
})();

const SVG = "http://www.w3.org/2000/svg";
const svgEl = (tag, attrs = {}) => {
  const el = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
};
const SETTING_TAGS = { auto: "AUTO", original: "ORIG", normal: "NORM", low: "LOW" };
const LOSSLESS = new Set(["flac", "wav", "alac", "ape", "wv", "aif", "aiff"]);
function streamTag(info) {
  const set = SETTING_TAGS[state.quality] || "AUTO";
  if (!info) return set;
  if (info.format) return `${set} · ${info.format.toUpperCase()} ${info.kbps}`;
  const suf = (info.suffix || "").toLowerCase();
  const name = suf ? suf.toUpperCase() : "FILE";
  return `${set} · ${name}${info.known && !LOSSLESS.has(suf) ? ` ${info.kbps}` : ""}`;
}

function volumeControl() {
  const btn = h("button", { type: "button", class: "pb-b sm vol", "aria-haspopup": "true", "aria-expanded": "false" });
  const range = h("input", { type: "range", min: 0, max: 100, step: 1, "aria-label": t("player.volumeLabel") });
  const mute = h("button", { type: "button", class: "pb-b sm vol mute" });
  const val = h("span", { class: "pb-vol-val" });
  const pop = h("div", { class: "pb-vol-pop", hidden: true }, mute, range, val);
  const wrap = h("div", { class: "pb-vol" }, btn, pop);
  const level = () => (vol.m || vol.v === 0 ? "mute" : vol.v < 50 ? "low" : "high");
  const paint = () => {
    btn.dataset.level = level();
    mute.dataset.level = vol.m ? "high" : "mute";
    const label = vol.m ? t("player.unmute") : volumeSettable ? t("player.volume", { v: vol.v }) : t("player.mutePhone");
    btn.title = volumeSettable ? label : vol.m ? t("player.unmutePhone") : t("player.mutePhone");
    btn.setAttribute("aria-label", volumeSettable ? (vol.m ? t("player.volumeMuted") : t("player.volume", { v: vol.v })) : btn.title);
    if (!volumeSettable) btn.setAttribute("aria-pressed", String(vol.m)); else btn.removeAttribute("aria-pressed");
    mute.title = vol.m ? t("player.unmute") : t("player.mute");
    mute.setAttribute("aria-label", mute.title);
    mute.setAttribute("aria-pressed", String(vol.m));
    range.value = String(vol.m ? 0 : vol.v);
    range.setAttribute("aria-valuenow", range.value);
    range.style.setProperty("--p", String(+range.value / 100));
    val.textContent = vol.m ? "MUTE" : String(vol.v);
  };
  const toggleMute = () => {
    if (vol.m || vol.v > 0 || !volumeSettable) vol.m = !vol.m; else { vol.v = 50; vol.m = false; }
    applyVolume(); paint();
  };
  const open = (on) => {
    pop.hidden = !on;
    btn.setAttribute("aria-expanded", String(on));
    if (on) { document.addEventListener("pointerdown", outside, true); document.addEventListener("keydown", esc, true); range.focus(); }
    else { document.removeEventListener("pointerdown", outside, true); document.removeEventListener("keydown", esc, true); }
  };
  const outside = (e) => { if (!wrap.contains(e.target)) open(false); };
  const esc = (e) => { if (e.key === "Escape") { e.preventDefault(); open(false); btn.focus(); } };
  btn.addEventListener("click", () => { if (!volumeSettable) toggleMute(); else open(pop.hidden); });
  mute.addEventListener("click", toggleMute);
  range.addEventListener("input", () => { vol.v = +range.value; vol.m = false; applyVolume(); paint(); });
  wrap.addEventListener("wheel", (e) => { if (!volumeSettable) return; e.preventDefault(); vol.v = Math.max(0, Math.min(100, vol.v + (e.deltaY < 0 ? 5 : -5))); vol.m = false; applyVolume(); paint(); }, { passive: false });
  if (!volumeSettable) btn.removeAttribute("aria-haspopup"), btn.removeAttribute("aria-expanded");
  paint();
  return wrap;
}

function fitMarquee(box, span) {
  box.classList.remove("run");
  if (box.childNodes.length > 1) box.lastChild.remove();
  const over = span.scrollWidth - box.clientWidth;
  if (over <= 1) return;
  box.append(h("span", { class: "mq-t", "aria-hidden": "true" }, span.textContent));
  box.classList.add("run");
  const w = span.offsetWidth;
  box.style.setProperty("--mq-w", `${w}px`);
  box.style.setProperty("--mq-steps", String(Math.max(8, Math.round(w / 6))));
  box.style.setProperty("--mq-dur", `${Math.max(6, w / 40).toFixed(1)}s`);
}

function setMarquee(box, span, text) {
  if (span.textContent === text && box.dataset.fit === String(box.clientWidth)) return;
  span.textContent = text;
  box.dataset.ghost = text.replace(/\S/g, "8");
  box.dataset.fit = String(box.clientWidth);
  fitMarquee(box, span);
  if (!box._mqObs && window.ResizeObserver) {
    let lastW = box.clientWidth;
    box._mqObs = new ResizeObserver(() => { if (box.clientWidth !== lastW) { lastW = box.clientWidth; box.dataset.fit = String(lastW); fitMarquee(box, span); } });
    box._mqObs.observe(box);
  }
}

export function renderBar(opts = {}) {
  const titleText = h("span", { class: "mq-t" });
  const title = h("div", { class: "pb-title mq" }, titleText);
  const sub = h("div", { class: "pb-sub" });
  const info = h("a", { class: "pb-info", "aria-label": t("player.openAlbum") }, title, sub);
  const pos = h("span", { class: "bar-time" }, "0:00");
  const dur = h("span", { class: "bar-time" }, "0:00");
  const range = h("input", { type: "range", min: 0, max: 0, step: "any", value: 0, "aria-label": t("player.seek") });
  let dragging = false;
  const paintSeek = () => { const m = +range.max; range.style.setProperty("--p", String(m > 0 ? Math.min(1, Math.max(0, +range.value / m)) : 0)); };
  range.addEventListener("input", () => { dragging = true; pos.textContent = fmtTime(+range.value); paintSeek(); });
  range.addEventListener("change", () => { dragging = false; seek(+range.value); });
  const btn = (label, cls, fn) => h("button", { type: "button", class: `pb-b ${cls}`, "aria-label": label, title: label, onclick: fn });
  const prevB = btn(t("player.previous"), "prev", prev);
  const playB = btn(t("player.play"), "play big", toggle);
  const nextB = btn(t("player.next"), "next", next);
  const shuffleB = btn(t("player.shuffle"), "sm shuffle", toggleShuffle);
  const repeatB = btn(t("player.repeat"), "sm repeat", cycleRepeat);
  const heartB = heartButton(null, "song", { cls: "pb-b sm bar-heart", get: current, onUnauthorized: opts.onUnauthorized });
  const rateB = h("button", { type: "button", class: "pb-rate", "aria-haspopup": "true" });
  rateB.addEventListener("click", (e) => { e.stopPropagation(); if (opts.onQuality) opts.onQuality(); });

  const labelImg = h("img", { alt: "", decoding: "async", draggable: "false" });
  const rim = svgEl("svg", { viewBox: "0 0 40 40", "aria-hidden": "true" });
  rim.append(
    svgEl("circle", { cx: 20, cy: 20, r: 20, fill: "#0b0c14" }),
    svgEl("circle", { cx: 20, cy: 20, r: 19.2, fill: "#ffffff" }),
    svgEl("circle", { cx: 20, cy: 20, r: 18.6, fill: "#0b0c14" }));
  const label = h("div", { class: "pb-label" }, labelImg, h("span", { class: "hole" }));
  const disc = h("button", { type: "button", class: "pb-disc" }, h("div", { class: "pb-spin" }, rim, label));

  const bar = h("footer", { class: "player pb", hidden: true },
    h("div", { class: "pb-bay" }, disc),
    h("div", { class: "pb-tab" }, prevB, playB, nextB),
    h("div", { class: "pb-main" },
      h("div", { class: "bar-seek" }, pos, range, dur),
      h("div", { class: "pb-left" },
        h("div", { class: "pb-screen" }, h("div", { class: "pb-chips" }, netChip(net), rateB), info),
        h("div", { class: "pb-btns" }, h("i", { class: "pb-grille" }), heartB, repeatB, shuffleB, volumeControl()))));

  bar.append(renderViz(audio, bar));

  const narrow = window.matchMedia ? window.matchMedia("(max-width: 760px)") : { matches: false };
  const setTucked = (on) => {
    if (bar.classList.contains("tucked") === on) return;
    bar.classList.toggle("tucked", on);
    paint();
  };
  disc.addEventListener("click", () => { if (bar.classList.contains("tucked")) setTucked(false); else toggle(); });
  let lastY = window.scrollY;
  let holdUntil = 0;
  window.addEventListener("bm:tuck", () => {
    if (!narrow.matches) return;
    holdUntil = Date.now() + 600;
    setTucked(true);
  });
  window.addEventListener("scroll", () => {
    const y = window.scrollY;
    if (Date.now() < holdUntil) { lastY = y; return; }
    if (!narrow.matches) setTucked(false);
    else if (y <= 0) { if (y < lastY - 6) setTucked(false); }
    else if (y > lastY + 6) setTucked(true);
    else if (y < lastY - 6) setTucked(false);
    lastY = y;
  }, { passive: true });

  const paintSpin = () => bar.classList.toggle("spinning", !audio.paused && audio.readyState >= 3 && !!current());
  for (const ev of ["play", "playing", "pause", "waiting", "stalled", "canplay", "emptied", "ended", "seeking", "seeked"]) audio.addEventListener(ev, paintSpin);

  let lastCover = null;
  const paint = () => {
    const song = current();
    bar.hidden = !song;
    paintSpin();
    if (!song) return;
    const name = song.title || t("player.unknownTitle");
    setMarquee(title, titleText, name);
    sub.textContent = [song.artist, song.album].filter(Boolean).join(" · ");
    const what = bar.classList.contains("tucked") ? t("player.showDisc") : isPlaying() ? t("player.pause") : t("player.play");
    disc.setAttribute("aria-label", `${what}: ${name}${song.artist ? `, ${song.artist}` : ""}`);
    if (heartB.dataset.id !== song.id) retarget(heartB, song);
    const ids = [...new Set([song.coverArt, song.albumId].filter(Boolean))];
    const key = ids.join("|");
    if (lastCover !== key) {
      lastCover = key;
      const tryNext = (i) => {
        if (i >= ids.length) { labelImg.removeAttribute("src"); labelImg.hidden = true; return; }
        labelImg.hidden = false;
        labelImg.onerror = () => tryNext(i + 1);
        labelImg.src = coverURL(ids[i], 256);
      };
      tryNext(0);
    }
    if (song.albumId) info.href = `#/library/album/${encodeURIComponent(song.albumId)}`;
    else info.removeAttribute("href");
    const si = streamInfo();
    rateB.textContent = streamTag(si);
    const down = !!si && si.auto && si.tier !== "original";
    rateB.classList.toggle("down", down);
    const tier = t(`quality.${si ? si.tier : "auto"}`);
    const tip = si ? `${t(si.auto ? "player.autoQuality" : "player.fixedQuality", { tier })}${si.format ? `, ${si.format} ${si.kbps} kbps` : `, ${t("quality.originalFile")}`}${down ? ` ${t("player.steppedDown")}` : ""}` : t("settings.quality");
    rateB.title = t("player.tapToChange", { tip });
    rateB.setAttribute("aria-label", t("player.rateLabel", { tag: rateB.textContent }));
    const s = settings();
    playB.classList.toggle("pause", isPlaying());
    playB.setAttribute("aria-label", isPlaying() ? t("player.pause") : t("player.play"));
    playB.title = isPlaying() ? t("player.pause") : t("player.play");
    shuffleB.classList.toggle("on", s.shuffle);
    shuffleB.setAttribute("aria-pressed", String(s.shuffle));
    repeatB.classList.toggle("on", s.repeat !== "off");
    repeatB.dataset.mode = s.repeat;
    repeatB.title = t(`player.repeat.${s.repeat}`);
    repeatB.setAttribute("aria-label", repeatB.title);
    const d = duration();
    const at = d ? Math.min(position(), d) : position();
    range.max = String(d);
    range.disabled = !d;
    dur.textContent = fmtTime(d);
    if (!dragging) { range.value = String(at); pos.textContent = fmtTime(at); }
    paintSeek();
  };
  onChange(paint);
  audio.addEventListener("timeupdate", paint);
  paint();
  return bar;
}
