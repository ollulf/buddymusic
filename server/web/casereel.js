import { t } from "./i18n.js";
import { h, api, cover, Unauthorized } from "./dom.js";
import { sparkles } from "./holo.js";
import { playList } from "./player.js";

const FETCH = 30;
const SLOTS = 44;
const WIN = 36;
const SPIN_MS = 4200;
const SETTLE_MS = 350;
const GROW_MS = 450;
const MIN_ALBUMS = 3;

const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
const reduced = () => motion.matches;

async function randomAlbums() {
  const res = await api(`/api/music/albums?type=random&size=${FETCH}`);
  return (res?.album || []).filter((al) => al && al.id);
}

function reelCase(al) {
  return h("div", { class: "reel-slot", "data-album": al.id }, cover(al.coverArt || al.id, 160, "art jc", true));
}

function fillSlots(albums, winner) {
  const pool = albums.filter((al) => al.id !== winner.id);
  const out = [];
  let prev = null;
  for (let i = 0; i < SLOTS; i++) {
    if (i === WIN) { out.push(winner); prev = winner; continue; }
    let pick = pool[Math.floor(Math.random() * pool.length)];
    if (pool.length > 1) while (pick === prev) pick = pool[Math.floor(Math.random() * pool.length)];
    out.push(pick);
    prev = pick;
  }
  return out;
}

export function openCaseReel(opts = {}) {
  let albums = [];
  let winner = null;
  let timers = [];
  let spinning = false;
  const later = (fn, ms) => timers.push(setTimeout(fn, ms));
  const clearTimers = () => { timers.forEach(clearTimeout); timers = []; };

  const track = h("div", { class: "reel-track" });
  const marker = h("div", { class: "reel-marker", "aria-hidden": "true" });
  const reel = h("div", { class: "reel", "aria-hidden": "true" }, track, h("div", { class: "reel-fade" }), marker);
  const status = h("p", { class: "reel-status muted", role: "status", "aria-live": "polite" });
  const name = h("b", { class: "reel-name" });
  const artist = h("small", { class: "reel-artist" });
  const mini = h("div", { class: "reel-mini" });
  const playBtn = h("button", { class: "primary", type: "button", onclick: () => playWinner() }, t("reel.play"));
  const againBtn = h("button", { type: "button", onclick: () => spinAgain() }, t("reel.again"));
  const reveal = h("div", { class: "reel-reveal", hidden: true },
    mini, h("div", { class: "reel-text" }, name, artist), h("div", { class: "reel-acts" }, playBtn, againBtn));
  const closeBtn = h("button", { class: "ghost news-close", type: "button", "aria-label": t("news.close"), title: t("news.close"), onclick: () => dlg.close() }, "✕");
  const dlg = h("dialog", { class: "sheet card case-reel", "aria-labelledby": "reel-title" },
    h("div", { class: "news-head" }, h("h2", { id: "reel-title" }, t("reel.title")), closeBtn),
    reel, h("div", { class: "reel-foot" }, status, reveal));

  dlg.addEventListener("click", (e) => {
    if (e.target !== dlg) return;
    const r = dlg.getBoundingClientRect();
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) dlg.close();
  });
  dlg.addEventListener("close", () => { clearTimers(); dlg.remove(); opts.returnTo?.focus(); });
  document.body.append(dlg);
  dlg.showModal();
  closeBtn.focus();

  const fail = (err) => {
    if (err instanceof Unauthorized) { dlg.close(); opts.onUnauthorized?.(); return true; }
    return false;
  };

  const setBusy = (on) => {
    spinning = on;
    playBtn.disabled = on;
    againBtn.disabled = on;
  };

  const centreOffset = (el) => reel.clientWidth / 2 - (el.offsetLeft + el.offsetWidth / 2);

  const land = (slots) => {
    const wc = slots[WIN];
    reel.classList.add("done");
    track.style.transition = reduced() ? "none" : `transform ${SETTLE_MS}ms ease-out`;
    track.style.transform = `translateX(${centreOffset(wc)}px)`;
    wc.classList.add("won");
    slots.forEach((c, i) => { if (i !== WIN) c.classList.add("dim", i < WIN ? "step-l" : "step-r"); });
    wc.append(sparkles());
    later(showWinner, reduced() ? 0 : GROW_MS);
  };

  const showWinner = () => {
    name.textContent = winner.name || t("library.unknownAlbum");
    name.title = name.textContent;
    artist.textContent = winner.artist || "";
    mini.replaceChildren(cover(winner.coverArt || winner.id, 128, "art small jc"));
    status.textContent = t("reel.won", { name: name.textContent, artist: artist.textContent });
    status.classList.add("quiet");
    reveal.hidden = false;
    setBusy(false);
    playBtn.focus();
  };

  const spin = () => {
    clearTimers();
    setBusy(true);
    reveal.hidden = true;
    status.classList.remove("quiet");
    status.textContent = t("reel.spinning");
    reel.classList.remove("done");
    winner = albums[Math.floor(Math.random() * albums.length)];
    const slots = fillSlots(albums, winner).map(reelCase);
    track.style.transition = "none";
    track.style.transform = "translateX(0)";
    track.replaceChildren(...slots);
    void track.offsetWidth;
    if (reduced()) { land(slots); return; }
    const jitter = (Math.random() - 0.5) * slots[WIN].offsetWidth * 0.7;
    track.style.transition = `transform ${SPIN_MS}ms cubic-bezier(.1,.75,.18,1)`;
    track.style.transform = `translateX(${centreOffset(slots[WIN]) + jitter}px)`;
    later(() => land(slots), SPIN_MS);
  };

  const load = async () => {
    setBusy(true);
    reveal.hidden = true;
    status.classList.remove("quiet");
    status.textContent = t("common.loading");
    try {
      albums = await randomAlbums();
    } catch (err) {
      if (fail(err)) return;
      status.textContent = t("reel.failed");
      reveal.hidden = true;
      return;
    }
    if (!dlg.open) return;
    if (albums.length < MIN_ALBUMS) {
      reel.hidden = true;
      status.textContent = t("reel.tooFew");
      return;
    }
    spin();
  };

  const spinAgain = () => { if (!spinning) load(); };

  const playWinner = async () => {
    if (spinning || !winner) return;
    playBtn.disabled = true;
    try {
      const body = await api(`/api/music/album/${encodeURIComponent(winner.id)}`);
      const songs = (body.song || []).map((s) => ({ ...s, coverArt: s.coverArt || body.coverArt || winner.coverArt }));
      if (!songs.length) throw new Error("empty");
      playList(songs, 0);
      dlg.close();
    } catch (err) {
      if (fail(err)) return;
      playBtn.disabled = false;
      status.classList.remove("quiet");
      status.textContent = t("reel.failed");
    }
  };

  load();
  return dlg;
}
