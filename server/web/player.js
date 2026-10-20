import { h, coverURL, fmtTime } from "./dom.js";
import { heartButton, retarget } from "./favs.js";

const SAVER_KEY = "bm.dataSaver";
const QUALITY_KEY = "bm.quality";
const FORMAT_KEY = "bm.originalFormat";
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
  scrobbled: false,
  order: [],
};

const listeners = new Set();
const emit = () => listeners.forEach((fn) => fn());
export const onChange = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };

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
  if (q === "auto") return "by connection";
  if (q === "original") return state.format === "mp3" ? "MP3 320" : "original file";
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
const play = { offset: 0, tries: 0, blob: "", heard: 0 };
const pre = { url: "", blob: "", ctrl: null, speed: 0 };

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
export const queue = () => state.queue.slice();
export const isPlaying = () => !audio.paused;
export const duration = () => {
  const d = audio.duration;
  if (isFinite(d) && d > 0) return play.offset + d;
  const song = current();
  return song && song.duration > 0 ? +song.duration : 0;
};
export const position = () => play.offset + (audio.currentTime || 0);

function rebuildOrder() {
  const n = state.queue.length;
  state.order = [...Array(n).keys()];
  if (!state.shuffle) return;
  const rest = state.order.filter((i) => i !== state.index);
  for (let i = rest.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [rest[i], rest[j]] = [rest[j], rest[i]];
  }
  state.order = state.index >= 0 ? [state.index, ...rest] : rest;
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
  if (play.blob && play.blob !== blob) URL.revokeObjectURL(play.blob);
  play.blob = blob;
  play.offset = off;
  play.heard = Math.max(0, from - off);
  listen.last = -1;
  const seekTo = from - off;
  Object.assign(meter, { t0: performance.now(), b0: Math.max(0, seekTo), done: !!blob, started: false, playing: { ...spec, tier } });
  if (blob && pre.speed) { auto.speed = pre.speed; auto.measuredKbps = spec.kbps; }
  audio.src = blob || (off ? `${base}&timeOffset=${off}` : base);
  if (seekTo > 0.5) audio.addEventListener("loadedmetadata", () => { audio.currentTime = seekTo; }, { once: true });
}

function load(i, autoplay = true) {
  if (listen.song) report(false);
  state.index = i;
  Object.assign(listen, { song: current(), secs: 0, last: -1, sent: 0 });
  state.scrobbled = false;
  play.tries = 0;
  const song = current();
  if (!song) { audio.removeAttribute("src"); audio.load(); emit(); return; }
  startStream(song, pickTier(song, true));
  if (autoplay) audio.play().catch(() => {});
  fetch(`/api/music/scrobble/${encodeURIComponent(song.id)}?submission=false`, { method: "POST" }).catch(() => {});
  updateSession();
  emit();
}

export function playList(songs, start = 0) {
  state.queue = songs.slice();
  state.index = start;
  rebuildOrder();
  load(start);
}

export function playNext(songs) {
  const at = state.index + 1;
  state.queue.splice(at, 0, ...songs);
  if (state.index < 0) return playList(state.queue, 0);
  rebuildOrder();
  emit();
}

export function addToQueue(songs) {
  if (state.index < 0) return playList(songs, 0);
  state.queue.push(...songs);
  rebuildOrder();
  emit();
}

