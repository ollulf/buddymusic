import { t } from "./i18n.js";
import { LABELS, fieldList, fromMediaTags, fromFileName, mostCommon, present, trackNumber } from "./tags.js";

const ALBUM_FIELDS = ["artist", "album", "year"];
const ROW_FIELDS = ["title", "track"];
const MAX_ARTISTS = 8;
const ERRORS = {
  "unsupported-type": t("upload.err.type"),
  "too-large": t("upload.err.tooLarge"),
  "cover-too-large": t("upload.err.coverTooLarge"),
  "unsupported-cover-type": t("upload.err.coverType"),
  "exists": t("upload.err.exists"),
  "probe-failed": t("upload.err.probe"),
  "convert-failed": t("upload.err.convert"),
  "unauthorized": t("upload.err.unauthorized"),
};

let state = { files: [], cover: null, edited: new Set(), busy: false, nextId: 1, albumArtists: [] };

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

let ui = {};

function readTags(file) {
  return new Promise((resolve) => {
    if (!window.jsmediatags || /\.(ogg|opus|wma)$/i.test(file.name)) return resolve({ tags: null, picture: null });
    window.jsmediatags.read(file, {
      onSuccess: (r) => resolve({ tags: fromMediaTags(r.tags), picture: r.tags?.picture || null }),
      onError: () => resolve({ tags: null, picture: null }),
    });
  });
}

function pending() {
  return state.files.filter((e) => e.status !== "done");
}

function album() {
  const out = {};
  for (const k of ALBUM_FIELDS) out[k] = String(ui.album[k].value || "").trim();
  return out;
}

function albumKey(a) {
  const artist = (a.artist || "").toLowerCase();
  const name = (a.album || "").toLowerCase();
  return artist && name ? artist + "\u0000" + name : "";
}

const folderCovers = new Map();

function folderCover(a) {
  const key = albumKey(a);
  if (!key) return false;
  if (!folderCovers.has(key)) {
    folderCovers.set(key, "pending");
    const q = new URLSearchParams({ artist: a.artist, album: a.album });
    fetch("/api/covers?" + q).then((r) => r.ok ? r.json() : { exists: false }).then((b) => {
      folderCovers.set(key, !!b.exists);
      refresh();
    }).catch(() => folderCovers.delete(key));
  }
  return folderCovers.get(key) === true;
}

function batchCover() {
  return state.files.some((e) => e.status !== "done" && e.fileTags?.cover);
}

function coverState(entry, a) {
  if (state.cover) return "picked";
  if (entry.fileTags?.cover) return "own";
  if (entry.serverMissing.includes("cover")) return folderCover(a) ? "folder" : "";
  if (batchCover()) return "album";
  if (folderCover(a)) return "folder";
  return entry.fileTags ? "" : "unknown";
}

function missingOf(entry) {
  const a = album();
  const miss = [];
  if (!present("title", entry.title)) miss.push("title");
  if (!present("artist", a.artist)) miss.push("artist");
  if (!present("album", a.album)) miss.push("album");
  if (!present("track", entry.track)) miss.push("track");
  if (!coverState(entry, a)) miss.push("cover");
  return miss;
}

function prefillAlbum() {
  const readable = pending().filter((e) => e.fileTags);
  for (const k of ALBUM_FIELDS) {
    if (state.edited.has(k)) continue;
    ui.album[k].value = mostCommon(readable.map((e) => e.fileTags[k]));
  }
}

function resetAlbum() {
  state.edited = new Set();
  state.cover = null;
  state.albumArtists.length = 0;
  ui.renderAlbumArtists();
  for (const k of ALBUM_FIELDS) ui.album[k].value = "";
  ui.coverInput.value = "";
}

