import { t, relTime } from "./i18n.js";
import { h, api, apiJSON, fmtTime, cover, Unauthorized, setPageTitle } from "./dom.js";
import { songRow } from "./library.js";
import * as player from "./player.js";

let me = "";
export const setPlaylistUser = (user) => { me = (user || "").toLowerCase(); };
const mine = (pl) => (pl.owner || "").toLowerCase() === me;

const ERRORS = {
  "not-owner": t("playlists.notOwner"),
  "not-found": t("playlists.notFound"),
  "navidrome-unreachable": t("common.navidrome"),
};

function report(e, opts, what) {
  if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return; }
  alert(t(`playlists.fail.${what}`, { error: ERRORS[e.message] || e.message }));
}

const listPlaylists = async () => (await api("/api/music/playlists")).playlist || [];

function shuffle(songs) {
  if (!songs.length) return;
  player.playList(songs, Math.floor(Math.random() * songs.length));
  if (!player.settings().shuffle) player.toggleShuffle();
}

async function createPlaylist(name, songIds, opts) {
  try {
    return await apiJSON("POST", "/api/music/playlists", { name, songIds });
  } catch (e) {
    report(e, opts, "create");
    return null;
  }
}

function deleteButton(pl, opts, after) {
  return h("button", { class: "icon more danger", title: t("playlists.delete"), "aria-label": t("playlists.deleteNamed", { name: pl.name }), onclick: async () => {
    if (!confirm(t("playlists.confirmDelete", { name: pl.name }))) return;
    try {
      await apiJSON("DELETE", `/api/music/playlist/${encodeURIComponent(pl.id)}`);
      after();
    } catch (e) { report(e, opts, "delete"); }
  } }, "✕");
}

function pile(pl) {
  const covers = (pl.covers || []).slice(0, 5);
  const n = pl.songCount || 0;
  const cases = covers.length
    ? covers.map((id, i) => {
      const c = cover(id, 150, i ? "art jc pl-case" : "art jc pl-case pl-front");
      c.style.setProperty("--i", i);
      return c;
    }).reverse()
    : [h("div", { class: "pl-case pl-empty" }, "+")];
  return h("div", { class: "pl-pile", "aria-hidden": "true" },
    ...cases,
    n ? h("span", { class: "pl-count" }, String(n)) : null);
}

function playlistRow(pl, opts, reload) {
  const own = mine(pl);
  const meta = [t("common.songCount", { n: pl.songCount || 0 }), pl.duration ? fmtTime(pl.duration) : null].filter(Boolean).join(" · ");
  const changed = pl.changed ? Date.parse(pl.changed) : NaN;
  const by = [own ? null : t("playlists.byLower", { owner: pl.owner }), isFinite(changed) ? t("playlists.updated", { date: relTime(changed / 1000) }) : null].filter(Boolean).join(" · ");
  return h("li", { class: "pl-row" },
    h("a", { class: "pl-main", href: `#/playlists/${encodeURIComponent(pl.id)}` },
      pile(pl),
      h("span", { class: "pl-text" },
        h("span", { class: "s-title" }, pl.name || t("playlists.untitled"), own && pl.public ? h("span", { class: "badge" }, t("playlists.publicBadge")) : null),
        h("span", { class: pl.comment ? "pl-desc" : "pl-desc none" }, pl.comment || t("playlists.noComment")),
        h("span", { class: "pl-meta" }, meta),
        by ? h("span", { class: "pl-by" }, by) : null)),
    h("span", { class: "pl-actions" },
      h("button", { class: "icon more", title: t("player.play"), "aria-label": t("playlists.playNamed", { name: pl.name }), onclick: async () => {
        try {
          const full = await api(`/api/music/playlist/${encodeURIComponent(pl.id)}`);
          if (full.entry?.length) player.playList(full.entry, 0);
        } catch (e) { report(e, opts, "load"); }
      } }, "▶"),
      own ? deleteButton(pl, opts, reload) : null));
}

