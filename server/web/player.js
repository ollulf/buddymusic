import { h, coverURL, cover, fmtTime } from "./dom.js";

const SAVER_KEY = "bm.dataSaver";

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
  saver: store.get(SAVER_KEY) === "1",
  scrobbled: false,
  order: [],
};

const listeners = new Set();
const emit = () => listeners.forEach((fn) => fn());
export const onChange = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };

const streamURL = (song) => {
  const base = `/api/music/stream/${encodeURIComponent(song.id)}`;
  return state.saver ? `${base}?format=opus&maxBitRate=128` : base;
};

export const current = () => state.queue[state.index] || null;
export const queue = () => state.queue.slice();
export const isPlaying = () => !audio.paused;

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

function load(i, autoplay = true) {
  state.index = i;
  state.scrobbled = false;
  const song = current();
  if (!song) { audio.removeAttribute("src"); audio.load(); emit(); return; }
  audio.src = streamURL(song);
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
  if (audio.currentTime > 3) { audio.currentTime = 0; return; }
  const i = step(-1);
  if (i >= 0) load(i); else audio.currentTime = 0;
}
export function seek(t) { if (isFinite(t)) audio.currentTime = t; }
export function toggleShuffle() { state.shuffle = !state.shuffle; rebuildOrder(); emit(); }
export function cycleRepeat() { state.repeat = { off: "all", all: "one", one: "off" }[state.repeat]; emit(); }
export const settings = () => ({ shuffle: state.shuffle, repeat: state.repeat, saver: state.saver });
export function setSaver(on) {
  state.saver = on;
  store.set(SAVER_KEY, on ? "1" : "0");
  const song = current();
  if (song) {
    const t = audio.currentTime;
    const playing = !audio.paused;
    audio.src = streamURL(song);
    audio.addEventListener("loadedmetadata", () => { audio.currentTime = t; }, { once: true });
    if (playing) audio.play().catch(() => {});
  }
  emit();
}

function preloadNext() {
  const i = state.repeat === "one" ? -1 : step(1);
  const song = state.queue[i];
  const url = song ? streamURL(song) : "";
  if (url && !preloader.src.endsWith(url)) { preloader.src = url; preloader.load(); }
}

function updateSession() {
  if (!("mediaSession" in navigator)) return;
  const song = current();
  if (!song) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: song.title || "", artist: song.artist || "", album: song.album || "",
    artwork: song.coverArt ? [96, 256, 512].map((s) => ({ src: coverURL(song.coverArt, s), sizes: `${s}x${s}` })) : [],
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
  if (state.repeat === "one") { audio.currentTime = 0; audio.play().catch(() => {}); return; }
  next();
});
audio.addEventListener("timeupdate", () => {
  const song = current();
  if (!song) return;
  const d = audio.duration || song.duration || 0;
  if (!state.scrobbled && d > 30 && (audio.currentTime >= d / 2 || audio.currentTime >= 240)) {
    state.scrobbled = true;
    fetch(`/api/music/scrobble/${encodeURIComponent(song.id)}`, { method: "POST" }).catch(() => {});
  }
  if (d && d - audio.currentTime < 30) preloadNext();
});
for (const ev of ["play", "pause", "loadedmetadata"]) audio.addEventListener(ev, emit);

export function renderBar() {
  const art = h("div", { class: "bar-art" });
  const title = h("div", { class: "bar-title" });
  const sub = h("div", { class: "bar-sub" });
  const pos = h("span", { class: "bar-time" }, "0:00");
  const dur = h("span", { class: "bar-time" }, "0:00");
  const range = h("input", { type: "range", min: 0, max: 0, step: "any", value: 0, "aria-label": "Seek" });
  let dragging = false;
  range.addEventListener("input", () => { dragging = true; pos.textContent = fmtTime(+range.value); });
  range.addEventListener("change", () => { dragging = false; seek(+range.value); });
  const btn = (label, cls, fn) => h("button", { class: `icon ${cls}`, "aria-label": label, title: label, onclick: fn });
  const shuffleB = btn("Shuffle", "shuffle", toggleShuffle);
  const prevB = btn("Previous", "prev", prev);
  const playB = btn("Play", "play big", toggle);
  const nextB = btn("Next", "next", next);
  const repeatB = btn("Repeat", "repeat", cycleRepeat);
  const saver = h("input", { type: "checkbox" });
  saver.addEventListener("change", () => setSaver(saver.checked));
  const bar = h("footer", { class: "player", hidden: true },
    h("div", { class: "bar-seek" }, pos, range, dur),
    h("div", { class: "bar-row" },
      art, h("div", { class: "bar-meta" }, title, sub),
      h("div", { class: "bar-ctl" }, shuffleB, prevB, playB, nextB, repeatB),
      h("label", { class: "saver", title: "Stream Opus 128 kbps to save mobile data" }, saver, h("span", {}, "Data saver"))));

  let lastCover = null;
  const paint = () => {
    const song = current();
    bar.hidden = !song;
    if (!song) return;
    title.textContent = song.title || "Unknown title";
    sub.textContent = [song.artist, song.album].filter(Boolean).join(" · ");
    if (lastCover !== song.coverArt) { lastCover = song.coverArt; art.replaceChildren(cover(song.coverArt, 96, "art small")); }
    const s = settings();
    playB.classList.toggle("pause", isPlaying());
    playB.setAttribute("aria-label", isPlaying() ? "Pause" : "Play");
    shuffleB.classList.toggle("on", s.shuffle);
    repeatB.classList.toggle("on", s.repeat !== "off");
    repeatB.dataset.mode = s.repeat;
    repeatB.title = `Repeat: ${s.repeat}`;
    saver.checked = s.saver;
    const d = audio.duration || song.duration || 0;
    range.max = d;
    dur.textContent = fmtTime(d);
    if (!dragging) { range.value = audio.currentTime; pos.textContent = fmtTime(audio.currentTime); }
    if ("mediaSession" in navigator && d) {
      try { navigator.mediaSession.setPositionState({ duration: d, position: Math.min(audio.currentTime, d), playbackRate: 1 }); } catch {}
    }
  };
  onChange(paint);
  audio.addEventListener("timeupdate", paint);
  paint();
  return bar;
}
