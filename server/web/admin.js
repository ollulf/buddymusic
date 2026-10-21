import { t, lang, relTime } from "./i18n.js";
import { h, api, Unauthorized } from "./dom.js";

const COLORS = ["#ffcd75", "#73eff7", "#a7f070", "#ef7d57", "#b13e53", "#3b5dc9", "#94b0c2", "#38b764"];

const fmtHours = (s) => (s >= 3600 ? `${(s / 3600).toFixed(1)} h` : `${Math.round(s / 60)} min`);
const fmtBytes = (b) => {
  if (b >= 1e9) return `${(b / 1e9).toFixed(2)} GB`;
  if (b >= 1e6) return `${(b / 1e6).toFixed(1)} MB`;
  return `${Math.round(b / 1e3)} kB`;
};
const fmtAgo = (at) => (at ? relTime(Math.min(at, Date.now() / 1000)) : t("admin.never"));
const fmtWhen = (at) => new Date(at * 1000).toLocaleString(lang, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
const dayLabel = (d) => new Date(`${d}T12:00:00`).toLocaleDateString(lang, { weekday: "short" });

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
    h("h2", {}, t("admin.perDay")),
    h("div", { class: "cols", role: "img", "aria-label": t("admin.perDayLabel") }, cols),
    legend.length ? h("div", { class: "legend" }, legend) : h("p", { class: "muted" }, t("admin.nobodyWeek")));
}

function userTable(d, color) {
  const head = h("tr", {}, ["friend", "lastLogin", "lastSeen", "devices", "plays", "listened", "data"].map((k) => h("th", { scope: "col" }, t(`admin.col.${k}`))));
  const rows = d.users.map((u) => h("tr", {},
    h("th", { scope: "row" }, h("i", { class: "dot", style: `background:${color(u.user)}` }), u.user),
    h("td", { title: u.lastLogin ? fmtWhen(u.lastLogin) : "" }, fmtAgo(u.lastLogin)),
    h("td", { title: u.lastSeen ? fmtWhen(u.lastSeen) : "" }, fmtAgo(u.lastSeen)),
    h("td", { class: "devs" }, u.devices.join(", ") || "–"),
    h("td", { class: "num" }, String(u.plays)),
    h("td", { class: "num" }, fmtHours(u.seconds)),
    h("td", { class: "num" }, fmtBytes(u.bytes))));
  return h("section", { class: "card" },
    h("h2", {}, t("admin.friends")),
    d.users.length
      ? h("div", { class: "table-wrap" }, h("table", { class: "admin-table" }, h("thead", {}, head), h("tbody", {}, rows)))
      : h("p", { class: "muted" }, t("admin.noActivity")));
}

function nowPlaying(d) {
  return h("section", { class: "card" },
    h("h2", {}, t("admin.nowPlaying")),
    d.nowPlaying.length
      ? h("ul", { class: "admin-list" }, d.nowPlaying.map((n) => h("li", {},
          h("strong", {}, n.user),
          h("span", {}, `${n.title || t("admin.unknown")}${n.artist ? ` – ${n.artist}` : ""}`),
          h("span", { class: "muted" }, [n.device, n.saver ? t("admin.dataSaver") : ""].filter(Boolean).join(" · ")))))
      : h("p", { class: "muted" }, t("admin.nobodyNow")));
}

function loginLog(d) {
  return h("section", { class: "card" },
    h("h2", {}, t("admin.recentLogins")),
    d.logins.length
      ? h("ul", { class: "admin-list" }, d.logins.map((e) => h("li", { class: e.ok ? "" : "failed" },
          h("strong", {}, e.user),
          h("span", {}, fmtWhen(e.at)),
          h("span", { class: "muted" }, [e.ok ? (e.remember ? t("admin.kept") : t("admin.short")) : t("admin.wrongPassword"), e.device].filter(Boolean).join(" · ")))))
      : h("p", { class: "muted" }, t("admin.noLogins")));
}