function addFiles(list) {
  if (!list.length) return;
  if (!pending().length) resetAlbum();
  for (const file of list) {
    const guess = fromFileName(file.name);
    const entry = { id: state.nextId++, file, fileTags: undefined, picture: null, title: guess.title, track: guess.track, disc: guess.disc,
      artists: [], serverMissing: [], status: "reading" };
    state.files.push(entry);
    entry.el = buildRow(entry);
    ui.list.append(entry.el.row);
    readTags(file).then(({ tags, picture }) => {
      entry.fileTags = tags;
      entry.picture = picture?.data?.length ? picture : null;
      entry.status = "pending";
      if (tags) {
        if (present("title", tags.title)) entry.title = tags.title;
        if (present("track", tags.track)) entry.track = String(trackNumber(tags.track));
        if (present("track", tags.disc)) entry.disc = String(trackNumber(tags.disc));
      }
      buildFields(entry);
      prefillAlbum();
      sortRows();
      refresh();
    });
  }
  refresh();
}

function buildRow(entry) {
  const chips = h("div", { class: "chips" });
  const fields = h("div", { class: "song" });
  const bar = h("i");
  const msg = h("div", { class: "msg" });
  const state_ = h("span", { class: "state" }, t("upload.reading"));
  const remove = h("button", { class: "icon", title: t("upload.remove"), onclick: () => removeEntry(entry) }, "×");
  const row = h("div", { class: "row" },
    h("div", { class: "row-head" }, h("span", { class: "fn" }, entry.file.name), state_, remove),
    fields, chips, h("div", { class: "progress" }, bar), msg);
  return { row, chips, fields, bar, msg, state: state_, remove, inputs: {} };
}

function rowInput(entry, key) {
  const numeric = key !== "title";
  const el = h("input", {
    type: numeric ? "number" : "text",
    min: numeric ? "1" : null,
    inputmode: numeric ? "numeric" : null,
    value: entry[key] || "",
    placeholder: t("upload.required"),
    "aria-label": t("upload.fieldFor", { field: LABELS[key], file: entry.file.name }),
    oninput: (e) => {
      entry[key] = e.target.value;
      entry.serverMissing = entry.serverMissing.filter((k) => k !== key);
      refresh();
    },
    onchange: () => { if (key !== "title") { sortRows(); refresh(); } },
  });
  entry.el.inputs[key] = el;
  return h("label", { class: "field " + key }, h("span", {}, LABELS[key]), el);
}

function buildFields(entry) {
  const keys = present("track", entry.disc) ? [...ROW_FIELDS, "disc"] : ROW_FIELDS;
  const extra = artistEditor(entry.artists, () => MAX_ARTISTS - 1 - state.albumArtists.length, t("upload.songArtists"),
    (el) => { entry.el.artistInputs = el; });
  entry.el.fields.replaceChildren(...keys.map((k) => rowInput(entry, k)), extra.box);
  entry.el.renderArtists = extra.render;
}

function artistEditor(list, room, label, track, changed = () => {}) {
  const items = h("div", { class: "artist-inputs" });
  const add = h("button", { type: "button", class: "ghost add-artist", onclick: () => {
    list.push("");
    render();
    changed();
    items.lastElementChild?.querySelector("input")?.focus();
  } }, t("upload.addArtist"));
  const box = h("div", { class: "field artists" }, h("span", {}, label), items, add);
  function render() {
    items.replaceChildren(...list.map((v, i) => h("div", { class: "artist-item" },
      h("input", { type: "text", value: v, maxlength: "300", placeholder: t("upload.optional"),
        "aria-label": t("upload.extraArtist", { n: i + 2 }),
        oninput: (e) => { list[i] = e.target.value; } }),
      h("button", { type: "button", class: "icon", title: t("upload.remove"),
        onclick: () => { list.splice(i, 1); render(); changed(); } }, "×"))));
    add.hidden = list.length >= room();
    box.classList.toggle("empty", list.length === 0);
    if (track) track([...items.querySelectorAll("input, button"), add]);
  }
  render();
  return { box, render };
}

function artistNames(list) {
  const out = [];
  const seen = new Set();
  for (const raw of list) {
    const v = String(raw ?? "").trim();
    if (!v || seen.has(v.toLowerCase())) continue;
    seen.add(v.toLowerCase());
    out.push(v);
  }
  return out.slice(0, MAX_ARTISTS);
}

