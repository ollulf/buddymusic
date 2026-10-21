import { t } from "./i18n.js";
import { h } from "./dom.js";

const THRESHOLD = 70;
const STEP = 4;
const reduced = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

let openMenu = null;

export function closeRowMenu() {
  if (!openMenu) return;
  const { pop, btn, off } = openMenu;
  openMenu = null;
  off();
  pop.remove();
  btn.setAttribute("aria-expanded", "false");
}

export function rowMenuButton(items) {
  const btn = h("button", { class: "icon row-more", type: "button", title: t("song.more"), "aria-label": t("song.more"), "aria-haspopup": "menu", "aria-expanded": "false" }, "⋯");
  btn.addEventListener("click", (e) => {
    e.stopPropagation();
    const was = openMenu?.btn === btn;
    closeRowMenu();
    if (!was) showMenu(btn, items());
  });
  return btn;
}

function showMenu(btn, items) {
  const pop = h("div", { class: "row-menu", role: "menu" },
    items.filter(Boolean).map((it) => h("button", {
      type: "button", role: "menuitem", class: `row-menu-item${it.danger ? " danger" : ""}`, disabled: !!it.disabled,
      onclick: (e) => { e.stopPropagation(); closeRowMenu(); it.run(); },
    }, h("span", { class: "rm-ico", "aria-hidden": "true" }, it.icon || ""), h("span", {}, it.label))));
  document.body.append(pop);
  const place = () => {
    const r = btn.getBoundingClientRect();
    const w = pop.offsetWidth, ph = pop.offsetHeight;
    const below = r.bottom + 4 + ph <= window.innerHeight - 8;
    pop.style.left = `${Math.max(8, Math.min(window.innerWidth - w - 8, r.right - w))}px`;
    pop.style.top = `${below ? r.bottom + 4 : Math.max(8, r.top - ph - 4)}px`;
  };
  place();
  btn.setAttribute("aria-expanded", "true");
  const outside = (e) => { if (!pop.contains(e.target) && e.target !== btn) closeRowMenu(); };
  const key = (e) => { if (e.key === "Escape") { closeRowMenu(); btn.focus(); } };
  const nav = () => closeRowMenu();
  const resize = () => place();
  const scroll = (e) => { if (!pop.contains(e.target)) closeRowMenu(); };
  document.addEventListener("pointerdown", outside, true);
  document.addEventListener("keydown", key);
  window.addEventListener("hashchange", nav);
  window.addEventListener("resize", resize);
  document.addEventListener("scroll", scroll, true);
  openMenu = { pop, btn, off: () => {
    document.removeEventListener("pointerdown", outside, true);
    document.removeEventListener("keydown", key);
    window.removeEventListener("hashchange", nav);
    window.removeEventListener("resize", resize);
    document.removeEventListener("scroll", scroll, true);
  } };
  pop.querySelector("button:not(:disabled)")?.focus();
}

export function swipeRow(li, fg, { right, left }) {
  const stripR = h("div", { class: "swipe-strip sw-right", "aria-hidden": "true" }, h("span", { class: "sw-ico" }, right.icon), h("span", { class: "sw-label" }));
  const stripL = h("div", { class: "swipe-strip sw-left", "aria-hidden": "true" }, h("span", { class: "sw-label" }), h("span", { class: "sw-ico" }, left.icon));
  li.prepend(stripR, stripL);
  let start = null, dx = 0, mode = null, blockClick = false;
  const setX = (x) => { fg.style.transform = x ? `translateX(${x}px)` : ""; };
  const reset = () => {
    li.classList.remove("swiping", "sw-go-right", "sw-go-left", "armed");
    setX(0);
    start = null; mode = null; dx = 0;
  };
  li.addEventListener("pointerdown", (e) => {
    if (e.button !== 0 || e.target.closest(".row-more")) return;
    start = { x: e.clientX, y: e.clientY, id: e.pointerId };
    mode = null; dx = 0;
  });
  li.addEventListener("pointermove", (e) => {
    if (!start || e.pointerId !== start.id) return;
    const mx = e.clientX - start.x, my = e.clientY - start.y;
    if (!mode) {
      if (Math.abs(mx) < 10 && Math.abs(my) < 10) return;
      if (Math.abs(mx) > Math.abs(my) * 1.5) {
        mode = "swipe";
        try { li.setPointerCapture(e.pointerId); } catch {}
        li.classList.add("swiping");
        stripR.querySelector(".sw-label").textContent = right.label();
        stripL.querySelector(".sw-label").textContent = left.label();
      } else { start = null; return; }
    }
    e.preventDefault();
    dx = Math.max(-140, Math.min(140, mx));
    li.classList.toggle("sw-go-right", dx > 0);
    li.classList.toggle("sw-go-left", dx < 0);
    li.classList.toggle("armed", Math.abs(dx) >= THRESHOLD);
    if (!reduced()) setX(Math.round(dx / STEP) * STEP);
  });
  const end = (e) => {
    if (!start || e.pointerId !== start.id) return;
    const was = mode, d = dx;
    reset();
    if (was !== "swipe") return;
    blockClick = true;
    setTimeout(() => { blockClick = false; }, 0);
    if (Math.abs(d) < THRESHOLD) return;
    (d > 0 ? right : left).run();
    li.classList.remove("flash");
    void li.offsetWidth;
    li.classList.add("flash");
    setTimeout(() => li.classList.remove("flash"), 450);
  };
  li.addEventListener("pointerup", end);
  li.addEventListener("pointercancel", (e) => { if (start && e.pointerId === start.id) reset(); });
  li.addEventListener("click", (e) => { if (blockClick) { e.preventDefault(); e.stopPropagation(); blockClick = false; } }, true);
}
