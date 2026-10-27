import { t, relTime } from "./i18n.js";
import { h, api, apiJSON, Unauthorized } from "./dom.js";
import * as player from "./player.js";
import { toast } from "./playlists.js";
import { AVATAR } from "./settings.js";

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

const ALERT_KEY = "bm.recAlerts";
const SEEN_KEY = "bm.recAnnounced";
const POLL_MS = 25000;
let opener = null;
let banner = null;
let bannerTimer = null;
let pollTimer = null;

export function recAlertsOn() {
  try { return localStorage.getItem(ALERT_KEY) !== "0"; } catch { return true; }
}

export function setRecAlerts(on) {
  try { localStorage.setItem(ALERT_KEY, on ? "1" : "0"); } catch {}
}

export function setRecsOpener(fn) { opener = fn; }

function announced() {
  try { return new Set(JSON.parse(localStorage.getItem(SEEN_KEY) || "[]")); } catch { return new Set(); }
}

function remember(ids) {
  try { localStorage.setItem(SEEN_KEY, JSON.stringify(ids.slice(-200))); } catch {}
}

function hideBanner() {
  clearTimeout(bannerTimer);
  if (banner) banner.remove();
  banner = null;
}

function showBanner(fresh) {
  hideBanner();
  const r = fresh[0];
  const s = r.song;
  const title = s.title || t("player.unknownTitle");
  const track = s.artist ? `${title} – ${s.artist}` : title;
  const open = () => { hideBanner(); opener?.(); };
  const text = fresh.length > 1
    ? h("span", { class: "rec-alert-text" }, t("recs.unread", { n: fresh.length }))
    : h("span", { class: "rec-alert-text" },
      t("recs.alert", { user: r.from, track }),
      r.note ? h("span", { class: "rec-note-text" }, `“${r.note}”`) : null);
  const play = fresh.length === 1
    ? h("button", { class: "icon", type: "button", title: t("recs.play"), "aria-label": t("recs.play"), onclick: (e) => { e.stopPropagation(); hideBanner(); player.playList([songOf(s)], 0); } }, "▶")
    : null;
  const shut = h("button", { class: "icon", type: "button", title: t("recs.close"), "aria-label": t("recs.close"), onclick: (e) => { e.stopPropagation(); hideBanner(); } }, "✕");
  banner = h("div", { class: "rec-alert", role: "status", onclick: open },
    fresh.length > 1 ? null : pic(r.from, r.avatar), text, play, shut);
  document.body.append(banner);
  try { navigator.vibrate?.(60); } catch {}
  bannerTimer = setTimeout(hideBanner, 6000);
}

export async function refreshUnread() {
  try {
    const body = await api("/api/recs");
    setUnread(body.unread);
    const known = announced();
    const fresh = (body.recs || []).filter((r) => !r.seen && !known.has(r.id));
    if (!fresh.length) return;
    remember([...known, ...fresh.map((r) => r.id)]);
    if (recAlertsOn() && document.visibilityState === "visible") showBanner(fresh);
  } catch {}
}

function poll() {
  clearInterval(pollTimer);
  pollTimer = null;
  if (document.visibilityState !== "visible") return;
  refreshUnread();
  pollTimer = setInterval(refreshUnread, POLL_MS);
}

export function startRecPolling() {
  if (startRecPolling.on) return;
  startRecPolling.on = true;
  document.addEventListener("visibilitychange", poll);
  window.addEventListener("focus", () => { if (document.visibilityState === "visible") refreshUnread(); });
  pollTimer = setInterval(refreshUnread, POLL_MS);
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

export async function openRecommendSheet(song, opts = {}) {
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
    h("h2", {}, t("recs.sheetTitle", { title: song.title || t("player.unknownTitle") })),
    list,
    h("form", { class: "rec-form", onsubmit: async (e) => {
      e.preventDefault();
      if (!picked.size) return;
      send.disabled = true;
      try {
        await apiJSON("POST", "/api/recs", { song: songOf(song), to: [...picked], note: note.value.trim() });
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

function recRow(r, onDismiss) {
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

export function recsPanel() {
  const list = h("ul", { class: "friends recs" });
  const el = h("section", { class: "friends-panel recs-panel", "aria-label": t("recs.title"), hidden: true },
    h("h3", {}, t("recs.title")), list);
  let items = [];
  const paint = () => {
    el.hidden = !items.length;
    list.replaceChildren(...items.map((r) => recRow(r, dismiss)));
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
