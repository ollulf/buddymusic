export const REQUIRED = ["title", "artist", "album", "track", "cover"];
export const TEXT_FIELDS = ["title", "artist", "album", "track", "year", "disc"];
export const LABELS = {
  title: "Title",
  artist: "Artist",
  album: "Album",
  track: "Track no.",
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

export function fromFileName(name) {
  let base = String(name || "").replace(/\.[^.]+$/, "").trim();
  let track = "";
  let disc = "";
  const m = base.match(/^(?:(\d{1,2})[-.](?=\d))?(\d{1,3})(?:\s*[-._)\]]+\s*|\s+)(.*)$/);
  if (m && m[3].trim()) {
    disc = m[1] ? String(parseInt(m[1], 10)) : "";
    track = String(parseInt(m[2], 10));
    base = m[3];
  }
  if (!/\s/.test(base)) base = base.replace(/_+/g, " ");
  base = base.replace(/^[\s\-_.]+|[\s\-_.]+$/g, "").replace(/\s+/g, " ");
  return { title: base, track: track === "0" ? "" : track, disc };
}

export function mostCommon(values) {
  const counts = new Map();
  let best = "", bestN = 0;
  for (const raw of values) {
    const v = String(raw ?? "").trim();
    if (!v) continue;
    const n = (counts.get(v) || 0) + 1;
    counts.set(v, n);
    if (n > bestN) { best = v; bestN = n; }
  }
  return best;
}
