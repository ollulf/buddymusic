import { t } from "./i18n.js";
import { h, apiJSON, Unauthorized } from "./dom.js";
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
  const [genreF, genre] = field(t("edit.genre"), al.genre, { maxlength: 300 });
  const rows = songs.map((s, i) => {
    const [titleF, title] = field(t("edit.trackTitle", { n: s.track || i + 1 }), s.title, { required: true, maxlength: 300 });
    const [tArtistF, tArtist] = field(t("edit.trackArtist"), trackArtist(s), { required: true, maxlength: 300 });
    return { s, title, tArtist, el: h("li", { class: "edit-track" }, titleF, tArtistF) };
  });
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
      toast(res.scan === "started" ? t("edit.saved") : t("edit.savedNoScan"));
      dlg.close();
      opts.onSaved?.();
    } catch (err) {
      if (err instanceof Unauthorized) { dlg.close(); opts.onUnauthorized?.(); return; }
      status.textContent = t("edit.failed", { error: ERRORS[err.message] ? t(ERRORS[err.message]) : err.message });
      save.disabled = false;
    }
  } },
    h("div", { class: "edit-grid" }, nameF, artistF, yearF, genreF),
    h("p", { class: "muted edit-hint" }, t("edit.artistHint")),
    h("h3", {}, t("edit.tracks")),
    h("ol", { class: "edit-tracks" }, rows.map((r) => r.el)),
    status,
    h("div", { class: "actions" }, save, cancel));
  const closeBtn = h("button", { class: "ghost news-close", type: "button", "aria-label": t("news.close"), title: t("news.close"), onclick: () => dlg.close() }, "✕");
  const dlg = h("dialog", { class: "sheet card album-edit-dialog", "aria-labelledby": "album-edit-title" },
    h("div", { class: "news-head" }, h("h2", { id: "album-edit-title" }, t("edit.title")), closeBtn),
    form);
  dlg.addEventListener("close", () => { dlg.remove(); opts.returnTo?.focus(); });
  document.body.append(dlg);
  dlg.showModal();
  name.focus();
}