async function renderOverview(container, opts) {
  const reload = () => renderOverview(container, opts);
  const newBtn = h("button", { class: "primary", onclick: async () => {
    const name = prompt(t("playlists.newName"));
    if (!name || !name.trim()) return;
    const pl = await createPlaylist(name.trim(), [], opts);
    if (pl) location.hash = `#/playlists/${encodeURIComponent(pl.id)}`;
  } }, t("playlists.new"));
  container.replaceChildren(h("div", { class: "pl-top" }, newBtn), h("div", { class: "muted" }, t("common.loading")));
  try {
    const all = await listPlaylists();
    const own = all.filter(mine);
    const friends = all.filter((pl) => !mine(pl));
    container.replaceChildren(
      h("div", { class: "pl-top" }, newBtn),
      h("section", {},
        h("h2", {}, t("playlists.mine", { n: own.length })),
        own.length ? h("ul", { class: "pl-list" }, own.map((pl) => playlistRow(pl, opts, reload)))
          : h("p", { class: "card muted" }, t("playlists.noneMine"))),
      h("section", {},
        h("h2", {}, t("playlists.friends", { n: friends.length })),
        friends.length ? h("ul", { class: "pl-list" }, friends.map((pl) => playlistRow(pl, opts, reload)))
          : h("p", { class: "card muted" }, t("playlists.noneFriends"))));
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(h("div", { class: "card muted" }, ERRORS[e.message] || t("playlists.loadFailed")));
  }
}

async function renderPlaylist(container, id, opts) {
  const back = h("a", { class: "back", href: "#/playlists" }, t("playlists.back"));
  container.replaceChildren(back, h("div", { class: "muted" }, t("common.loading")));
  let pl;
  try {
    pl = await api(`/api/music/playlist/${encodeURIComponent(id)}`);
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(back, h("div", { class: "card muted" }, ERRORS[e.message] || t("playlists.loadOneFailed")));
    return;
  }
  const own = mine(pl);
  const songs = pl.entry || [];
  const reload = () => renderPlaylist(container, id, opts);
  const patch = async (body) => {
    try { await apiJSON("PATCH", `/api/music/playlist/${encodeURIComponent(pl.id)}`, body); }
    catch (e) { report(e, opts, "save"); }
    reload();
  };
  const saveSongs = async (list) => {
    try { await apiJSON("PUT", `/api/music/playlist/${encodeURIComponent(pl.id)}/songs`, { songIds: list.map((s) => s.id) }); }
    catch (e) { report(e, opts, "save"); }
    reload();
  };
  const move = (i, d) => {
    const list = songs.slice();
    [list[i], list[i + d]] = [list[i + d], list[i]];
    saveSongs(list);
  };
  const total = songs.reduce((t, s) => t + (s.duration || 0), 0);
  setPageTitle(pl.name || t("playlists.untitled"), container);
  const publicBox = h("input", { type: "checkbox", checked: !!pl.public, onchange: (e) => patch({ public: e.target.checked }) });
  const rows = songs.map((_, i) => songRow(songs, i, {
    showArtist: true, showCover: true, onUnauthorized: opts.onUnauthorized,
    menu: own ? [
      { icon: "↑", label: t("playlists.moveUp"), disabled: i === 0, run: () => move(i, -1) },
      { icon: "↓", label: t("playlists.moveDown"), disabled: i === songs.length - 1, run: () => move(i, 1) },
      { icon: "−", label: t("playlists.remove"), danger: true, run: () => saveSongs(songs.filter((_, j) => j !== i)) },
    ] : null,
  }));
  container.replaceChildren(
    back,
    h("section", { class: "pl-head" },
      h("div", { class: "muted" }, [own ? t("playlists.yours") : t("playlists.by", { owner: pl.owner }), t("common.songCount", { n: songs.length }), fmtTime(total)].join(" · "), own && pl.public ? h("span", { class: "badge" }, t("playlists.publicBadge")) : null),
      h("div", { class: "actions" },
        h("button", { class: "primary", disabled: !songs.length, onclick: () => player.playList(songs, 0) }, t("favorites.playAll")),
        h("button", { disabled: !songs.length, onclick: () => shuffle(songs) }, t("player.shuffle")),
        h("button", { disabled: !songs.length, onclick: () => player.addToQueue(songs) }, t("song.addToQueue")),
        own ? h("button", { onclick: () => {
          const name = prompt(t("playlists.renamePrompt"), pl.name || "");
          if (name && name.trim() && name.trim() !== pl.name) patch({ name: name.trim() });
        } }, t("playlists.rename")) : null,
        own ? null : h("button", { onclick: async () => {
          const copy = await createPlaylist(pl.name || t("playlists.copyName"), songs.map((s) => s.id), opts);
          if (copy) location.hash = `#/playlists/${encodeURIComponent(copy.id)}`;
        } }, t("playlists.copy")),
        own ? h("button", { class: "danger", onclick: async () => {
          if (!confirm(t("playlists.confirmDelete", { name: pl.name }))) return;
          try {
            await apiJSON("DELETE", `/api/music/playlist/${encodeURIComponent(pl.id)}`);
            location.hash = "#/playlists";
          } catch (e) { report(e, opts, "delete"); }
        } }, t("playlists.delete")) : null),
      own ? h("label", { class: "pl-public" }, publicBox, h("span", {}, t("playlists.publicHint"))) : null),
    songs.length ? h("ol", { class: "songs card" }, rows)
      : h("p", { class: "card muted" }, own ? t("playlists.emptyOwn") : t("playlists.empty")));
}

