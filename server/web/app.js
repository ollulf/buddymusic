import { renderUpload } from "./upload.js";
import { renderLibrary, setNewAlbums, setVinylAlbums, prefetchLibrary, dropLibraryPrefetch } from "./library.js";
import { renderSearch, searchBox } from "./search.js";
import { renderFavorites } from "./favorites.js";
import { renderQueue } from "./queue.js";
import "./fly.js";
import { renderBar, playList, settings, setQuality, onChange, stopAll, QUALITIES, qualityHint } from "./player.js";
import { api, Unauthorized } from "./dom.js";
import { renderPlaylists, setPlaylistUser } from "./playlists.js";
import { renderAdmin } from "./admin.js";
import { t, relTime, lang } from "./i18n.js";
import { renderSettings, GEAR, AVATAR, paintAvatar, openFeedback } from "./settings.js";
import { registerWorker, onOpenRecs } from "./push.js";
import { recsPanel, recBadge, setUnread, refreshUnread, startRecPolling } from "./recs.js";

const root = document.getElementById("app");

const h = (tag, attrs = {}, ...kids) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, "");
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) if (kid != null) el.append(kid);
  return el;
};

const LOGIN_ERRORS = {
  "bad-login": t("login.badLogin"),
  "too-many-attempts": t("login.tooMany"),
  "navidrome-unreachable": t("login.navidrome"),
};

function showLogin() {
  stopAll();
  const user = h("input", { name: "username", autocomplete: "username", required: true, autocapitalize: "none" });
  const pass = h("input", { name: "password", type: "password", autocomplete: "current-password", required: true });
  const remember = h("input", { name: "remember", type: "checkbox", checked: true });
  const err = h("div", { class: "err", role: "alert" });
  const btn = h("button", { class: "primary", type: "submit" }, t("login.button"));
  const form = h("form", { class: "card login", onsubmit: async (e) => {
    e.preventDefault();
    btn.disabled = true;
    err.textContent = "";
    try {
      const res = await fetch("/api/login", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: user.value, password: pass.value, remember: remember.checked }),
      });
      const body = await res.json().catch(() => ({}));
      if (res.ok) return showApp(body.user, body.admin, body.avatar, body);
      err.textContent = LOGIN_ERRORS[body.error] || t("login.failed");
    } catch {
      err.textContent = t("login.noConnection");
    }
    btn.disabled = false;
  } },
    h("div", { class: "brand" }, h("h2", {}, "buddymusic"), h("span", { class: "powered" }, t("app.powered"))),
    h("p", { class: "muted" }, t("login.intro")),
    h("label", { class: "field" }, h("span", {}, t("login.username")), user),
    h("label", { class: "field" }, h("span", {}, t("login.password")), pass),
    h("label", { class: "check" }, remember, h("span", {}, t("login.remember"))),
    err, btn,
    h("p", { class: "muted privacy" }, t("login.privacy")));
  root.replaceChildren(h("main", {}, form));
  user.focus();
}

async function logout() {
  await fetch("/api/logout", { method: "POST" }).catch(() => {});
  showLogin();
}

const BASE_TABS = ["library", "search", "favorites", "playlists", "upload"].map((k) => [k, t(`tab.${k}`)]);
let titleHandler = null;
let routeHandler = null;
let playerBar = null;
let openQuality = null;

const SVG = "http://www.w3.org/2000/svg";

const TAB_ICONS = {
  library: ["............", "############", "#.#........#", "#.#........#", "#.#........#", "#.#........#", "#.#........#", "#.#........#", "#.#........#", "#.#........#", "############", "............"],
  favorites: ["............", "............", "..##....##..", ".#..#..#..#.", ".#...##...#.", ".#........#.", "..#......#..", "..#......#..", "...#....#...", "....#..#....", ".....##.....", "............"],
  playlists: ["............", "............", "...#........", "...#.######.", "...#........", "...#.#####..", "...#........", "...#.####...", ".###........", ".###........", "............", "............"],
  upload: ["............", "..########..", ".#........#.", ".#...##...#.", ".#..####..#.", ".#.######.#.", ".#...##...#.", ".#...##...#.", ".#...##...#.", ".#........#.", "..########..", "............"],
  admin: ["............", "........##..", "........##..", "........##..", ".....##.##..", ".....##.##..", ".....##.##..", "..##.##.##..", "..##.##.##..", "..##.##.##..", "..##.##.##..", "............"],
};

