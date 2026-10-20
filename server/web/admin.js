import { h, api, Unauthorized } from "./dom.js";

const COLORS = ["#ffcd75", "#73eff7", "#a7f070", "#ef7d57", "#b13e53", "#3b5dc9", "#94b0c2", "#38b764"];

const fmtHours = (s) => (s >= 3600 ? `${(s / 3600).toFixed(1)} h` : `${Math.round(s / 60)} min`);
const fmtBytes = (b) => {
  if (b >= 1e9) return `${(b / 1e9).toFixed(2)} GB`;
  if (b >= 1e6) return `${(b / 1e6).toFixed(1)} MB`;
  return `${Math.round(b / 1e3)} kB`;
};
const fmtAgo = (t) => {
  if (!t) return "never";
  const s = Math.max(0, Date.now() / 1000 - t);
  if (s < 90) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return `${Math.round(s / 86400)} d ago`;
};
const fmtWhen = (t) => new Date(t * 1000).toLocaleString([], { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
const dayLabel = (d) => new Date(`${d}T12:00:00`).toLocaleDateString([], { weekday: "short" });

function tile(label, value, sub) {
  return h("div", { class: "stat" }, h("span", { class: "stat-label" }, label), h("strong", {}, value), sub ? h("span", { class: "stat-sub" }, sub) : null);
}

function chart(d, color) {
  const totals = d.days.map((_, i) => d.users.reduce((n, u) => n + u.daily[i], 0));
  const top = Math.max(1, ...totals);
  const cols = d.days.map((day, i) => {
    const segs = d.users.filter((u) => u.daily[i] > 0).map((u) =>
      h("span", { class: "seg", style: `height:${(u.daily[i] / top) * 100}%;background:${color(u.user)}`, title: `${u.user}: ${fmtHours(u.daily[i])}` }));
    return h("div", { class: "col" },
      h("span", { class: "col-val" }, totals[i] ? fmtHours(totals[i]) : ""),
      h("div", { class: "col-bar" }, segs),
      h("span", { class: "col-day" }, dayLabel(day)));
  });
  const legend = d.users.filter((u) => u.seconds > 0).map((u) =>
    h("span", { class: "key" }, h("i", { style: `background:${color(u.user)}` }), u.user));
  return h("section", { class: "card admin-chart" },
    h("h2", {}, "Listening per day"),
    h("div", { class: "cols", role: "img", "aria-label": "Hours listened per day, last 7 days" }, cols),
    legend.length ? h("div", { class: "legend" }, legend) : h("p", { class: "muted" }, "Nobody listened this week."));
}

function userTable(d, color) {
  const head = h("tr", {}, ["Friend", "Last login", "Last seen", "Devices", "Plays", "Listened", "Data"].map((t) => h("th", { scope: "col" }, t)));
  const rows = d.users.map((u) => h("tr", {},
    h("th", { scope: "row" }, h("i", { class: "dot", style: `background:${color(u.user)}` }), u.user),
    h("td", { title: u.lastLogin ? fmtWhen(u.lastLogin) : "" }, fmtAgo(u.lastLogin)),
    h("td", { title: u.lastSeen ? fmtWhen(u.lastSeen) : "" }, fmtAgo(u.lastSeen)),
    h("td", { class: "devs" }, u.devices.join(", ") || "–"),
    h("td", { class: "num" }, String(u.plays)),
    h("td", { class: "num" }, fmtHours(u.seconds)),
    h("td", { class: "num" }, fmtBytes(u.bytes))));
  return h("section", { class: "card" },
    h("h2", {}, "Friends"),
    d.users.length
      ? h("div", { class: "table-wrap" }, h("table", { class: "admin-table" }, h("thead", {}, head), h("tbody", {}, rows)))
      : h("p", { class: "muted" }, "No activity recorded yet."));
}

function nowPlaying(d) {
  return h("section", { class: "card" },
    h("h2", {}, "Now playing"),
    d.nowPlaying.length
      ? h("ul", { class: "admin-list" }, d.nowPlaying.map((n) => h("li", {},
          h("strong", {}, n.user),
          h("span", {}, `${n.title || "Unknown"}${n.artist ? ` – ${n.artist}` : ""}`),
          h("span", { class: "muted" }, [n.device, n.saver ? "data saver" : ""].filter(Boolean).join(" · ")))))
      : h("p", { class: "muted" }, "Nobody is listening right now."));
}

function loginLog(d) {
  return h("section", { class: "card" },
    h("h2", {}, "Recent logins"),
    d.logins.length
      ? h("ul", { class: "admin-list" }, d.logins.map((e) => h("li", { class: e.ok ? "" : "failed" },
          h("strong", {}, e.user),
          h("span", {}, fmtWhen(e.at)),
          h("span", { class: "muted" }, [e.ok ? (e.remember ? "kept logged in" : "short session") : "wrong password", e.device].filter(Boolean).join(" · ")))))
      : h("p", { class: "muted" }, "No logins recorded yet."));
}

let timer = null;

export async function renderAdmin(container, opts = {}) {
  clearInterval(timer);
  const title = h("div", { class: "admin-head" },
    h("h1", {}, "Admin"),
    h("button", { type: "button", onclick: () => renderAdmin(container, opts) }, "Refresh"));
  if (!container.firstChild) container.replaceChildren(title, h("div", { class: "muted" }, "Loading…"));
  try {
    const d = await api("/api/admin/stats");
    const color = (u) => COLORS[d.users.findIndex((x) => x.user === u) % COLORS.length];
    const t = d.totals;
    container.replaceChildren(
      title,
      h("p", { class: "muted" }, "Last 7 days. Data older than 90 days is deleted automatically."),
      h("div", { class: "stats" },
        tile("Listened", fmtHours(t.seconds), `${t.plays} plays`),
        tile("Data streamed", fmtBytes(t.bytes)),
        tile("Active friends", String(t.active), `of ${d.users.length}`),
        tile("Logins", String(t.logins), t.failed ? `${t.failed} failed` : "none failed")),
      nowPlaying(d),
      chart(d, color),
      userTable(d, color),
      loginLog(d));
    timer = setInterval(() => {
      if (container.hidden) return clearInterval(timer);
      if (document.visibilityState === "visible") renderAdmin(container, opts);
    }, 30000);
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(title,
      h("div", { class: "card muted" }, e.message === "admin-only" ? "Only admins can see this page." : "Could not load the stats."));
  }
}
