import { REQUIRED, LABELS, fromMediaTags, merge, missingFields, present } from "./tags.js";

const SHARED_FIELDS = ["artist", "album", "album_artist", "year"];
const ERRORS = {
  "unsupported-type": "Not a supported audio file (FLAC, MP3, M4A, OGG, Opus)",
  "too-large": "File is too big",
  "cover-too-large": "Cover image is too big (10 MB max)",
  "unsupported-cover-type": "Cover must be a JPEG or PNG",
  "exists": "This track is already in your library",
  "probe-failed": "The server could not read this file",
  "unauthorized": "Logged out, please log in again",
};

let state = { files: [], shared: { cover: null }, busy: false, nextId: 1 };

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
    if (!window.jsmediatags || /\.(ogg|opus)$/i.test(file.name)) return resolve(null);
    window.jsmediatags.read(file, {
      onSuccess: (r) => resolve(fromMediaTags(r.tags)),
      onError: () => resolve(null),
    });
  });
}

function effective(entry) {
  const base = entry.fileTags || {};
  return merge(base, entry.typed, entry.fileTags ? sharedFor(entry) : {});
}

function sharedFor(entry) {
  const s = {};
  for (const k of SHARED_FIELDS) s[k] = ui.shared?.[k]?.value || "";
  s.cover = state.shared.cover;
  return entry.fileTags?.cover ? { ...s, cover: null } : s;
}

function albumKey(t) {
  const artist = String(t.album_artist || t.artist || "").trim().toLowerCase();
  const album = String(t.album || "").trim().toLowerCase();
  return artist && album ? artist + "\u0000" + album : "";
}

const folderCovers = new Map();

function coverElsewhere(entry, eff) {
  const key = albumKey(eff);
  if (!key) return false;
  if (folderCovers.get(key) === true) return true;
  return state.files.some((o) => o !== entry && o.fileTags?.cover && albumKey(effective(o)) === key);
}

function checkFolderCover(eff) {
  const key = albumKey(eff);
  if (!key || folderCovers.has(key)) return;
  folderCovers.set(key, "pending");
  const q = new URLSearchParams({ artist: eff.artist || "", album: eff.album || "", album_artist: eff.album_artist || "" });
  fetch("/api/covers?" + q).then((r) => r.ok ? r.json() : { exists: false }).then((b) => {
    folderCovers.set(key, !!b.exists);
    if (b.exists) refresh();
  }).catch(() => folderCovers.delete(key));
}

function missingOf(entry) {
  if (!entry.fileTags) {
    return entry.serverMissing.filter((k) => !present(k, k === "cover" ? entry.typed.cover || state.shared.cover : entry.typed[k]));
  }
  const eff = effective(entry);
  const miss = missingFields(eff);
  if (!miss.includes("cover")) return miss;
  checkFolderCover(eff);
  return coverElsewhere(entry, eff) ? miss.filter((k) => k !== "cover") : miss;
}

function addFiles(list) {
  for (const file of list) {
    const entry = { id: state.nextId++, file, fileTags: undefined, typed: {}, serverMissing: [], status: "reading", message: "" };
    state.files.push(entry);
    entry.el = buildRow(entry);
    ui.list.append(entry.el.row);
    readTags(file).then((tags) => {
      entry.fileTags = tags;
      entry.status = "pending";
      buildFields(entry);
      refresh();
    });
  }
  refresh();
}

function buildRow(entry) {
  const chips = h("div", { class: "chips" });
  const fields = h("div", { class: "fields" });
  const bar = h("i");
  const msg = h("div", { class: "msg" });
  const state_ = h("span", { class: "state" }, "reading tags…");
  const remove = h("button", { class: "icon", title: "Remove", onclick: () => removeEntry(entry) }, "×");
  const row = h("div", { class: "row" },
    h("div", { class: "row-head" }, h("span", { class: "fn" }, entry.file.name), state_, remove),
    chips, fields, h("div", { class: "progress" }, bar), msg);
  return { row, chips, fields, bar, msg, state: state_, remove };
}

function input(entry, key) {
  if (key === "cover") {
    return h("label", { class: "field" }, h("span", {}, LABELS.cover),
      h("input", { type: "file", accept: "image/jpeg,image/png", onchange: (e) => { entry.typed.cover = e.target.files[0] || null; refresh(); } }));
  }
  return h("label", { class: "field" }, h("span", {}, LABELS[key]),
    h("input", {
      type: key === "track" || key === "year" ? "number" : "text",
      min: key === "track" ? "1" : null,
      value: entry.typed[key] || "",
      placeholder: entry.fileTags ? "required" : "only if the file lacks it",
      oninput: (e) => { entry.typed[key] = e.target.value; refresh(); },
    }));
}

