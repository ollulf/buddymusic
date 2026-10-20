import { renderUpload } from "./upload.js";
import { renderLibrary } from "./library.js";
import { renderSearch, searchBox } from "./search.js";
import { renderFavorites } from "./favorites.js";
import { renderBar, settings, setQuality, onChange, QUALITIES, qualityHint } from "./player.js";
import { renderPlaylists, setPlaylistUser } from "./playlists.js";
import { renderAdmin } from "./admin.js";
import { renderSettings, GEAR } from "./settings.js";

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
      if (res.ok) return showApp(body.user, body.admin, body.avatar);
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
    err, btn,
    h("p", { class: "muted privacy" }, "Admins can see when you log in and how much you listen. Friends can see what you are playing (you can turn that off in your profile)."));
  root.replaceChildren(h("main", {}, form));
  user.focus();
}

async function logout() {
  await fetch("/api/logout", { method: "POST" }).catch(() => {});
  showLogin();
}

const BASE_TABS = [["library", "Library"], ["search", "Search"], ["favorites", "Favorites"], ["playlists", "Playlists"], ["upload", "Upload"]];
let routeHandler = null;
let playerBar = null;

const AVATAR = '<svg viewBox="0 0 8 8" width="20" height="20" shape-rendering="crispEdges" aria-hidden="true"><path fill="currentColor" d="M3 1h2v1h1v2h-1v1h-2v-1h-1v-2h1zM1 6h1v-1h4v1h1v2h-6z"/></svg>';
let closeProfile = null;

document.addEventListener("click", (e) => { if (closeProfile && !e.target.closest(".profile")) closeProfile(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && closeProfile) closeProfile(true); });

const AVATAR_SIZE = 128;

async function squareAvatar(file) {
  const bmp = await createImageBitmap(file);
  const side = Math.min(bmp.width, bmp.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = AVATAR_SIZE;
  const ctx = canvas.getContext("2d");
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, AVATAR_SIZE, AVATAR_SIZE);
  bmp.close();
  const blob = (type, q) => new Promise((ok) => canvas.toBlob(ok, type, q));
  const png = await blob("image/png");
  return png && png.size <= 200 * 1024 ? png : blob("image/jpeg", 0.9);
}

const ago = (sec) => {
  const min = Math.max(0, Math.round((Date.now() / 1000 - sec) / 60));
  if (min < 1) return "just now";
  return `${min} min ago`;
};

function friendRow(f) {
  const pic = h("span", { class: "friend-pic" });
  if (f.avatar) {
    const img = h("img", { src: `/api/avatar/${encodeURIComponent(f.user)}?v=${f.avatar}`, alt: "", width: 32, height: 32 });
    img.addEventListener("error", () => { pic.innerHTML = AVATAR; });
    pic.append(img);
  } else pic.innerHTML = AVATAR;
  let what;
  if (!f.online) what = `last seen ${ago(f.lastSeen)}`;
  else if (f.state === "playing" || f.state === "paused") {
    const track = f.artist ? `${f.title} – ${f.artist}` : f.title;
    what = f.state === "paused" ? `paused: ${track}` : track;
  } else what = "idle";
  return h("li", { class: `friend${f.online ? " online" : ""}${f.state === "playing" ? " playing" : ""}` },
    pic,
    h("span", { class: "friend-text" },
      h("span", { class: "friend-name" }, h("span", { class: "dot", "aria-hidden": "true" }), f.user),
      h("span", { class: "friend-what", title: what }, what)));
}

function friendsPanel() {
  const list = h("ul", { class: "friends" });
  const empty = h("p", { class: "muted friends-empty" }, "No friends online right now.");
  const share = h("input", { type: "checkbox", checked: true });
  const toggle = h("label", { class: "check share-toggle" }, share, h("span", {}, "Share my listening"));
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
      share.checked = body.share !== false;
      list.replaceChildren(...body.friends.map(friendRow));
      empty.hidden = body.friends.some((f) => f.online);
      empty.textContent = body.friends.length ? "Nobody else is online right now." : "No friends online right now.";
    } catch {
      empty.hidden = false;
      empty.textContent = "Could not load friends";
    }
  };
  share.addEventListener("change", async () => {
    const want = share.checked;
    share.disabled = true;
    try {
      const res = await fetch("/api/friends/share", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ share: want }) });
      if (res.status === 401) return showLogin();
      if (!res.ok) throw new Error();
    } catch {
      share.checked = !want;
    } finally {
      share.disabled = false;
    }
  });
  const el = h("section", { class: "friends-panel", "aria-label": "Friends online" },
    h("h3", {}, "Friends online"), list, empty, toggle);
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