export function removeSongs(ids) {
  const gone = new Set(ids);
  if (!state.queue.some((s) => gone.has(s.id))) return;
  const cur = current();
  const kept = state.queue.slice(0, Math.max(state.index, 0)).filter((s) => !gone.has(s.id)).length;
  state.queue = state.queue.filter((s) => !gone.has(s.id));
  if (cur && gone.has(cur.id)) {
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

export function jump(i) { if (state.queue[i]) load(i); }
export function toggle() { if (!current()) return; audio.paused ? audio.play().catch(() => {}) : audio.pause(); }
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
export const settings = () => ({ shuffle: state.shuffle, repeat: state.repeat, quality: state.quality, format: state.format });
export const streamInfo = () => (meter.playing && current() ? { ...meter.playing, auto: state.quality === "auto" } : null);
export function setFormat(f) {
  if (!FORMATS.includes(f)) return;
  state.format = f;
  store.set(FORMAT_KEY, f);
  if (meter.playing && meter.playing.tier === "original") restream("original");
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
  if (!song) return;
  if (state.quality === "auto" && (meter.playing ? !meter.done && auto.speed < 4 : false)) return;
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
  set("pause", () => audio.pause());
  set("previoustrack", prev);
  set("nexttrack", next);
  set("seekto", (d) => seek(d.seekTime));
}

audio.addEventListener("ended", () => {
  const song = current();
  const known = song && +song.duration > 0 ? +song.duration : 0;
  const t = audio.currentTime || 0;
  const at = play.offset + (t - play.heard > 2 ? play.heard : t);
  if (song && meter.playing && known - at > 3 && play.tries < 2) {
    play.tries += 1;
    startStream(song, meter.playing.tier, at);
    audio.play().catch(() => {});
    emit();
    return;
  }
  if (state.repeat === "one") { seek(0); audio.play().catch(() => {}); return; }
  next();
});
audio.addEventListener("error", () => { if (play.tries > 0 && current()) next(); });
audio.addEventListener("pause", () => { listen.last = -1; report(false); });
audio.addEventListener("play", () => { listen.last = audio.currentTime; report(true); });
audio.addEventListener("seeking", () => { listen.last = -1; });
audio.addEventListener("seeked", () => { play.heard = audio.currentTime; });
audio.addEventListener("progress", measure);
audio.addEventListener("playing", () => { meter.started = true; });
audio.addEventListener("waiting", () => {
  if (!meter.started || audio.seeking || state.quality !== "auto") return;
  auto.stalls += 1;
  auto.next = null;
});
window.addEventListener("pagehide", () => report(false));
audio.addEventListener("timeupdate", () => {
  const song = current();
  if (!song) return;
  const t = audio.currentTime;
  if (t >= play.heard && t - play.heard < 2) play.heard = t;
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

export function renderBar(opts = {}) {
  const art = h("a", { class: "bar-art", "aria-label": "Open album" });
  const title = h("div", { class: "bar-title" });
  const sub = h("div", { class: "bar-sub" });
  const pos = h("span", { class: "bar-time" }, "0:00");
  const dur = h("span", { class: "bar-time" }, "0:00");
  const range = h("input", { type: "range", min: 0, max: 0, step: "any", value: 0, "aria-label": "Seek" });
  let dragging = false;
  const paintSeek = () => { const m = +range.max; range.style.setProperty("--p", String(m > 0 ? Math.min(1, Math.max(0, +range.value / m)) : 0)); };
  range.addEventListener("input", () => { dragging = true; pos.textContent = fmtTime(+range.value); paintSeek(); });
  range.addEventListener("change", () => { dragging = false; seek(+range.value); });
  const btn = (label, cls, fn) => h("button", { class: `icon ${cls}`, "aria-label": label, title: label, onclick: fn });
  const shuffleB = btn("Shuffle", "shuffle", toggleShuffle);
  const prevB = btn("Previous", "prev", prev);
  const playB = btn("Play", "play big", toggle);
  const nextB = btn("Next", "next", next);
  const repeatB = btn("Repeat", "repeat", cycleRepeat);
  const rateB = h("span", { class: "bar-rate" });
  const heartB = heartButton(null, "song", { cls: "bar-heart", get: current, onUnauthorized: opts.onUnauthorized });
  const muteB = h("button", { class: "icon mute", type: "button" });
  const volR = h("input", { type: "range", min: 0, max: 100, step: 1, value: vol.v, "aria-label": "Volume" });
  const paintVol = () => {
    const off = vol.m || vol.v === 0;
    muteB.classList.toggle("off", off);
    muteB.classList.toggle("low", !off && vol.v < 50);
    muteB.setAttribute("aria-pressed", String(vol.m));
    muteB.setAttribute("aria-label", vol.m ? "Unmute" : "Mute");
    muteB.title = vol.m ? "Unmute" : "Mute";
    volR.value = vol.v;
    volR.style.setProperty("--fill", `${vol.m ? 0 : vol.v}%`);
  };
  muteB.addEventListener("click", () => {
    if (vol.m || vol.v > 0) vol.m = !vol.m; else { vol.v = 50; vol.m = false; }
    applyVolume(); paintVol();
  });
  volR.addEventListener("input", () => { vol.v = +volR.value; vol.m = false; applyVolume(); paintVol(); });
  paintVol();
  const volBox = h("div", { class: "bar-vol" + (volumeSettable ? "" : " fixed") }, muteB, volR);
  const bar = h("footer", { class: "player", hidden: true },
    h("div", { class: "bar-seek" }, pos, range, dur),
    h("div", { class: "bar-row" },
      art, h("div", { class: "bar-meta" }, title, sub), rateB, heartB,
      h("div", { class: "bar-ctl" }, shuffleB, prevB, playB, nextB, repeatB),
      volBox));

  let lastCover = null;
  const paint = () => {
    const song = current();
    bar.hidden = !song;
    if (!song) return;
    title.textContent = song.title || "Unknown title";
    sub.textContent = [song.artist, song.album].filter(Boolean).join(" · ");
    if (heartB.dataset.id !== song.id) retarget(heartB, song);
    const ids = [...new Set([song.coverArt, song.albumId].filter(Boolean))];
    const key = ids.join("|");
    if (lastCover !== key) {
      lastCover = key;
      const box = h("div", { class: "art small" });
      const tryNext = (i) => {
        if (i >= ids.length) { box.replaceChildren(); return; }
        const img = h("img", { src: coverURL(ids[i], 96), alt: song.album || "Album cover", decoding: "async" });
        img.addEventListener("error", () => tryNext(i + 1), { once: true });
        box.replaceChildren(img);
      };
      tryNext(0);
      art.replaceChildren(box);
    }
    if (song.albumId) art.href = `#/library/album/${encodeURIComponent(song.albumId)}`;
    else art.removeAttribute("href");
    const info = streamInfo();
    rateB.hidden = !info;
    if (info) {
      rateB.textContent = info.format ? `${info.format.toUpperCase()} ${info.kbps}` : [info.suffix ? info.suffix.toUpperCase() : "ORIG", info.known ? info.kbps : ""].filter(Boolean).join(" ");
      const down = info.auto && info.tier !== "original";
      rateB.classList.toggle("down", down);
      rateB.title = `${info.auto ? "Auto" : "Fixed"} quality: ${info.tier}${info.format ? `, ${info.format} ${info.kbps} kbps` : ", original file"}${down ? " (stepped down for a slow connection)" : ""}`;
    }
    const s = settings();
    playB.classList.toggle("pause", isPlaying());
    playB.setAttribute("aria-label", isPlaying() ? "Pause" : "Play");
    shuffleB.classList.toggle("on", s.shuffle);
    repeatB.classList.toggle("on", s.repeat !== "off");
    repeatB.dataset.mode = s.repeat;
    repeatB.title = `Repeat: ${s.repeat}`;
    const d = duration();
    const t = d ? Math.min(position(), d) : position();
    range.max = String(d);
    range.disabled = !d;
    dur.textContent = fmtTime(d);
    if (!dragging) { range.value = String(t); pos.textContent = fmtTime(t); }
    paintSeek();
    if ("mediaSession" in navigator && d) {
      try { navigator.mediaSession.setPositionState({ duration: d, position: t, playbackRate: 1 }); } catch {}
    }
  };
  onChange(paint);
  audio.addEventListener("timeupdate", paint);
  paint();
  return bar;
}
