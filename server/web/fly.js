import { h } from "./dom.js";
import { spine } from "./queue.js";

const STEPS = 10;
const TIME = 520;
const RECENT = 2000;

let tapEl = null;
let tapAt = 0;
let rowEl = null;
let rowAt = 0;

const calm = () => !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);

const shown = (el) => {
  if (!el || !el.isConnected) return false;
  const r = el.getBoundingClientRect();
  if (!r.width || !r.height) return false;
  return getComputedStyle(el).visibility !== "hidden";
};

const center = (r) => ({ x: r.left + r.width / 2, y: r.top + r.height / 2 });

const sourcePoint = () => {
  if (tapEl && performance.now() - tapAt < RECENT && shown(tapEl)) return center(tapEl.getBoundingClientRect());
  if (rowEl && performance.now() - rowAt < RECENT * 5 && shown(rowEl)) return center(rowEl.getBoundingClientRect());
  return { x: window.innerWidth / 2, y: window.innerHeight / 2 };
};

const isAlbum = (songs) => songs.length > 1 && !!songs[0].albumId && songs.every((s) => s.albumId === songs[0].albumId);

const once = (el, cls, ms) => {
  el.classList.remove(cls);
  void el.offsetWidth;
  el.classList.add(cls);
  setTimeout(() => el.classList.remove(cls), ms);
};

const toast = (songs, album) => {
  const bar = document.querySelector("footer.player");
  const r = shown(bar) ? bar.getBoundingClientRect() : null;
  const pop = h("div", { class: "cdfly-toast" + (calm() ? " still" : ""), "aria-hidden": "true" },
    h("span", { class: "cdfly-case " + (album ? "album" : "single") }),
    h("b", {}, "+" + songs.length));
  pop.style.setProperty("--c", spine(songs[0]));
  pop.style.bottom = (r ? Math.max(8, window.innerHeight - r.top + 8) : 24) + "px";
  document.body.append(pop);
  setTimeout(() => pop.remove(), 900);
};

const fly = (songs) => {
  if (!songs || !songs.length) return;
  const album = isAlbum(songs);
  const stack = document.querySelector(".cdq");
  if (!shown(stack)) { toast(songs, album); return; }
  const cases = stack.querySelector(".cdq-cases");
  if (calm()) { once(stack, "flash", 700); return; }

  const fresh = stack.querySelector(".case.drop");
  const target = fresh || stack.querySelector(".case.top") || cases;
  if (fresh) { fresh.classList.remove("drop"); fresh.classList.add("wait"); }

  const from = sourcePoint();
  const tr = target.getBoundingClientRect();
  const to = fresh ? center(tr) : { x: tr.left + tr.width / 2, y: tr.top - (album ? 10 : 4) };
  const peak = Math.min(from.y, to.y) - Math.max(60, Math.abs(to.x - from.x) * 0.25);

  const el = h("span", { class: "cdfly cdfly-case " + (album ? "album" : "single"), "aria-hidden": "true" });
  el.style.setProperty("--c", spine(songs[0]));
  document.body.append(el);

  const frames = [];
  for (let i = 0; i <= STEPS; i++) {
    const k = i / STEPS;
    const u = 1 - k;
    const x = Math.round(u * u * from.x + 2 * u * k * ((from.x + to.x) / 2) + k * k * to.x);
    const y = Math.round(u * u * from.y + 2 * u * k * peak + k * k * to.y);
    const s = 1.6 - 0.6 * k;
    const rot = Math.round((1 - k) * -18);
    frames.push({ transform: `translate(${x}px, ${y}px) translate(-50%, -50%) scale(${s.toFixed(2)}) rotate(${rot}deg)`, easing: "steps(1, end)" });
  }

  let done = false;
  const land = () => {
    if (done) return;
    done = true;
    el.remove();
    if (fresh) { fresh.classList.remove("wait"); once(fresh, "land", 400); }
    once(cases, "thud", 400);
  };
  if (!el.animate) { land(); return; }
  const anim = el.animate(frames, { duration: TIME, fill: "forwards" });
  anim.onfinish = land;
  anim.oncancel = land;
};

document.addEventListener("click", (e) => {
  const el = e.target && e.target.closest && e.target.closest("button, [role=button], a, li");
  if (el) { tapEl = el; tapAt = performance.now(); }
}, true);

document.addEventListener("pointerdown", (e) => {
  const li = e.target && e.target.closest && e.target.closest("li");
  if (li) { rowEl = li; rowAt = performance.now(); }
}, true);

document.addEventListener("queueadd", (e) => fly(e.detail && e.detail.songs));
