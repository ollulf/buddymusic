import { renderUpload } from "./upload.js";
import { renderLibrary } from "./library.js";
import { renderSearch } from "./search.js";
import { renderFavorites } from "./favorites.js";
import { renderBar, playList } from "./player.js";
import { api, Unauthorized } from "./dom.js";
import { renderPlaylists, setPlaylistUser } from "./playlists.js";

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
  "bad-login": "Wrong username or password",
  "too-many-attempts": "Too many attempts, wait a few minutes",
  "navidrome-unreachable": "Navidrome is not answering",
};

function showLogin() {
  const user = h("input", { name: "username", autocomplete: "username", required: true, autocapitalize: "none" });
  const pass = h("input", { name: "password", type: "password", autocomplete: "current-password", required: true });
  const remember = h("input", { name: "remember", type: "checkbox", checked: true });
  const err = h("div", { class: "err", role: "alert" });
  const btn = h("button", { class: "primary", type: "submit" }, "Log in");
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
      if (res.ok) return showApp(body.user, body.admin);
      err.textContent = LOGIN_ERRORS[body.error] || "Login failed";
    } catch {
      err.textContent = "No connection to the server";
    }
    btn.disabled = false;
  } },
    h("div", { class: "brand" }, h("h2", {}, "buddymusic"), h("span", { class: "powered" }, "powered by Navidrome")),
    h("p", { class: "muted" }, "Log in with your Navidrome account."),
    h("label", { class: "field" }, h("span", {}, "Username"), user),
    h("label", { class: "field" }, h("span", {}, "Password"), pass),
    h("label", { class: "check" }, remember, h("span", {}, "Keep me logged in")),
    err, btn);
  root.replaceChildren(h("main", {}, form));
  user.focus();
}

async function logout() {
  await fetch("/api/logout", { method: "POST" }).catch(() => {});
  showLogin();
}

const TABS = [["library", "Library"], ["search", "Search"], ["favorites", "Favorites"], ["playlists", "Playlists"], ["upload", "Upload"]];
let routeHandler = null;
let playerBar = null;

const SVG = "http://www.w3.org/2000/svg";

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
  const btn = h("button", { class: "dice", type: "button", title: "Play random songs", "aria-label": "Play random songs" }, diceIcon());
  btn.addEventListener("click", async () => {
    if (btn.classList.contains("busy")) return;
    btn.classList.add("busy");
    btn.classList.remove("fail");
    btn.title = "Play random songs";
    try {
      const res = await api("/api/music/random-songs?size=50");
      const songs = res?.song || [];
      if (!songs.length) throw new Error("empty");
      playList(songs, 0);
    } catch (err) {
      if (err instanceof Unauthorized) return showLogin();
      btn.title = "Couldn't load";
      void btn.offsetWidth;
      btn.classList.add("fail");
    } finally {
      btn.classList.remove("busy");
    }
  });
  return btn;
}

function showApp(user, admin) {
  const opts = { onUnauthorized: showLogin, admin: admin === true };
  setPlaylistUser(user);
  const views = Object.fromEntries(TABS.map(([k]) => [k, h("main", { class: `view view-${k}`, hidden: true })]));
  const links = Object.fromEntries(TABS.map(([k, label]) => [k, h("a", { class: "tab", href: `#/${k}` }, label)]));
  playerBar ||= renderBar({ onUnauthorized: showLogin });
  root.replaceChildren(
    h("header", { class: "top" },
      h("span", { class: "brand" }, h("strong", {}, "buddymusic"), h("span", { class: "powered" }, "powered by Navidrome")),
      h("nav", { class: "tabs" }, Object.values(links)),
      randomButton(),
      h("span", { class: "who" }, admin ? `${user} (admin)` : user),
      h("button", { class: "ghost", onclick: logout }, "Log out")),
    ...Object.values(views),
    playerBar);
  renderUpload(views.upload, opts);
  let shown = "";
  let lastTab = "";
  const route = () => {
    const parts = location.hash.replace(/^#\/?/, "").split("/").map(decodeURIComponent);
    const tab = TABS.some(([k]) => k === parts[0]) ? parts[0] : "library";
    for (const [k] of TABS) {
      views[k].hidden = k !== tab;
      links[k].classList.toggle("on", k === tab);
      if (k === tab) links[k].setAttribute("aria-current", "page");
      else links[k].removeAttribute("aria-current");
    }
    const key = [tab, ...parts.slice(1)].join("/");
    if (tab === "library" && key !== shown) {
      renderLibrary(views.library, parts.slice(1), opts);
      window.scrollTo(0, 0);
    }
    if (tab === "search" && !views.search.firstChild) renderSearch(views.search, opts);
    if (tab === "favorites" && lastTab !== "favorites") renderFavorites(views.favorites, opts);
    if (tab === "playlists" && key !== shown) {
      renderPlaylists(views.playlists, parts.slice(1), opts);
      window.scrollTo(0, 0);
    }
    if (tab === "library" || tab === "playlists") shown = key;
    lastTab = tab;
  };
  if (routeHandler) window.removeEventListener("hashchange", routeHandler);
  routeHandler = route;
  window.addEventListener("hashchange", route);
  route();
}

async function boot() {
  try {
    const res = await fetch("/api/me");
    if (res.ok) {
      const me = await res.json();
      return showApp(me.user, me.admin);
    }
  } catch {}
  showLogin();
}

boot();
