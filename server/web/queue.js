import { h, fmtTime } from "./dom.js";
import { t } from "./i18n.js";
import { onChange, queue, current, jump, move, removeAt, clearManual, entryInfo } from "./player.js";

const SPINES = ["var(--accent)", "var(--orange)", "var(--accent-lo)", "var(--muted)", "var(--accent-hi)", "var(--fg)"];
const MAX = 6;

export const spine = (song) => {
  const k = String(song.albumId || song.id || "");
  let x = 0;
  for (const c of k) x = (x * 31 + c.charCodeAt(0)) >>> 0;
  return SPINES[x % SPINES.length];
};

const calm = () => !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
const touchy = () => !!(window.matchMedia && window.matchMedia("(hover: none)").matches);

export function renderQueue() {
  const stack = h("button", { type: "button", class: "cdq", "aria-haspopup": "dialog", "aria-expanded": "false" });
  const cases = h("span", { class: "cdq-cases", "aria-hidden": "true" });
  stack.append(cases);

  const list = h("ol", { class: "qp-list" });
  const count = h("small");
  const closeB = h("button", { type: "button", class: "qp-x", "aria-label": t("queue.close"), title: t("queue.close") }, "✕");
  const clearB = h("button", { type: "button", class: "qp-clear" }, t("queue.clear"));
  const panel = h("div", { class: "qpanel", role: "dialog", "aria-label": t("queue.title"), hidden: true },
    h("div", { class: "qp-head" }, h("strong", {}, t("queue.title")), count, clearB, closeB),
    list);
  const root = h("div", { class: "cdq-wrap" }, stack, panel);

  let lastSig = "";
  let lastRuns = null;
  let lastUp = 0;
  const upcoming = () => {
    const q = queue();
    const c = current();
    const i = c ? q.indexOf(c) : -1;
    const up = [];
    for (let n = i + 1; n < q.length && entryInfo(q[n]).manual; n++) up.push(q[n]);
    return { q, i, up };
  };

  const setLift = (on) => root.classList.toggle("lift", on);
  const isOpen = () => !panel.hidden;
  const close = () => {
    if (!isOpen() && !root.classList.contains("lift")) return;
    panel.hidden = true;
    root.classList.remove("open");
    setLift(false);
    stack.setAttribute("aria-expanded", "false");
  };
  const open = () => {
    panel.hidden = false;
    root.classList.add("open");
    stack.setAttribute("aria-expanded", "true");
    paintList();
    const cur = list.querySelector("li.cur");
    if (cur) cur.scrollIntoView({ block: "nearest" });
    closeB.focus({ preventScroll: true });
  };

  const opB = (label, glyph, fn, off = false) => h("button", { type: "button", class: "qp-op", disabled: off, "aria-label": label, title: label, onclick: (e) => { e.stopPropagation(); fn(); } }, glyph);

  const row = (q, i, s, n, ops) => {
    const li = h("li", { class: n === i ? "cur" : n < i ? "done" : "", style: `--c:${spine(s)}` },
      h("span", { class: "sp" }),
      h("span", { class: "n" }, n === i ? "▶" : String(n + 1)),
      h("span", { class: "t" }, s.title || t("player.unknownTitle"), h("i", {}, [s.artist, s.duration ? fmtTime(+s.duration) : ""].filter(Boolean).join(" · "))),
      h("span", { class: "ops" }, ops));
    li.tabIndex = 0;
    li.addEventListener("click", () => jump(n));
    li.addEventListener("keydown", (e) => { if (e.key === "Enter" && e.target === li) jump(n); });
    return li;
  };
  const divider = (label) => h("li", { class: "qp-sec", "aria-hidden": "true" }, label);

  const paintList = () => {
    const { q, i, up } = upcoming();
    const first = i + 1;
    const last = i + up.length;
    count.textContent = t("queue.tracks", { n: up.length });
    clearB.disabled = !up.length;
    const kids = [];
    const rm = (n) => opB(t("queue.remove"), "✕", () => removeAt([n]));
    q.forEach((s, n) => {
      if (n === first && up.length) kids.push(divider(t("queue.upNext")));
      if (n === last + 1 && n > i) kids.push(divider(t("queue.nextFrom")));
      let ops = [];
      if (n >= first && n <= last) ops = [
        opB(t("queue.up"), "▲", () => move(n, n - 1), n === first),
        opB(t("queue.down"), "▼", () => move(n, n + 1), n === last),
        rm(n),
      ];
      else if (n !== i) ops = [rm(n)];
      kids.push(row(q, i, s, n, ops));
    });
    list.replaceChildren(...kids);
  };

  const paint = () => {
    const { q, i, up } = upcoming();
    const sig = q.map((s) => s.id + (entryInfo(s).manual ? "+" + entryInfo(s).batch : "")).join(",") + "|" + i;
    if (sig === lastSig) return;
    lastSig = sig;
    const runs = [];
    for (const s of up) {
      const { batch: b, size } = entryInfo(s);
      const last = runs[runs.length - 1];
      if (last && last.batch === b) last.n++;
      else runs.push({ batch: b, song: s, n: 1, size, key: b + ":" + s.id });
    }
    const shown = Math.min(runs.length, MAX);
    const kids = [];
    if (!shown) kids.push(h("span", { class: "case empty" }));
    for (let n = 0; n < shown; n++) {
      const r = runs[shown - 1 - n];
      const c = h("span", { class: "case" + (r.size > 1 ? " album" : " single") + (n % 3 === 1 ? " alt" : n % 3 === 2 ? " alt2" : "") + (n === shown - 1 ? " top" : "") });
      c.style.setProperty("--c", spine(r.song));
      c.dataset.key = r.key;
      kids.push(c);
    }
    cases.replaceChildren(...kids);
    const label = up.length ? t("queue.open", { n: up.length }) : `${t("queue.title")}: ${t("queue.empty")}`;
    stack.setAttribute("aria-label", label);
    stack.title = label;
    if (lastRuns && up.length > lastUp && !calm()) {
      const added = runs.slice(0, shown).find((r) => !lastRuns.has(r.key));
      const el = added && [...cases.children].find((c) => c.dataset.key === added.key);
      if (el) { el.classList.add("drop"); el.addEventListener("animationend", () => el.classList.remove("drop"), { once: true }); }
    }
    lastRuns = new Set(runs.map((r) => r.key));
    lastUp = up.length;
    if (!q.length) close();
    else if (isOpen()) paintList();
  };

  stack.addEventListener("click", (e) => {
    e.stopPropagation();
    if (isOpen()) { close(); return; }
    open();
  });
  closeB.addEventListener("click", (e) => { e.stopPropagation(); close(); stack.focus({ preventScroll: true }); });
  clearB.addEventListener("click", (e) => {
    e.stopPropagation();
    clearManual();
  });
  panel.addEventListener("click", (e) => e.stopPropagation());
  document.addEventListener("click", (e) => { if (!root.contains(e.target)) close(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && (isOpen() || root.classList.contains("lift"))) { close(); stack.focus({ preventScroll: true }); } });

  onChange(paint);
  paint();
  return root;
}
