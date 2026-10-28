import { t, relTime } from "./i18n.js";
import { h, api, apiJSON, Unauthorized, cover } from "./dom.js";
import * as player from "./player.js";
import { toast } from "./playlists.js";
import { AVATAR } from "./settings.js";
import { refreshFriendsQuietly } from "./friends.js";

const NOTE_MAX = 140;
const ERRORS = { "rate-limited": "recs.rateLimited", "unknown-user": "recs.unknownUser", self: "recs.self" };

const badges = new Set();
let unread = 0;

export function setUnread(n) {
  unread = Math.max(0, n | 0);
  for (const b of badges) paintBadge(b);
}

function paintBadge(b) {
  b.hidden = !unread;
  b.textContent = unread > 9 ? "9+" : String(unread);
  b.title = t("recs.unread", { n: unread });
}

export function recBadge() {
  const b = h("span", { class: "rec-badge", "aria-hidden": "true" });
  badges.add(b);
  paintBadge(b);
  return b;
}

export async function refreshUnread() {
  try { setUnread((await api("/api/recs")).unread); } catch {}
}

function pic(user, avatar) {
  const el = h("span", { class: "friend-pic" });
  if (avatar) {
    const img = h("img", { src: `/api/avatar/${encodeURIComponent(user)}?v=${avatar}`, alt: "", width: 32, height: 32 });
    img.addEventListener("error", () => { el.innerHTML = AVATAR; });
    el.append(img);
  } else el.innerHTML = AVATAR;
  return el;
}

const songOf = (s) => ({ id: s.id, title: s.title || "", artist: s.artist || "", album: s.album || "", albumId: s.albumId || "", coverArt: s.coverArt || "", duration: s.duration || 0 });

const albumOf = (al) => ({ id: al.id, name: al.name || al.title || "", artist: al.artist || "", coverArt: al.coverArt || "" });

export function openRecommendSheet(song, opts = {}) {
  return recommendSheet(t("recs.sheetTitle", { title: song.title || t("player.unknownTitle") }), { song: songOf(song) }, opts);
}

export function openAlbumRecommendSheet(al, opts = {}) {
  const album = albumOf(al);
  return recommendSheet(t("recs.sheetTitleAlbum", { title: album.name || t("library.unknownAlbum") }), { kind: "album", album }, opts);
}

async function recommendSheet(heading, item, opts) {
  const picked = new Set();
  const list = h("ul", { class: "sheet-list rec-pick" }, h("li", { class: "muted" }, t("common.loading")));
  const note = h("textarea", { class: "rec-note", rows: 2, maxlength: NOTE_MAX, placeholder: t("recs.notePlaceholder"), "aria-label": t("recs.notePlaceholder") });
  const count = h("span", { class: "muted rec-count" }, `0/${NOTE_MAX}`);
  note.addEventListener("input", () => { count.textContent = `${note.value.length}/${NOTE_MAX}`; });
  const status = h("div", { class: "muted", role: "status" });
  const send = h("button", { class: "primary", type: "submit", disabled: true }, t("recs.send"));
  const paintSend = () => {
    send.disabled = !picked.size;
    send.textContent = picked.size > 1 ? t("recs.sendMany", { n: picked.size }) : t("recs.send");
  };
  const dlg = h("dialog", { class: "sheet card rec-sheet" },
    h("h2", {}, heading),
    list,
    h("form", { class: "rec-form", onsubmit: async (e) => {
      e.preventDefault();
      if (!picked.size) return;
      send.disabled = true;
      try {
        await apiJSON("POST", "/api/recs", { ...item, to: [...picked], note: note.value.trim() });
        dlg.close();
        toast(picked.size > 1 ? t("recs.sentMany", { n: picked.size }) : t("recs.sentOne", { user: [...picked][0] }));
      } catch (err) {
        if (err instanceof Unauthorized) { dlg.close(); return opts.onUnauthorized?.(); }
        status.textContent = t(ERRORS[err.message] || "recs.failed");
        paintSend();
      }
    } }, note, count, send),
    status,
    h("button", { class: "ghost", type: "button", onclick: () => dlg.close() }, t("common.cancel")));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  try {
    const { friends } = await api("/api/friends");
    if (!friends.length) return list.replaceChildren(h("li", { class: "muted" }, t("recs.noFriends")));
    list.replaceChildren(...friends.map((f) => {
      const btn = h("button", { class: "sheet-item rec-friend", type: "button", "aria-pressed": "false" },
        h("span", { class: "rec-who" }, pic(f.user, f.avatar), h("span", {}, f.user)),
        h("span", { class: "rec-check", "aria-hidden": "true" }, ""));
      btn.addEventListener("click", () => {
        if (picked.has(f.user)) picked.delete(f.user); else picked.add(f.user);
        const on = picked.has(f.user);
        btn.setAttribute("aria-pressed", String(on));
        btn.lastChild.textContent = on ? "✓" : "";
        paintSend();
      });
      return h("li", {}, btn);
    }));
  } catch (e) {
    if (e instanceof Unauthorized) { dlg.close(); return opts.onUnauthorized?.(); }
    list.replaceChildren(h("li", { class: "muted" }, t("friends.failed")));
  }
}

