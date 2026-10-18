import { renderUpload } from "./upload.js";
import { renderLibrary } from "./library.js";
import { renderSearch } from "./search.js";
import { renderBar } from "./player.js";

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
  const err = h("div", { class: "err", role: "alert" });
  const btn = h("button", { class: "primary", type: "submit" }, "Log in");
  const form = h("form", { class: "card login", onsubmit: async (e) => {
    e.preventDefault();
    btn.disabled = true;
    err.textContent = "";
    try {
      const res = await fetch("/api/login", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: user.value, password: pass.value }),
      });
      const body = await res.json().catch(() => ({}));
      if (res.ok) return showApp(body.user, body.admin);
      err.textContent = LOGIN_ERRORS[body.error] || "Login failed";
    } catch {
      err.textContent = "No connection to the server";
    }
    btn.disabled = false;
  } },
    h("h2", {}, "buddymusic"),
    h("p", { class: "muted" }, "Log in with your Navidrome account."),
    h("label", { class: "field" }, h("span", {}, "Username"), user),
    h("label", { class: "field" }, h("span", {}, "Password"), pass),
    err, btn);
  root.replaceChildren(h("main", {}, form));
  user.focus();
}

async function logout() {
  await fetch("/api/logout", { method: "POST" }).catch(() => {});
  showLogin();
}

const TABS = [["library", "Library"], ["search", "Search"], ["upload", "Upload"]];
let routeHandler = null;
let playerBar = null;

function showApp(user, admin) {
  const opts = { onUnauthorized: showLogin, admin: admin === true };
  const views = Object.fromEntries(TABS.map(([k]) => [k, h("main", { class: `view view-${k}`, hidden: true })]));
  const links = Object.fromEntries(TABS.map(([k, label]) => [k, h("a", { class: "tab", href: `#/${k}` }, label)]));
  playerBar ||= renderBar();
  root.replaceChildren(
    h("header", { class: "top" },
      h("strong", {}, "buddymusic"),
      h("nav", { class: "tabs" }, Object.values(links)),
      h("span", { class: "who" }, admin ? `${user} (admin)` : user),
      h("button", { class: "ghost", onclick: logout }, "Log out")),
    ...Object.values(views),
    playerBar);
  renderUpload(views.upload, opts);
  let shown = "";
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
    if (tab === "library") shown = key;
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