export function renderPlaylists(container, parts, opts = {}) {
  const [id] = parts;
  if (id) return renderPlaylist(container, id, opts);
  return renderOverview(container, opts);
}

export async function openAddSheet(songs, opts = {}) {
  const ids = songs.map((s) => s.id);
  const list = h("ul", { class: "sheet-list" }, h("li", { class: "muted" }, t("common.loading")));
  const name = h("input", { class: "search-box", placeholder: t("playlists.newPlaceholder"), "aria-label": t("playlists.newPlaceholder") });
  const status = h("div", { class: "muted", role: "status" });
  const dlg = h("dialog", { class: "sheet card" },
    h("h2", {}, songs.length === 1 && songs[0].title ? t("playlists.addOne", { title: songs[0].title }) : t("playlists.addMany", { n: songs.length })),
    list,
    h("form", { class: "sheet-new", onsubmit: async (e) => {
      e.preventDefault();
      if (!name.value.trim()) return name.focus();
      const pl = await createPlaylist(name.value.trim(), ids, opts);
      if (pl) { dlg.close(); toast(t("playlists.added", { name: pl.name })); }
    } }, name, h("button", { class: "primary", type: "submit" }, t("playlists.create"))),
    status,
    h("button", { class: "ghost", type: "button", onclick: () => dlg.close() }, t("common.cancel")));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  try {
    const own = (await listPlaylists()).filter(mine);
    list.replaceChildren(...(own.length ? own.map((pl) => h("li", {}, h("button", { class: "sheet-item", onclick: async () => {
      try {
        await apiJSON("POST", `/api/music/playlist/${encodeURIComponent(pl.id)}/songs`, { songIds: ids });
        dlg.close();
        toast(t("playlists.added", { name: pl.name }));
      } catch (e) { report(e, opts, "add"); }
    } }, h("span", {}, pl.name), h("span", { class: "muted" }, t("common.songCount", { n: pl.songCount || 0 }))))) : [h("li", { class: "muted" }, t("playlists.noneBelow"))]));
  } catch (e) {
    if (e instanceof Unauthorized) { dlg.close(); return opts.onUnauthorized?.(); }
    list.replaceChildren(h("li", { class: "muted" }, t("playlists.loadYoursFailed")));
  }
}

export function toast(text) {
  const el = h("div", { class: "toast", role: "status" }, text);
  document.body.append(el);
  setTimeout(() => el.remove(), 2200);
}

export const addToPlaylistButton = (songs, opts = {}, cls = "icon more") =>
  h("button", { class: cls, title: t("playlists.addTo"), "aria-label": t("playlists.addTo"), onclick: () => openAddSheet(songs, opts) }, cls.includes("icon") ? "☰" : t("playlists.addTo"));