async function albumSongs(al) {
  const body = await api(`/api/music/album/${encodeURIComponent(al.id)}`);
  return (body.song || []).map((s) => ({ ...s, coverArt: s.coverArt || body.coverArt || al.coverArt }));
}

function albumRecRow(r, onDismiss, opts) {
  const al = r.album;
  const name = al.name || t("library.unknownAlbum");
  const label = al.artist ? `${name} – ${al.artist}` : name;
  const href = `#/library/album/${encodeURIComponent(al.id)}`;
  const open = () => opts.onNavigate?.();
  const play = async () => {
    try {
      const songs = await albumSongs(al);
      if (songs.length) player.playList(songs, 0);
      else toast(t("recs.albumGone"));
    } catch { toast(t("recs.albumGone")); }
  };
  return h("li", { class: `rec rec-album${r.seen ? "" : " new"}` },
    h("a", { class: "rec-cover", href, onclick: open, "aria-hidden": "true", tabindex: "-1" }, cover(al.coverArt, 96, "art tiny")),
    h("span", { class: "friend-text" },
      h("span", { class: "tag rec-kind" }, t("recs.album")),
      h("a", { class: "linkish rec-track", href, title: label, onclick: open }, label),
      h("span", { class: "friend-what" }, t("recs.from", { user: r.from, when: relTime(r.createdAt) })),
      r.note ? h("span", { class: "rec-note-text" }, `“${r.note}”`) : null),
    h("span", { class: "rec-actions" },
      h("button", { class: "icon", type: "button", title: t("recs.playAlbum"), "aria-label": t("recs.playAlbum"), onclick: play }, "▶"),
      h("button", { class: "icon", type: "button", title: t("recs.dismiss"), "aria-label": t("recs.dismiss"), onclick: () => onDismiss(r) }, "✕")));
}

function recRow(r, onDismiss, opts) {
  if (r.kind === "album" && r.album) return albumRecRow(r, onDismiss, opts);
  const s = r.song;
  const title = s.title || t("player.unknownTitle");
  const track = s.artist ? `${title} – ${s.artist}` : title;
  const play = () => player.playList([songOf(s)], 0);
  const queue = () => { player.addToQueue([songOf(s)]); toast(t("recs.queued", { title })); };
  return h("li", { class: `rec${r.seen ? "" : " new"}` },
    pic(r.from, r.avatar),
    h("span", { class: "friend-text" },
      h("button", { class: "linkish rec-track", type: "button", title: track, onclick: play }, track),
      h("span", { class: "friend-what" }, t("recs.from", { user: r.from, when: relTime(r.createdAt) })),
      r.note ? h("span", { class: "rec-note-text" }, `“${r.note}”`) : null),
    h("span", { class: "rec-actions" },
      h("button", { class: "icon", type: "button", title: t("recs.play"), "aria-label": t("recs.play"), onclick: play }, "▶"),
      h("button", { class: "icon", type: "button", title: t("song.addToQueue"), "aria-label": t("song.addToQueue"), onclick: queue }, "+"),
      h("button", { class: "icon", type: "button", title: t("recs.dismiss"), "aria-label": t("recs.dismiss"), onclick: () => onDismiss(r) }, "✕")));
}

export function recsPanel(opts = {}) {
  const list = h("ul", { class: "friends recs" });
  const el = h("section", { class: "friends-panel recs-panel", "aria-label": t("recs.title"), hidden: true },
    h("h3", {}, t("recs.title")), list);
  let items = [];
  const paint = () => {
    el.hidden = !items.length;
    list.replaceChildren(...items.map((r) => recRow(r, dismiss, opts)));
  };
  const dismiss = async (r) => {
    items = items.filter((x) => x.id !== r.id);
    paint();
    try { await apiJSON("POST", `/api/recs/${encodeURIComponent(r.id)}`, { dismiss: true }); } catch {}
  };
  const load = async () => {
    try {
      const body = await api("/api/recs");
      items = body.recs;
      paint();
      if (body.unread) {
        const res = await apiJSON("POST", "/api/recs/all", { seen: true });
        setUnread(res.unread);
      } else setUnread(0);
    } catch {}
  };
  return { el, start: load, stop() {} };
}

const POLL_MS = 60000;
let pollTimer = null;

function refreshAll() {
  refreshUnread();
  refreshFriendsQuietly();
}

function poll() {
  clearInterval(pollTimer);
  pollTimer = null;
  if (document.visibilityState !== "visible") return;
  refreshAll();
  pollTimer = setInterval(refreshAll, POLL_MS);
}

export function startRecPolling() {
  if (startRecPolling.on) return;
  startRecPolling.on = true;
  document.addEventListener("visibilitychange", poll);
  window.addEventListener("focus", () => { if (document.visibilityState === "visible") refreshAll(); });
  refreshFriendsQuietly();
  if (document.visibilityState === "visible") pollTimer = setInterval(refreshAll, POLL_MS);
}