function buildFields(entry) {
  const keys = new Set(entry.serverMissing);
  if (entry.fileTags) for (const k of missingFields(entry.fileTags)) keys.add(k);
  entry.el.fields.replaceChildren(...REQUIRED.filter((k) => keys.has(k)).map((k) => input(entry, k)));
}

function chip(text, cls) {
  return h("span", { class: "chip " + cls }, text);
}

function renderChips(entry) {
  const el = entry.el.chips;
  if (entry.status === "reading") return el.replaceChildren();
  if (!entry.fileTags && !entry.serverMissing.length) {
    return el.replaceChildren(chip("tags checked on upload", "warn"));
  }
  const eff = entry.fileTags ? effective(entry) : {};
  const miss = new Set(missingOf(entry));
  const out = [];
  for (const k of REQUIRED) {
    if (miss.has(k)) out.push(chip(LABELS[k] + " missing", "bad"));
    else if (entry.fileTags) out.push(chip(k === "track" ? "track " + eff.track : k === "cover" ? (eff.cover ? "cover" : "album cover") : LABELS[k].toLowerCase(), "ok"));
  }
  if (entry.fileTags) {
    out.push(chip(eff.album_artist ? "album artist" : "no album artist", eff.album_artist ? "ok" : "warn"));
    out.push(chip(eff.year ? eff.year : "no year", eff.year ? "ok" : "warn"));
  }
  el.replaceChildren(...out);
}

function removeEntry(entry) {
  if (entry.status === "uploading") return;
  state.files = state.files.filter((e) => e !== entry);
  entry.el.row.remove();
  refresh();
}

function refresh() {
  let ready = 0, blocked = 0, done = 0;
  const needShared = state.files.some((e) => e.status !== "done" && e.fileTags &&
    ["artist", "album", "cover"].some((k) => !present(k, e.fileTags[k])));
  ui.sharedBox.hidden = !needShared;
  for (const e of state.files) {
    renderChips(e);
    const miss = e.status === "reading" ? [] : missingOf(e);
    e.el.row.dataset.status = e.status;
    if (e.status === "done") { done++; e.el.state.textContent = "in library"; continue; }
    if (e.status === "uploading") { e.el.state.textContent = "uploading…"; continue; }
    if (e.status === "reading") { blocked++; continue; }
    if (miss.length) { blocked++; e.el.state.textContent = "needs " + miss.map((k) => LABELS[k].toLowerCase()).join(", "); }
    else { ready++; e.el.state.textContent = e.status === "error" ? "retry" : "ready"; }
    e.el.row.classList.toggle("blocked", miss.length > 0);
  }
  const pending = ready + blocked;
  ui.upload.disabled = state.busy || ready === 0 || blocked > 0;
  ui.upload.textContent = state.busy ? "Uploading…" : pending ? `Upload ${pending} file${pending === 1 ? "" : "s"}` : "Upload";
  const parts = [];
  if (pending) parts.push(`${ready} of ${pending} ready`);
  if (blocked) parts.push(`${blocked} need${blocked === 1 ? "s" : ""} tags`);
  if (done) parts.push(`${done} uploaded`);
  ui.summary.textContent = parts.length ? parts.join(" · ") : "No files picked yet";
  ui.clear.hidden = done === 0;
}

function tagPayload(entry) {
  const out = {};
  const shared = entry.fileTags ? sharedFor(entry) : {};
  for (const k of ["title", "artist", "album", "track", "album_artist", "year", "disc"]) {
    const t = String(entry.typed[k] ?? "").trim();
    if (t) out[k] = t;
    else if (entry.fileTags && !present(k, entry.fileTags[k]) && shared[k]) out[k] = String(shared[k]).trim();
  }
  return out;
}