function pixelIcon(rows) {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", `0 0 ${rows[0].length} ${rows.length}`);
  svg.setAttribute("width", rows[0].length * 2);
  svg.setAttribute("height", rows.length * 2);
  svg.setAttribute("shape-rendering", "crispEdges");
  svg.setAttribute("aria-hidden", "true");
  rows.forEach((row, y) => [...row].forEach((c, x) => {
    if (c !== "#") return;
    const r = document.createElementNS(SVG, "rect");
    r.setAttribute("x", x); r.setAttribute("y", y); r.setAttribute("width", 1); r.setAttribute("height", 1);
    r.setAttribute("fill", "currentColor");
    svg.append(r);
  }));
  return svg;
}

function diceIcon() {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("width", "16");
  svg.setAttribute("height", "16");
  svg.setAttribute("shape-rendering", "crispEdges");
  svg.setAttribute("aria-hidden", "true");
  const rects = [[2, 1, 12, 1], [1, 2, 1, 12], [14, 2, 1, 12], [2, 14, 12, 1], [4, 4, 2, 2], [10, 4, 2, 2], [7, 7, 2, 2], [4, 10, 2, 2], [10, 10, 2, 2]];
  for (const [x, y, w, hh] of rects) {
    const r = document.createElementNS(SVG, "rect");
    r.setAttribute("x", x); r.setAttribute("y", y); r.setAttribute("width", w); r.setAttribute("height", hh);
    r.setAttribute("fill", "currentColor");
    svg.append(r);
  }
  return svg;
}

function randomButton() {
  const btn = h("button", { class: "dice", type: "button", title: t("dice.title"), "aria-label": t("dice.title") }, diceIcon());
  btn.addEventListener("click", async () => {
    if (btn.classList.contains("busy")) return;
    btn.classList.add("busy");
    btn.classList.remove("fail");
    btn.title = t("dice.title");
    try {
      const res = await api("/api/music/random-songs?size=50");
      const songs = res?.song || [];
      if (!songs.length) throw new Error("empty");
      playList(songs, 0);
    } catch (err) {
      if (err instanceof Unauthorized) return showLogin();
      btn.title = t("dice.failed");
      void btn.offsetWidth;
      btn.classList.add("fail");
    } finally {
      btn.classList.remove("busy");
    }
  });
  return btn;
}

let closeProfile = null;

document.addEventListener("click", (e) => { if (closeProfile && !e.target.closest(".profile")) closeProfile(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && closeProfile) closeProfile(true); });

function friendRow(f) {
  const pic = h("span", { class: "friend-pic" });
  if (f.avatar) {
    const img = h("img", { src: `/api/avatar/${encodeURIComponent(f.user)}?v=${f.avatar}`, alt: "", width: 32, height: 32 });
    img.addEventListener("error", () => { pic.innerHTML = AVATAR; });
    pic.append(img);
  } else pic.innerHTML = AVATAR;
  let what;
  if (!f.online) what = t("friends.lastSeen", { when: relTime(f.lastSeen) });
  else if (f.state === "playing" || f.state === "paused") {
    const track = f.artist ? `${f.title} – ${f.artist}` : f.title;
    what = f.state === "paused" ? t("friends.paused", { track }) : track;
  } else what = t("friends.idle");
  return h("li", { class: `friend${f.online ? " online" : ""}${f.state === "playing" ? " playing" : ""}` },
    pic,
    h("span", { class: "friend-text" },
      h("span", { class: "friend-name" }, h("span", { class: "dot", "aria-hidden": "true" }), f.user),
      h("span", { class: "friend-what", title: what }, what)));
}

