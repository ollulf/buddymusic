const STEPS = 16;
const live = new Set();
const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
let queued = false;

function update() {
  queued = false;
  if (motion.matches) return;
  const vh = window.innerHeight || 1;
  for (const el of live) {
    const r = el.getBoundingClientRect();
    const p = Math.max(0, Math.min(1, 1 - (r.top + r.height / 2) / vh));
    el.style.setProperty("--p", (Math.round(p * STEPS) / STEPS).toFixed(4));
  }
}

function queue() {
  if (queued) return;
  queued = true;
  requestAnimationFrame(update);
}

const io = "IntersectionObserver" in window ? new IntersectionObserver((entries) => {
  for (const e of entries) {
    if (e.isIntersecting) live.add(e.target);
    else {
      live.delete(e.target);
      if (!e.target.isConnected) io.unobserve(e.target);
    }
  }
  queue();
}) : null;

document.addEventListener("scroll", queue, { passive: true, capture: true });
window.addEventListener("resize", queue);

export function holo() {
  const el = document.createElement("span");
  el.className = "holo";
  el.setAttribute("aria-hidden", "true");
  if (io) io.observe(el);
  return el;
}

export function sparkles() {
  const el = document.createElement("span");
  el.className = "sparkles";
  el.setAttribute("aria-hidden", "true");
  for (const n of [1, 2, 3]) {
    const s = document.createElement("i");
    s.className = `sp s${n}`;
    el.append(s);
  }
  return el;
}

export function unholo(el) {
  if (!el) return;
  if (io) io.unobserve(el);
  live.delete(el);
  el.remove();
}