const fmtMs = (ms) => (ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`);
const tierName = (tier, format) => [tier ? t(`quality.${tier}`) : "", (format || "").toUpperCase()].filter(Boolean).join(" ");

function startTimes(d) {
  const st = d.starts || { groups: [], recent: [] };
  const head = h("tr", {}, ["device", "quality", "starts", "median", "slow"].map((k) => h("th", { scope: "col" }, t(`admin.col.${k}`))));
  const rows = st.groups.map((g) => h("tr", {},
    h("th", { scope: "row" }, g.device || "–"),
    h("td", {}, tierName(g.tier, "") || "–"),
    h("td", { class: "num" }, String(g.count)),
    h("td", { class: "num" }, fmtMs(g.median)),
    h("td", { class: "num" }, fmtMs(g.slow))));
  const recent = st.recent.map((e) => h("li", {},
    h("strong", {}, e.user),
    h("span", {}, `${fmtMs(e.ms)} · ${tierName(e.tier, e.format) || "–"}`),
    h("span", { class: "muted" }, [fmtWhen(e.at), e.device, e.preload ? t("admin.preloaded") : "", e.auto ? t("admin.autoNext") : "", e.waits ? t("admin.stalls", { n: e.waits }) : ""].filter(Boolean).join(" · "))));
  return h("section", { class: "card" },
    h("h2", {}, t("admin.starts")),
    h("p", { class: "muted" }, t("admin.startsIntro")),
    st.recent.length
      ? [
          st.groups.length ? h("div", { class: "table-wrap" }, h("table", { class: "admin-table" }, h("thead", {}, head), h("tbody", {}, rows))) : null,
          h("h3", { class: "admin-sub" }, t("admin.recentStarts")),
          h("ul", { class: "admin-list" }, recent),
        ]
      : h("p", { class: "muted" }, t("admin.noStarts")));
}

const fmtStamp = (ms) => new Date(ms).toLocaleString(lang, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });

function feedbackList(list) {
  return h("section", { class: "card" },
    h("h2", {}, t("admin.feedback")),
    !list
      ? h("p", { class: "muted" }, t("admin.feedbackFailed"))
      : list.length
        ? h("ul", { class: "admin-list feedback-list" }, list.map((f) => h("li", {},
            h("strong", {}, f.user),
            h("span", { class: "muted" }, [fmtStamp(f.createdAt), t(`feedback.kind.${f.category}`), f.version, f.userAgent].filter(Boolean).join(" · ")),
            h("p", { class: "feedback-msg" }, f.text))))
        : h("p", { class: "muted" }, t("admin.noFeedback")));
}

let timer = null;

export async function renderAdmin(container, opts = {}) {
  clearInterval(timer);
  const title = h("div", { class: "admin-head" },
    h("h1", {}, t("tab.admin")),
    h("button", { type: "button", onclick: () => renderAdmin(container, opts) }, t("admin.refresh")));
  if (!container.firstChild) container.replaceChildren(title, h("div", { class: "muted" }, t("common.loading")));
  try {
    const [d, fb] = await Promise.all([
      api("/api/admin/stats"),
      api("/api/admin/feedback").then((r) => r.feedback).catch((e) => { if (e instanceof Unauthorized) throw e; return null; }),
    ]);
    const color = (u) => COLORS[d.users.findIndex((x) => x.user === u) % COLORS.length];
    const tot = d.totals;
    container.replaceChildren(
      title,
      h("p", { class: "muted" }, t("admin.intro")),
      h("div", { class: "stats" },
        tile(t("admin.col.listened"), fmtHours(tot.seconds), t("admin.playCount", { n: tot.plays })),
        tile(t("admin.streamed"), fmtBytes(tot.bytes)),
        tile(t("admin.active"), String(tot.active), t("admin.of", { n: d.users.length })),
        tile(t("admin.logins"), String(tot.logins), tot.failed ? t("admin.failed", { n: tot.failed }) : t("admin.noneFailed"))),
      nowPlaying(d),
      chart(d, color),
      userTable(d, color),
      startTimes(d),
      feedbackList(fb),
      loginLog(d));
    timer = setInterval(() => {
      if (container.hidden) return clearInterval(timer);
      if (document.visibilityState === "visible") renderAdmin(container, opts);
    }, 30000);
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(title,
      h("div", { class: "card muted" }, e.message === "admin-only" ? t("admin.adminOnly") : t("admin.loadFailed")));
  }
}