function sortKey(e) {
  return [trackNumber(e.disc) || 1, trackNumber(e.track) || 1e6, e.file.name];
}

function sortRows() {
  const sorted = [...state.files].sort((x, y) => {
    const a = sortKey(x), b = sortKey(y);
    return a[0] - b[0] || a[1] - b[1] || a[2].localeCompare(b[2]);
  });
  const order = state.files.map((e) => e.id).join();
  state.files = sorted;
  if (sorted.map((e) => e.id).join() !== order) ui.list.append(...sorted.map((e) => e.el.row));
}

function chip(text, cls) {
  return h("span", { class: "chip " + cls }, text);
}

const COVER_CHIPS = {
  picked: [t("upload.chip.picked"), "ok"],
  own: [t("upload.chip.own"), "ok"],
  album: [t("upload.chip.album"), "ok"],
  folder: [t("upload.chip.folder"), "ok"],
  unknown: [t("upload.chip.unknown"), "warn"],
};

function renderChips(entry, miss) {
  const el = entry.el.chips;
  if (entry.status === "reading" || entry.status === "done") return el.replaceChildren();
  const out = miss.map((k) => chip(t("upload.missing", { field: LABELS[k] }), "bad"));
  const c = COVER_CHIPS[coverState(entry, album())];
  if (c) out.push(chip(c[0], c[1]));
  if (entry.fileTags === null) out.push(chip(t("upload.unreadable"), "warn"));
  el.replaceChildren(...out);
}

function removeEntry(entry) {
  if (entry.status === "uploading") return;
  state.files = state.files.filter((e) => e !== entry);
  entry.el.row.remove();
  prefillAlbum();
  refresh();
}

let previewUrl = "";

function renderCover(a) {
  const withPic = state.cover ? null : pending().find((e) => e.picture);
  const src = state.cover || withPic;
  if (src !== ui.coverSrc) {
    ui.coverSrc = src;
    if (previewUrl) URL.revokeObjectURL(previewUrl);
    let blob = state.cover;
    if (withPic) {
      const fmt = String(withPic.picture.format || "image/jpeg");
      blob = new Blob([new Uint8Array(withPic.picture.data)], { type: fmt.startsWith("image/") ? fmt : "image/" + fmt });
    }
    previewUrl = blob ? URL.createObjectURL(blob) : "";
    ui.preview.replaceChildren(previewUrl ? h("img", { src: previewUrl, alt: t("upload.coverPreview") }) : h("span", {}, t("upload.noCover")));
  }
  if (state.cover) ui.coverNote.textContent = t("upload.note.picked");
  else if (withPic) ui.coverNote.textContent = t("upload.note.embedded", { file: withPic.file.name });
  else if (folderCover(a)) ui.coverNote.textContent = t("upload.note.folder");
  else ui.coverNote.textContent = t("upload.note.pick");
}

function refresh() {
  let ready = 0, blocked = 0, done = 0;
  const open = pending();
  ui.albumBox.hidden = open.length === 0;
  const a = album();
  const needAlbum = new Set();
  for (const e of state.files) {
    e.el.row.dataset.status = e.status;
    if (e.status === "done") { done++; e.el.state.textContent = t("upload.inLibrary"); renderChips(e, []); continue; }
    if (e.status === "reading") { blocked++; renderChips(e, []); continue; }
    const miss = missingOf(e);
    renderChips(e, miss);
    for (const k of miss) if (ALBUM_FIELDS.includes(k)) needAlbum.add(k);
    for (const k of ROW_FIELDS) e.el.inputs[k]?.classList.toggle("need", miss.includes(k));
    if (e.status === "uploading") { e.el.state.textContent = t("upload.uploadingRow"); continue; }
    if (miss.length) { blocked++; e.el.state.textContent = t("upload.needs", { fields: fieldList(miss) }); }
    else { ready++; e.el.state.textContent = e.status === "error" ? t("upload.retry") : t("upload.ready"); }
    e.el.row.classList.toggle("blocked", miss.length > 0);
  }
  for (const k of ALBUM_FIELDS) ui.album[k].classList.toggle("need", needAlbum.has(k));
  if (open.length) renderCover(a);
  const coverMissing = open.some((e) => e.status !== "reading" && missingOf(e).includes("cover"));
  ui.coverBox.classList.toggle("need", coverMissing);
  const total = ready + blocked;
  ui.upload.disabled = state.busy || ready === 0 || blocked > 0;
  ui.upload.textContent = state.busy ? t("upload.uploading") : total ? t("upload.uploadN", { n: total }) : t("upload.upload");
  const parts = [];
  if (total) parts.push(t("upload.readyOf", { ready, total }));
  if (blocked) parts.push(t("upload.needTags", { n: blocked }));
  if (done) parts.push(t("upload.uploaded", { n: done }));
  ui.summary.textContent = parts.length ? parts.join(" · ") : t("upload.noFiles");
  ui.clear.hidden = done === 0;
}

