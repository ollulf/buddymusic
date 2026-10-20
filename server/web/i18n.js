const KEY = "bm.lang";

export const LANGS = { en: "English", de: "Deutsch" };

const en = {
  "settings.title": "Settings",
  "settings.close": "✕ Close",
  "settings.closeLabel": "Close settings",
  "settings.language": "Language",
  "settings.languageHint": "Saved on this device. The page reloads to switch.",
  "settings.quality": "Streaming quality",
  "settings.format": "Original format",
  "settings.preload": "Preload next song",
  "settings.qualityHint": "Saved on this device. Auto picks Original, Normal or Low per song from how fast the music loads.",
  "settings.noOpus": "This browser can't play Opus, so Normal and Low use MP3 192 and MP3 128.",
  "settings.formatHint": "What Original streams.",
  "settings.preloadHint": "Saved on this device. Loads the next song before the current one ends.",
  "quality.auto": "Auto",
  "quality.original": "Original",
  "quality.normal": "Normal",
  "quality.low": "Low",
  "format.flac": "FLAC (original file as stored)",
  "format.mp3": "MP3 320 kbps",
  "preload.on": "On",
  "preload.off": "Off",
  "preload.onHint": "Next song starts instantly, uses a bit more data",
  "preload.offHint": "Saves data, short pause between songs",
  "avatar.notPicture": "That file is not a picture",
  "avatar.saveFailed": "Could not save the picture",
  "avatar.resetFailed": "Could not reset the picture",
  "avatar.changeLabel": "Change profile picture",
  "avatar.change": "Change picture",
  "avatar.reset": "Reset picture",
  "common.saving": "Saving…",
};

const de = {
  "settings.title": "Einstellungen",
  "settings.close": "✕ Schließen",
  "settings.closeLabel": "Einstellungen schließen",
  "settings.language": "Sprache",
  "settings.languageHint": "Wird auf diesem Gerät gespeichert. Die Seite lädt zum Wechseln neu.",
  "settings.quality": "Streaming-Qualität",
  "settings.format": "Original-Format",
  "settings.preload": "Nächsten Song vorladen",
  "settings.qualityHint": "Wird auf diesem Gerät gespeichert. Auto wählt pro Song Original, Normal oder Niedrig, je nachdem wie schnell die Musik lädt.",
  "settings.noOpus": "Dieser Browser kann kein Opus abspielen, deshalb nutzen Normal und Niedrig MP3 192 und MP3 128.",
  "settings.formatHint": "Was bei Original gestreamt wird.",
  "settings.preloadHint": "Wird auf diesem Gerät gespeichert. Lädt den nächsten Song, bevor der aktuelle endet.",
  "quality.auto": "Auto",
  "quality.original": "Original",
  "quality.normal": "Normal",
  "quality.low": "Niedrig",
  "format.flac": "FLAC (Originaldatei wie gespeichert)",
  "format.mp3": "MP3 320 kbps",
  "preload.on": "An",
  "preload.off": "Aus",
  "preload.onHint": "Nächster Song startet sofort, braucht etwas mehr Daten",
  "preload.offHint": "Spart Daten, kurze Pause zwischen Songs",
  "avatar.notPicture": "Diese Datei ist kein Bild",
  "avatar.saveFailed": "Das Bild konnte nicht gespeichert werden",
  "avatar.resetFailed": "Das Bild konnte nicht zurückgesetzt werden",
  "avatar.changeLabel": "Profilbild ändern",
  "avatar.change": "Bild ändern",
  "avatar.reset": "Bild zurücksetzen",
  "common.saving": "Speichert…",
};

const DICTS = { en, de };

function pick() {
  let saved = null;
  try { saved = localStorage.getItem(KEY); } catch {}
  if (saved && DICTS[saved]) return saved;
  const nav = (typeof navigator !== "undefined" && navigator.language) || "";
  return nav.toLowerCase().startsWith("de") ? "de" : "en";
}

export const lang = pick();

const plurals = new Intl.PluralRules(lang);

export function t(key, vars = {}) {
  let s = DICTS[lang][key] ?? en[key] ?? key;
  if (typeof s === "object") s = s[plurals.select(vars.n ?? 0)] ?? s.other;
  return s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
}

export function setLang(code) {
  if (!DICTS[code] || code === lang) return;
  try { localStorage.setItem(KEY, code); } catch {}
  location.reload();
}

const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });

export function relTime(ms) {
  const sec = Math.round((ms - Date.now()) / 1000);
  const abs = Math.abs(sec);
  if (abs < 60) return rtf.format(sec, "second");
  if (abs < 3600) return rtf.format(Math.round(sec / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(sec / 3600), "hour");
  if (abs < 2592000) return rtf.format(Math.round(sec / 86400), "day");
  if (abs < 31536000) return rtf.format(Math.round(sec / 2592000), "month");
  return rtf.format(Math.round(sec / 31536000), "year");
}

if (typeof document !== "undefined") document.documentElement.lang = lang;
