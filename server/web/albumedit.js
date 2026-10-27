import { t } from "./i18n.js";
import { h, api, apiJSON, Unauthorized } from "./dom.js";
import { toast } from "./playlists.js";

const ERRORS = {
  "admin-only": "edit.adminOnly",
  "bad-request": "edit.badInput",
  "unknown-track": "edit.badInput",
  "outside-library": "delete.outside",
  "file-not-found": "delete.fileNotFound",
  "not-found": "delete.notFound",
  "unsupported-format": "edit.unsupported",
  "write-failed": "edit.writeFailed",
};

const BACK_ERRORS = {
  "too-large": "edit.backTooLarge",
  "unsupported-cover-type": "edit.backType",
};

const MAX_BACK = 10 << 20;

const trackArtist = (s) => (Array.isArray(s.artists) && s.artists.length ? s.artists.map((a) => a.name).join("; ") : s.artist || "");
const albumArtist = (al) => (Array.isArray(al.artists) && al.artists.length ? al.artists.map((a) => a.name).join("; ") : al.artist || "");

function field(label, value, attrs = {}) {
  const input = h("input", { type: "text", value: value ?? "", ...attrs });
  return [h("label", { class: "field" }, h("span", {}, label), input), input];
}

export function openAlbumEdit(al, songs, opts = {}) {
  const [nameF, name] = field(t("edit.album"), al.name, { required: true, maxlength: 300 });
  const [artistF, artist] = field(t("edit.albumArtist"), albumArtist(al), { required: true, maxlength: 300, placeholder: "Artist; Other Artist" });
  const [yearF, year] = field(t("edit.year"), al.year ? String(al.year) : "", { inputmode: "numeric", pattern: "[0-9]{4}", maxlength: 4 });
  const [genreF, genre] = field(t("edit.genre"), al.genre, { maxlength: 300, list: "edit-genre-list", autocomplete: "off" });
  const genreOptions = h("datalist", { id: "edit-genre-list" });
  genreF.append(genreOptions);
  api("/api/music/genres").then((d) => {
    const names = [...new Set([...(d.list || []), ...(d.genres || []).map((g) => g.name)])];
    genreOptions.replaceChildren(...names.map((n) => h("option", { value: n })));
  }).catch(() => {});
  const vinyl = h("input", { type: "checkbox", checked: !!al.vinyl });
  const vinylF = h("label", { class: "field edit-check" }, vinyl, h("span", {}, t("edit.vinyl")));
  const rows = songs.map((s, i) => {
    const [titleF, title] = field(t("edit.trackTitle", { n: s.track || i + 1 }), s.title, { required: true, maxlength: 300 });
    const [tArtistF, tArtist] = field(t("edit.trackArtist"), trackArtist(s), { required: true, maxlength: 300 });
    return { s, title, tArtist, el: h("li", { class: "edit-track" }, titleF, tArtistF) };
  });
  let hasBack = !!al.hasBack;
  let backChanged = false;
  let saved = false;
  const backStatus = h("p", { class: "muted edit-hint", role: "status" });
  const backImg = h("img", { alt: t("edit.backAlt"), decoding: "async" });
  const backBox = h("div", { class: "edit-back-preview" }, backImg);
  const backFile = h("input", { type: "file", accept: "image/jpeg,image/png", hidden: true });
  const backPick = h("button", { type: "button", onclick: () => backFile.click() });
  const backRemove = h("button", { type: "button" }, t("edit.backRemove"));
  const showBack = (v) => {
    backBox.hidden = !hasBack;
    backRemove.hidden = !hasBack;
    backPick.textContent = t(hasBack ? "edit.backReplace" : "edit.backAdd");
    if (hasBack) backImg.src = `/api/music/back/${encodeURIComponent(al.id)}?v=${v}`;
  };
  const backCall = async (method, body, contentType) => {
    backPick.disabled = backRemove.disabled = true;
    backStatus.textContent = t("edit.backSaving");
    try {
      const res = await fetch(`/api/admin/album/${encodeURIComponent(al.id)}/back`, { method, body, headers: contentType ? { "Content-Type": contentType } : {} });
      if (res.status === 401) { dlg.close(); opts.onUnauthorized?.(); return; }
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `http ${res.status}`);
      hasBack = method === "PUT";
      backChanged = true;
      opts.onBackChanged?.();
      backStatus.textContent = t(hasBack ? "edit.backSaved" : "edit.backRemoved");
      showBack(Date.now());
    } catch (err) {
      backStatus.textContent = t("edit.failed", { error: BACK_ERRORS[err.message] ? t(BACK_ERRORS[err.message]) : ERRORS[err.message] ? t(ERRORS[err.message]) : err.message });
    } finally {
      backPick.disabled = backRemove.disabled = false;
    }
  };
  backFile.addEventListener("change", () => {
    const f = backFile.files[0];
    backFile.value = "";
    if (!f) return;
    if (f.size > MAX_BACK) { backStatus.textContent = t("edit.failed", { error: t("edit.backTooLarge") }); return; }
    backCall("PUT", f, f.type || "application/octet-stream");
  });
  backRemove.addEventListener("click", () => backCall("DELETE"));
  showBack(al.backV || 0);
  const backF = h("div", { class: "edit-back" }, h("h3", {}, t("edit.back")), backBox, h("div", { class: "actions" }, backPick, backRemove, backFile), backStatus);
  const status = h("p", { class: "muted", role: "status" });
  const save = h("button", { class: "primary", type: "submit" }, t("edit.save"));
  const cancel = h("button", { type: "button", onclick: () => dlg.close() }, t("edit.cancel"));
  const changed = (input, orig) => input.value.trim() !== String(orig ?? "").trim();
  const form = h("form", { class: "album-edit", onsubmit: async (e) => {
    e.preventDefault();
    const body = {};
    if (changed(name, al.name)) body.album = name.value;
    if (changed(artist, albumArtist(al))) body.albumArtist = artist.value;
    if (changed(year, al.year ? String(al.year) : "")) body.year = year.value.trim();
    if (changed(genre, al.genre)) body.genre = genre.value;
    if (vinyl.checked !== !!al.vinyl) body.vinyl = vinyl.checked;
    const tracks = rows.map(({ s, title, tArtist }) => {
      const tr = { id: s.id };
      if (changed(title, s.title)) tr.title = title.value;
      if (changed(tArtist, trackArtist(s))) tr.artist = tArtist.value;
      return tr;
    }).filter((tr) => Object.keys(tr).length > 1);
    if (tracks.length) body.tracks = tracks;
    if (!Object.keys(body).length) return dlg.close();
    save.disabled = true;
    status.textContent = t("edit.saving");
    try {
      const res = await apiJSON("PATCH", `/api/admin/album/${encodeURIComponent(al.id)}`, body);
      toast(res.scan === "started" || res.scan === "skipped" ? t("edit.saved") : t("edit.savedNoScan"));
      saved = true;
      dlg.close();
      opts.onSaved?.(body);
    } catch (err) {
      if (err instanceof Unauthorized) { dlg.close(); opts.onUnauthorized?.(); return; }
      status.textContent = t("edit.failed", { error: ERRORS[err.message] ? t(ERRORS[err.message]) : err.message });
      save.disabled = false;
    }
  } },
    h("div", { class: "edit-grid" }, nameF, artistF, yearF, genreF),
    h("p", { class: "muted edit-hint" }, t("edit.artistHint")),
    vinylF,
    backF,
    h("h3", {}, t("edit.tracks")),
    h("ol", { class: "edit-tracks" }, rows.map((r) => r.el)),
    status,
    h("div", { class: "actions" }, save, cancel));
  const closeBtn = h("button", { class: "ghost news-close", type: "button", "aria-label": t("news.close"), title: t("news.close"), onclick: () => dlg.close() }, "✕");
  const dlg = h("dialog", { class: "sheet card album-edit-dialog", "aria-labelledby": "album-edit-title" },
    h("div", { class: "news-head" }, h("h2", { id: "album-edit-title" }, t("edit.title")), closeBtn),
    form);
  dlg.addEventListener("close", () => { dlg.remove(); if (backChanged && !saved) opts.onSaved?.({}); opts.returnTo?.focus(); });
  document.body.append(dlg);
  dlg.showModal();
  name.focus();
}
