export const REQUIRED = ["title", "artist", "album", "track", "cover"];
export const TEXT_FIELDS = ["title", "artist", "album", "track", "album_artist", "year", "disc"];
export const LABELS = {
  title: "Title",
  artist: "Artist",
  album: "Album",
  track: "Track no.",
  album_artist: "Album artist",
  year: "Year",
  disc: "Disc",
  cover: "Cover art",
};

export function trackNumber(v) {
  const n = parseInt(String(v ?? "").split("/")[0], 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

export function present(field, value) {
  if (field === "cover") return !!value;
  if (field === "track") return trackNumber(value) > 0;
  return String(value ?? "").trim() !== "";
}

export function missingFields(tags) {
  return REQUIRED.filter((k) => !present(k, tags[k]));
}

export function fromMediaTags(t) {
  const tags = t || {};
  const raw = (k) => {
    const v = tags[k];
    if (v == null) return "";
    if (typeof v === "object" && "data" in v) return typeof v.data === "string" ? v.data : "";
    return String(v);
  };
  return {
    title: tags.title || "",
    artist: tags.artist || "",
    album: tags.album || "",
    track: tags.track != null ? String(tags.track) : "",
    album_artist: raw("TPE2") || raw("ALBUMARTIST") || raw("aART") || "",
    year: tags.year ? String(tags.year) : raw("DATE") || raw("TDRC") || raw("©day") || "",
    disc: raw("TPOS") || raw("DISCNUMBER") || "",
    cover: !!(tags.picture && tags.picture.data && tags.picture.data.length),
  };
}

export function merge(fileTags, typed, shared) {
  const out = { ...fileTags };
  for (const k of TEXT_FIELDS) {
    const t = String(typed[k] ?? "").trim();
    const s = String(shared[k] ?? "").trim();
    if (t) out[k] = t;
    else if (!present(k, out[k]) && s) out[k] = s;
  }
  out.cover = !!(fileTags.cover || typed.cover || shared.cover);
  return out;
}