function friendsPanel() {
  const list = h("ul", { class: "friends" });
  const empty = h("p", { class: "muted friends-empty" }, t("friends.none"));
  let timer = null;
  const stop = () => {
    clearInterval(timer);
    timer = null;
  };
  const load = async () => {
    if (!list.isConnected) return stop();
    try {
      const res = await fetch("/api/friends");
      if (res.status === 401) {
        stop();
        return showLogin();
      }
      if (!res.ok) throw new Error();
      const body = await res.json();
      list.replaceChildren(...body.friends.map(friendRow));
      empty.hidden = body.friends.some((f) => f.online);
      empty.textContent = body.friends.length ? t("friends.nobody") : t("friends.none");
    } catch {
      empty.hidden = false;
      empty.textContent = t("friends.failed");
    }
  };
  const el = h("section", { class: "friends-panel", "aria-label": t("friends.title") },
    h("h3", {}, t("friends.title")), list, empty);
  return {
    el,
    start() {
      stop();
      timer = setInterval(load, 30000);
      load();
    },
    stop,
  };
}

let changelog = null;

async function loadChangelog() {
  if (changelog) return changelog;
  const res = await fetch("/changelog.json", { cache: "no-cache" });
  if (!res.ok) throw new Error(String(res.status));
  changelog = await res.json();
  return changelog;
}

function openChangelog(version, returnTo) {
  const body = h("div", { class: "news-body", tabindex: "0" }, h("p", { class: "muted" }, t("common.loading")));
  const closeBtn = h("button", { class: "ghost news-close", type: "button", "aria-label": t("news.close"), title: t("news.close"), autofocus: true, onclick: () => dlg.close() }, "✕");
  const dlg = h("dialog", { class: "sheet card news", "aria-labelledby": "news-title" },
    h("div", { class: "news-head" }, h("h2", { id: "news-title" }, t("news.title")), closeBtn),
    body);
  dlg.addEventListener("click", (e) => {
    if (e.target !== dlg) return;
    const r = dlg.getBoundingClientRect();
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) dlg.close();
  });
  dlg.addEventListener("close", () => { dlg.remove(); returnTo?.focus(); });
  document.body.append(dlg);
  dlg.showModal();
  closeBtn.focus();
  loadChangelog().then((list) => {
    const entries = Array.isArray(list) ? list : [];
    if (!entries.length) return body.replaceChildren(h("p", { class: "muted" }, t("news.empty")));
    body.replaceChildren(...entries.map((r) => h("section", { class: r.version === version ? "news-rel current" : "news-rel" },
      h("h3", {}, h("span", { class: "news-ver" }, r.version), r.date ? h("span", { class: "news-date" }, r.date) : null,
        r.version === version ? h("span", { class: "tag" }, t("news.current")) : null),
      h("ul", {}, (r[lang] || r.en || []).map((line) => h("li", {}, line))))));
  }).catch(() => body.replaceChildren(h("p", { class: "muted" }, t("news.failed"))));
}

let openRecsMenu = null;
let pendingRecs = false;
registerWorker();
onOpenRecs(() => { if (openRecsMenu) openRecsMenu(); else pendingRecs = true; });

let headerAvatar = { version: 0, paint() {} };

function profileMenu(user, admin, avatar, version) {
  const btn = h("button", { class: "avatar", type: "button", "aria-haspopup": "menu", "aria-expanded": "false", "aria-label": t("profile.label"), title: user });
  headerAvatar = { version: avatar || 0, paint() { paintAvatar(btn, this.version); } };
  headerAvatar.paint();
  const friends = friendsPanel();
  const recs = recsPanel();
  const settingsItem = h("button", { class: "ghost settings-item", type: "button", role: "menuitem", onclick: () => { close(); location.hash = "#/settings"; } });
  settingsItem.innerHTML = GEAR;
  settingsItem.append(t("settings.title"));
  const menu = h("div", { class: "profile-menu", role: "menu", hidden: true },
    h("div", { class: "profile-name" }, user, admin ? h("span", { class: "tag" }, "admin") : null),
    recs.el, friends.el, settingsItem,
    h("button", { class: "ghost", type: "button", role: "menuitem", "aria-haspopup": "dialog", onclick: () => { close(); openFeedback(btn); } }, t("settings.feedback")),
    h("button", { class: "ghost", type: "button", role: "menuitem", onclick: logout }, t("profile.logout")),
    h("p", { class: "powered" }, t("app.powered")),
    h("p", { class: "version" }, t("app.version", { v: version || "dev" }), " ",
      h("button", { class: "linkish news-link", type: "button", role: "menuitem", "aria-haspopup": "dialog", onclick: () => { close(); openChangelog(version, btn); } }, t("news.open"))));
  const close = (focus) => {
    menu.hidden = true;
    friends.stop();
    btn.setAttribute("aria-expanded", "false");
    closeProfile = null;
    if (focus === true) btn.focus();
  };
  btn.addEventListener("click", () => {
    if (!menu.hidden) return close();
    menu.hidden = false;
    friends.start();
    recs.start();
    btn.setAttribute("aria-expanded", "true");
    closeProfile = close;
  });
  openRecsMenu = () => { if (menu.hidden) btn.click(); };
  if (pendingRecs) { pendingRecs = false; setTimeout(openRecsMenu, 0); }
  return h("div", { class: "profile" }, btn, recBadge(), menu);
}