function send(entry) {
  return new Promise((resolve) => {
    const fd = new FormData();
    fd.append("tags", JSON.stringify(tagPayload(entry)));
    const cover = entry.typed.cover || (entry.fileTags?.cover ? null : state.shared.cover);
    if (cover) fd.append("cover", cover, cover.name);
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

async function uploadAll() {
  if (state.busy) return;
  state.busy = true;
  refresh();
  const order = [...state.files].sort((x, y) => (y.fileTags?.cover ? 1 : 0) - (x.fileTags?.cover ? 1 : 0));
  for (const e of order) {
    if (e.status === "done" || e.status === "reading" || missingOf(e).length) continue;
    e.status = "uploading";
    e.el.msg.textContent = "";
    e.el.bar.style.width = "0";
    refresh();
    const { status, body } = await send(e);
    if (status === 201) {
      e.status = "done";
      e.el.msg.textContent = "Added: " + (body.libraryPath || body.filename);
      e.el.fields.replaceChildren();
      const key = albumKey({ artist: body.artist, album: body.album, ...(e.fileTags ? effective(e) : {}) });
      if (key) folderCovers.set(key, true);
    } else if (status === 422 && body.missing) {
      e.status = "error";
      e.serverMissing = body.missing;
      e.el.msg.textContent = "Server: missing " + body.missing.map((k) => LABELS[k]?.toLowerCase() || k).join(", ");
      const t = body.tags || {};
      e.fileTags = { title: t.title || "", artist: t.artist || "", album: t.album || "", track: t.track || "",
        album_artist: t.album_artist || "", year: t.year || "", disc: t.disc || "", cover: !body.missing.includes("cover") };
      e.typed = {};
      buildFields(e);
    } else if (status === 401) {
      e.status = "error";
      e.el.msg.textContent = ERRORS.unauthorized;
      state.busy = false;
      refresh();
      onUnauthorized();
      return;
    } else {
      e.status = "error";
      e.el.msg.textContent = ERRORS[body.error] || `Upload failed (${body.error || status})`;
    }
    refresh();
  }
  state.busy = false;
  refresh();
  loadRecent();
}

let onUnauthorized = () => {};

async function loadRecent() {
  const res = await fetch("/api/uploads");
  if (!res.ok) return;
  const list = await res.json();
  ui.recent.replaceChildren(...list.slice(0, 20).map((u) =>
    h("li", {}, h("span", { class: "fn" }, u.libraryPath || u.filename), h("span", { class: "chip " + (u.status === "in-library" ? "ok" : "bad") },
      u.status === "in-library" ? "in library" : "rejected" + (u.missing?.length ? ": " + u.missing.join(", ") : "")))));
  ui.recentBox.hidden = list.length === 0;
}

function sharedInput(key) {
  return h("label", { class: "field" }, h("span", {}, LABELS[key]),
    h("input", { type: key === "year" ? "number" : "text", oninput: refresh }));
}

export function renderUpload(container, opts = {}) {
  state = { files: [], shared: { cover: null }, busy: false, nextId: 1 };
  if (opts.onUnauthorized) onUnauthorized = opts.onUnauthorized;
  const picker = h("input", { type: "file", multiple: true, accept: "audio/*,.flac,.mp3,.m4a,.ogg,.opus", hidden: true,
    onchange: (e) => { addFiles(e.target.files); e.target.value = ""; } });
  const drop = h("div", { class: "drop", tabindex: "0", role: "button",
    onclick: () => picker.click(),
    onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); picker.click(); } },
    ondragover: (e) => { e.preventDefault(); drop.classList.add("over"); },
    ondragleave: () => drop.classList.remove("over"),
    ondrop: (e) => { e.preventDefault(); drop.classList.remove("over"); addFiles(e.dataTransfer.files); },
  }, h("strong", {}, "Drop audio files here"), h("span", {}, "or tap to pick · FLAC, MP3, M4A, OGG, Opus"), picker);

  ui.shared = Object.fromEntries(SHARED_FIELDS.map((k) => [k, null]));
  const sharedFields = SHARED_FIELDS.map((k) => { const el = sharedInput(k); ui.shared[k] = el.querySelector("input"); return el; });
  const coverInput = h("label", { class: "field" }, h("span", {}, LABELS.cover),
    h("input", { type: "file", accept: "image/jpeg,image/png", onchange: (e) => { state.shared.cover = e.target.files[0] || null; refresh(); } }));
  ui.sharedBox = h("section", { class: "card shared", hidden: true },
    h("h2", {}, "For all files that lack it"),
    h("p", { class: "muted" }, "Fills in album fields and cover art only where a file has none."),
    h("div", { class: "fields" }, ...sharedFields, coverInput));

  ui.list = h("div", { class: "list" });
  ui.summary = h("span", { class: "muted" });
  ui.upload = h("button", { class: "primary", onclick: uploadAll, disabled: true }, "Upload");
  ui.clear = h("button", { class: "ghost", hidden: true, onclick: () => {
    for (const e of state.files.filter((x) => x.status === "done")) e.el.row.remove();
    state.files = state.files.filter((x) => x.status !== "done");
    refresh();
  } }, "Clear uploaded");
  ui.recent = h("ul", { class: "recent" });
  ui.recentBox = h("section", { class: "card", hidden: true }, h("h2", {}, "Recent uploads"), ui.recent);

  container.replaceChildren(
    drop, ui.sharedBox,
    h("section", { class: "card files" }, ui.list, h("div", { class: "foot" }, ui.summary, h("div", { class: "actions" }, ui.clear, ui.upload))),
    ui.recentBox);
  refresh();
  loadRecent();
}