function tagPayload(entry) {
  const a = album();
  const out = {};
  for (const k of ROW_FIELDS) {
    const v = String(entry[k] ?? "").trim();
    if (v) out[k] = v;
  }
  const disc = String(entry.disc ?? "").trim();
  if (disc) out.disc = disc;
  for (const k of ALBUM_FIELDS) if (a[k]) out[k] = a[k];
  if (a.artist && (state.albumArtists.length || entry.artists.length)) {
    out.albumArtists = artistNames([a.artist, ...state.albumArtists]);
    out.artists = artistNames([...out.albumArtists, ...entry.artists]);
  }
  return out;
}

function send(entry) {
  return new Promise((resolve) => {
    const fd = new FormData();
    fd.append("tags", JSON.stringify(tagPayload(entry)));
    if (state.cover) fd.append("cover", state.cover, state.cover.name);
    fd.append("file", entry.file, entry.file.name);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/uploads");
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) entry.el.bar.style.width = (100 * e.loaded / e.total) + "%"; };
    xhr.onload = () => {
      let body = {};
      try { body = JSON.parse(xhr.responseText); } catch {}
      resolve({ status: xhr.status, body });
    };
    xhr.onerror = () => resolve({ status: 0, body: { error: "network" } });
    xhr.send(fd);
  });
}

function setRowEnabled(entry, on) {
  for (const el of Object.values(entry.el.inputs)) el.disabled = !on;
  for (const el of entry.el.artistInputs || []) el.disabled = !on;
}

function setAlbumEnabled(on) {
  for (const k of ALBUM_FIELDS) ui.album[k].disabled = !on;
  for (const el of ui.albumArtistInputs || []) el.disabled = !on;
  ui.coverInput.disabled = !on;
}

async function uploadAll() {
  if (state.busy) return;
  state.busy = true;
  setAlbumEnabled(false);
  refresh();
  const order = [...state.files].sort((x, y) => (y.fileTags?.cover ? 1 : 0) - (x.fileTags?.cover ? 1 : 0));
  for (const e of order) {
    if (e.status === "done" || e.status === "reading" || missingOf(e).length) continue;
    e.status = "uploading";
    e.el.msg.textContent = "";
    e.el.bar.style.width = "0";
    setRowEnabled(e, false);
    refresh();
    const { status, body } = await send(e);
    setRowEnabled(e, true);
    if (status === 201) {
      e.status = "done";
      e.el.msg.textContent = t("upload.added", { path: body.libraryPath || body.filename });
      e.el.fields.replaceChildren();
      e.el.inputs = {};
      e.el.artistInputs = [];
      e.el.renderArtists = null;
      const key = albumKey(album());
      if (key) folderCovers.set(key, true);
    } else if (status === 422 && body.missing) {
      e.status = "error";
      e.serverMissing = body.missing;
      e.el.msg.textContent = t("upload.serverMissing", { fields: fieldList(body.missing) });
      if (body.missing.includes("cover")) folderCovers.delete(albumKey(album()));
    } else if (status === 401) {
      e.status = "error";
      e.el.msg.textContent = ERRORS.unauthorized;
      unlockAlbum();
      refresh();
      onUnauthorized();
      return;
    } else {
      e.status = "error";
      e.el.msg.textContent = ERRORS[body.error] || t("upload.failed", { error: body.error || status });
    }
    refresh();
  }
  unlockAlbum();
  refresh();
}