const QUALITY_LETTERS = { auto: "A", original: "O", normal: "N", low: "L" };

function qualityButton() {
  const btn = h("button", { type: "button", class: "saver-btn", "aria-haspopup": "true", "aria-expanded": "false" });
  const menu = h("div", { class: "quality-menu", hidden: true, role: "menu" });
  const close = () => { menu.hidden = true; btn.setAttribute("aria-expanded", "false"); };
  const paint = () => {
    const q = settings().quality;
    btn.textContent = QUALITY_LETTERS[q];
    btn.classList.toggle("on", q === "normal" || q === "low");
    btn.setAttribute("aria-label", t("quality.label", { q: t(`quality.${q}`) }));
    btn.title = t("quality.label", { q: t(`quality.${q}`) });
    menu.replaceChildren(...QUALITIES.map((k) => h("button", {
      type: "button", role: "menuitemradio", "aria-checked": String(k === q), class: k === q ? "on" : "",
      onclick: () => { setQuality(k); close(); },
    }, `${t(`quality.${k}`)} `, h("span", { class: "muted" }, qualityHint(k)))));
  };
  btn.addEventListener("click", () => {
    if (!menu.hidden) return close();
    menu.hidden = false;
    btn.setAttribute("aria-expanded", "true");
  });
  openQuality = () => {
    menu.hidden = false;
    btn.setAttribute("aria-expanded", "true");
    const on = menu.querySelector("button.on") || menu.querySelector("button");
    if (on) on.focus();
  };
  document.addEventListener("click", (e) => { if (!wrap.contains(e.target)) close(); });
  const wrap = h("div", { class: "saver-toggle" }, btn, menu);
  wrap.addEventListener("keydown", (e) => { if (e.key === "Escape") { close(); btn.focus(); } });
  onChange(paint);
  paint();
  return wrap;
}

