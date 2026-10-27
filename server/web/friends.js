import { api, Unauthorized } from "./dom.js";
import { onChange, current } from "./player.js";

const SONG_GAP_MS = 15000;
const listeners = new Set();
let snapshot = null;
let failed = false;
let inflight = null;
let lastSong = "";
let lastSongCheck = 0;

export const friendsSnapshot = () => snapshot;
export const friendsFailed = () => failed;

export function onFriends(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function refreshFriends() {
  if (inflight) return inflight;
  inflight = api("/api/friends")
    .then((body) => {
      snapshot = body.friends || [];
      failed = false;
    })
    .catch((e) => {
      if (e instanceof Unauthorized) throw e;
      failed = true;
    })
    .finally(() => {
      inflight = null;
      listeners.forEach((fn) => fn());
    });
  return inflight;
}

export function refreshFriendsQuietly() {
  refreshFriends().catch(() => {});
}

onChange(() => {
  const s = current();
  const id = s ? s.id || "" : "";
  if (!id || id === lastSong) return;
  lastSong = id;
  const now = Date.now();
  if (now - lastSongCheck < SONG_GAP_MS || document.visibilityState !== "visible") return;
  lastSongCheck = now;
  refreshFriendsQuietly();
});