function unlockAlbum() {
  state.busy = false;
  setAlbumEnabled(true);
}

let onUnauthorized = () => {};

function albumInput(key) {
  const el = h("input", {
    type: "text",
    inputmode: key === "year" ? "numeric" : null,
    placeholder: key === "artist" || key === "album" ? t("upload.required") : t("upload.optional"),
    oninput: () => { state.edited.add(key); refresh(); },
  });
  ui.album[key] = el;
  const field = h("label", { class: "field" }, h("span", {}, LABELS[key]), el);
  if (key !== "artist") return field;
  const extra = artistEditor(state.albumArtists, () => MAX_ARTISTS - 1, t("upload.moreArtists"), (els) => { ui.albumArtistInputs = els; },
    () => { for (const e of state.files) e.el.renderArtists?.(); });
  ui.renderAlbumArtists = () => {
    extra.render();
    for (const e of state.files) e.el.renderArtists?.();
  };
  return [field, extra.box];
}

export function renderUpload(container, opts = {}) {
  state = { files: [], cover: null, edited: new Set(), busy: false, nextId: 1, albumArtists: [] };
  ui = { album: {}, renderAlbumArtists: () => {} };
  if (opts.onUnauthorized) onUnauthorized = opts.onUnauthorized;
  const picker = h("input", { type: "file", multiple: true, accept: "audio/*,.flac,.mp3,.m4a,.ogg,.opus,.wma", hidden: true,
    onchange: (e) => { addFiles(e.target.files); e.target.value = ""; } });
  const drop = h("div", { class: "drop", tabindex: "0", role: "button",
    onclick: () => picker.click(),
    onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); picker.click(); } },
    ondragover: (e) => { e.preventDefault(); drop.classList.add("over"); },
    ondragleave: () => drop.classList.remove("over"),
    ondrop: (e) => { e.preventDefault(); drop.classList.remove("over"); addFiles(e.dataTransfer.files); },
  }, h("strong", {}, t("upload.drop")), h("span", {}, t("upload.dropHint")), picker);

  ui.coverInput = h("input", { type: "file", accept: "image/jpeg,image/png", onchange: (e) => {
    state.cover = e.target.files[0] || null;
    refresh();
  } });
  ui.preview = h("div", { class: "preview" }, h("span", {}, t("upload.noCover")));
  ui.coverNote = h("span", { class: "muted" });
  ui.coverBox = h("div", { class: "cover" }, ui.preview,
    h("label", { class: "field" }, h("span", {}, LABELS.cover), ui.coverInput, ui.coverNote));
  ui.albumBox = h("section", { class: "card album", hidden: true },
    h("h2", {}, t("tag.album")),
    h("p", { class: "muted" }, t("upload.albumHint")),
    h("div", { class: "album-grid" }, ui.coverBox, h("div", { class: "fields" }, ...ALBUM_FIELDS.map(albumInput))));

  ui.list = h("div", { class: "list", "data-empty": t("upload.emptyList") });
  ui.summary = h("span", { class: "muted" });
  ui.upload = h("button", { class: "primary", onclick: uploadAll, disabled: true }, t("upload.upload"));
  ui.clear = h("button", { class: "ghost", hidden: true, onclick: () => {
    for (const e of state.files.filter((x) => x.status === "done")) e.el.row.remove();
    state.files = state.files.filter((x) => x.status !== "done");
    refresh();
  } }, t("upload.clear"));

  container.replaceChildren(
    drop, ui.albumBox,
    h("section", { class: "card files" }, h("h2", {}, t("search.songs")), ui.list,
      h("div", { class: "foot" }, ui.summary, h("div", { class: "actions" }, ui.clear, ui.upload))));
  refresh();
}