function showApp(user, admin, avatar, me = {}) {
  setNewAlbums(me.newSince, me.ownAlbums, me.openedAlbums, me.renewedAlbums);
  setVinylAlbums(me.vinylAlbums);
  setUnread(me.recsUnread);
  startRecPolling();
  const TABS = admin === true ? [...BASE_TABS, ["admin", t("tab.admin")]] : BASE_TABS;
  const opts = { onUnauthorized: showLogin, admin: admin === true };
  setPlaylistUser(user);
  const views = Object.fromEntries(TABS.map(([k]) => [k, h("main", { class: `view view-${k}`, hidden: true })]));
  const links = Object.fromEntries(TABS.filter(([k]) => k !== "search").map(([k, label]) => [k, h("a", { class: "tab", href: `#/${k}`, title: label, "aria-label": label }, pixelIcon(TAB_ICONS[k]), h("span", { class: "tab-label", "aria-hidden": "true" }, label))]));
  for (const a of Object.values(links)) a.addEventListener("click", () => window.dispatchEvent(new Event("bm:tuck")));
  const titleEl = h("h1", { class: "page-title" });
  const setTitle = (text) => { titleEl.textContent = text; titleEl.title = text; document.title = `${text} · buddymusic`; };
  if (titleHandler) window.removeEventListener("page-title", titleHandler);
  titleHandler = (e) => setTitle(e.detail);
  window.addEventListener("page-title", titleHandler);
  const titleRow = h("div", { class: "title-row" }, titleEl, searchBox());
  const settingsView = h("main", { class: "view view-settings", hidden: true });
  let cameFromApp = false;
  const closeSettings = () => {
    if (cameFromApp) history.back();
    else location.hash = "#/library";
  };
  settingsView.addEventListener("keydown", (e) => { if (e.key === "Escape") closeSettings(); });
  if (!playerBar) { playerBar = renderBar({ onUnauthorized: showLogin, onQuality: () => openQuality && openQuality() }); playerBar.append(renderQueue()); }
  root.replaceChildren(
    h("header", { class: "top" },
      h("nav", { class: "tabs", "aria-label": t("tab.sections") }, Object.values(links)),
      randomButton(),
      qualityButton(),
      profileMenu(user, admin, avatar, me.version)),
    titleRow,
    ...Object.values(views),
    settingsView,
    playerBar);
  renderUpload(views.upload, opts);
  let shown = "";
  let lastTab = "";
  const route = () => {
    const parts = location.hash.replace(/^#\/?/, "").split("/").map(decodeURIComponent);
    const inSettings = parts[0] === "settings";
    if (inSettings && settingsView.hidden) {
      cameFromApp = lastTab !== "";
      settingsView.hidden = false;
      document.body.classList.add("settings-open");
      renderSettings(settingsView, {
        onClose: closeSettings,
        user,
        avatar: headerAvatar.version,
        onAvatar: (v) => { headerAvatar.version = v; headerAvatar.paint(); },
        onLogin: showLogin,
      });
    } else if (!inSettings && !settingsView.hidden) {
      settingsView.hidden = true;
      document.body.classList.remove("settings-open");
    }
    if (inSettings) {
      setTitle(t("settings.title"));
      titleRow.classList.remove("no-search");
      return;
    }
    const tab = TABS.some(([k]) => k === parts[0]) ? parts[0] : "library";
    for (const [k] of TABS) {
      views[k].hidden = k !== tab;
      if (!links[k]) continue;
      links[k].classList.toggle("on", k === tab);
      if (k === tab) links[k].setAttribute("aria-current", "page");
      else links[k].removeAttribute("aria-current");
    }
    setTitle(TABS.find(([k]) => k === tab)[1]);
    titleRow.classList.toggle("no-search", tab === "upload" || tab === "admin");
    const key = [tab, ...parts.slice(1)].join("/");
    if (tab === "library" && key !== shown) {
      renderLibrary(views.library, parts.slice(1), opts);
      window.scrollTo(0, 0);
    }
    if (tab === "search" && lastTab !== "search") renderSearch(views.search, opts);
    if (tab === "favorites" && lastTab !== "favorites") renderFavorites(views.favorites, opts);
    if (tab === "playlists" && key !== shown) {
      renderPlaylists(views.playlists, parts.slice(1), opts);
      window.scrollTo(0, 0);
    }
    if (tab === "admin" && lastTab !== "admin") renderAdmin(views.admin, opts);
    if (tab === "library" || tab === "playlists") shown = key;
    lastTab = tab;
  };
  if (routeHandler) window.removeEventListener("hashchange", routeHandler);
  routeHandler = route;
  window.addEventListener("hashchange", route);
  route();
}

async function boot() {
  const parts = location.hash.replace(/^#\/?/, "").split("/").map(decodeURIComponent);
  if (!parts[0] || parts[0] === "library") prefetchLibrary(parts.slice(1));
  try {
    const res = await fetch("/api/me");
    if (res.ok) {
      const me = await res.json();
      return showApp(me.user, me.admin, me.avatar, me);
    }
  } catch {}
  dropLibraryPrefetch();
  showLogin();
}

let scrollTick = false;
function syncScrolled() {
  scrollTick = false;
  document.body.classList.toggle('scrolled', window.scrollY > 8);
}
window.addEventListener('scroll', () => {
  if (scrollTick) return;
  scrollTick = true;
  requestAnimationFrame(syncScrolled);
}, { passive: true });
syncScrolled();

boot();