function profileMenu(user, admin, avatar) {
  let version = avatar || 0;
  const btn = h("button", { class: "avatar", type: "button", "aria-haspopup": "menu", "aria-expanded": "false", "aria-label": "Profile", title: user });
  const status = h("div", { class: "profile-status", role: "status" });
  const picker = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp", hidden: true });
  const change = h("button", { class: "ghost", type: "button", role: "menuitem", onclick: () => picker.click() }, "Change picture");
  const reset = h("button", { class: "ghost", type: "button", role: "menuitem" }, "Reset picture");
  const paint = () => {
    btn.replaceChildren();
    if (version) {
      const img = h("img", { src: `/api/avatar?v=${version}`, alt: "", width: AVATAR_SIZE, height: AVATAR_SIZE });
      img.addEventListener("error", () => { btn.innerHTML = AVATAR; });
      btn.append(img);
    } else btn.innerHTML = AVATAR;
    btn.classList.toggle("has-pic", !!version);
    reset.hidden = !version;
  };
  const send = async (method, body) => {
    status.textContent = method === "PUT" ? "Saving…" : "";
    change.disabled = reset.disabled = true;
    try {
      const res = await fetch("/api/avatar", { method, body, headers: body ? { "Content-Type": body.type } : {} });
      if (res.status === 401) return showLogin();
      if (!res.ok) throw new Error();
      version = method === "PUT" ? (await res.json()).avatar : 0;
      status.textContent = "";
      paint();
    } catch {
      status.textContent = "Could not save the picture";
    } finally {
      change.disabled = reset.disabled = false;
    }
  };
  picker.addEventListener("change", async () => {
    const file = picker.files[0];
    picker.value = "";
    if (!file) return;
    let blob;
    try {
      blob = await squareAvatar(file);
    } catch {}
    if (!blob) {
      status.textContent = "That file is not a picture";
      return;
    }
    send("PUT", blob);
  });
  reset.addEventListener("click", () => send("DELETE"));
  paint();
  const friends = friendsPanel();
  const settingsItem = h("button", { class: "ghost settings-item", type: "button", role: "menuitem", onclick: () => { close(); location.hash = "#/settings"; } });
  settingsItem.innerHTML = GEAR;
  settingsItem.append("Settings");
  const menu = h("div", { class: "profile-menu", role: "menu", hidden: true },
    h("div", { class: "profile-name" }, user, admin ? h("span", { class: "tag" }, "admin") : null),
    change, reset, picker, status, friends.el, settingsItem,
    h("button", { class: "ghost", type: "button", role: "menuitem", onclick: logout }, "Log out"));
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
    btn.setAttribute("aria-expanded", "true");
    closeProfile = close;
  });
  return h("div", { class: "profile" }, btn, menu);
}

const QUALITY_LABELS = { auto: ["A", "Auto"], original: ["O", "Original"], normal: ["N", "Normal"], low: ["L", "Low"] };

function qualityButton() {
  const btn = h("button", { type: "button", class: "saver-btn", "aria-haspopup": "true", "aria-expanded": "false" });
  const menu = h("div", { class: "quality-menu", hidden: true, role: "menu" });
  const close = () => { menu.hidden = true; btn.setAttribute("aria-expanded", "false"); };
  const paint = () => {
    const q = settings().quality;
    btn.textContent = QUALITY_LABELS[q][0];
    btn.classList.toggle("on", q === "normal" || q === "low");
    btn.setAttribute("aria-label", `Streaming quality: ${QUALITY_LABELS[q][1]}`);
    btn.title = `Streaming quality: ${QUALITY_LABELS[q][1]}`;
    menu.replaceChildren(...QUALITIES.map((k) => h("button", {
      type: "button", role: "menuitemradio", "aria-checked": String(k === q), class: k === q ? "on" : "",
      onclick: () => { setQuality(k); close(); },
    }, `${QUALITY_LABELS[k][1]} `, h("span", { class: "muted" }, qualityHint(k)))));
  };
  btn.addEventListener("click", () => {
    if (!menu.hidden) return close();
    menu.hidden = false;
    btn.setAttribute("aria-expanded", "true");
  });
  document.addEventListener("click", (e) => { if (!wrap.contains(e.target)) close(); });
  const wrap = h("div", { class: "saver-toggle" }, btn, menu);
  wrap.addEventListener("keydown", (e) => { if (e.key === "Escape") { close(); btn.focus(); } });
  onChange(paint);
  paint();
  return wrap;
}

function showApp(user, admin, avatar) {
  const TABS = admin === true ? [...BASE_TABS, ["admin", "Admin"]] : BASE_TABS;
  const opts = { onUnauthorized: showLogin, admin: admin === true };
  setPlaylistUser(user);
  const views = Object.fromEntries(TABS.map(([k]) => [k, h("main", { class: `view view-${k}`, hidden: true })]));
  const links = Object.fromEntries(TABS.filter(([k]) => k !== "search").map(([k, label]) => [k, h("a", { class: "tab", href: `#/${k}` }, label)]));
  const settingsView = h("main", { class: "view view-settings", hidden: true });
  let cameFromApp = false;
  const closeSettings = () => {
    if (cameFromApp) history.back();
    else location.hash = "#/library";
  };
  settingsView.addEventListener("keydown", (e) => { if (e.key === "Escape") closeSettings(); });
  playerBar ||= renderBar({ onUnauthorized: showLogin });
  root.replaceChildren(
    h("header", { class: "top" },
      h("span", { class: "brand" }, h("strong", {}, "buddymusic"), h("span", { class: "powered" }, "powered by Navidrome")),
      h("nav", { class: "tabs" }, Object.values(links)),
      searchBox(),
      qualityButton(),
      profileMenu(user, admin, avatar)),
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
      renderSettings(settingsView, { onClose: closeSettings });
    } else if (!inSettings && !settingsView.hidden) {
      settingsView.hidden = true;
      document.body.classList.remove("settings-open");
    }
    if (inSettings) return;
    const tab = TABS.some(([k]) => k === parts[0]) ? parts[0] : "library";
    for (const [k] of TABS) {
      views[k].hidden = k !== tab;
      if (!links[k]) continue;
      links[k].classList.toggle("on", k === tab);
      if (k === tab) links[k].setAttribute("aria-current", "page");
      else links[k].removeAttribute("aria-current");
    }
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
  try {
    const res = await fetch("/api/me");
    if (res.ok) {
      const me = await res.json();
      return showApp(me.user, me.admin, me.avatar);
    }
  } catch {}
  showLogin();
}

boot();
