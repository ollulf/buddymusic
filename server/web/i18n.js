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
  "player.volumeLabel": "Volume",
  "player.volume": "Volume {v}%",
  "player.volumeMuted": "Volume muted",
  "player.mute": "Mute",
  "player.unmute": "Unmute",
  "player.mutePhone": "Mute (use the phone's volume buttons)",
  "player.unmutePhone": "Unmute (use the phone's volume buttons)",
  "player.openAlbum": "Open album",
  "player.seek": "Seek",
  "player.previous": "Previous",
  "player.play": "Play",
  "player.pause": "Pause",
  "player.next": "Next",
  "player.shuffle": "Shuffle",
  "player.repeat": "Repeat",
  "player.repeat.off": "Repeat: off",
  "player.repeat.all": "Repeat: all",
  "player.repeat.one": "Repeat: one",
  "player.unknownTitle": "Unknown title",
  "player.showDisc": "Show disc",
  "player.autoQuality": "Auto quality: {tier}",
  "player.fixedQuality": "Fixed quality: {tier}",
  "player.steppedDown": "(stepped down for a slow connection)",
  "player.tapToChange": "{tip}. Tap to change.",
  "player.rateLabel": "Streaming quality {tag}, change",
  "time.justNow": "just now",
  "login.badLogin": "Wrong username or password",
  "login.tooMany": "Too many attempts, wait a few minutes",
  "login.navidrome": "Navidrome is not answering",
  "login.button": "Log in",
  "login.failed": "Login failed",
  "login.noConnection": "No connection to the server",
  "login.intro": "Log in with your Navidrome account.",
  "login.username": "Username",
  "login.password": "Password",
  "login.remember": "Keep me logged in",
  "login.privacy": "Admins can see when you log in and how much you listen. Friends can see what you are playing.",
  "tab.library": "Library",
  "tab.search": "Search",
  "tab.favorites": "Favorites",
  "tab.playlists": "Playlists",
  "tab.upload": "Upload",
  "tab.admin": "Admin",
  "tab.sections": "Sections",
  "dice.title": "Play random songs",
  "dice.failed": "Couldn't load",
  "friends.lastSeen": "last seen {when}",
  "friends.paused": "paused: {track}",
  "friends.idle": "idle",
  "friends.none": "No friends online right now.",
  "friends.nobody": "Nobody else is online right now.",
  "friends.failed": "Could not load friends",
  "friends.title": "Friends online",
  "profile.label": "Profile",
  "profile.logout": "Log out",
  "quality.label": "Streaming quality: {q}",
  "quality.byConnection": "by connection",
  "quality.originalFile": "original file",
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
  "player.volumeLabel": "Lautstärke",
  "player.volume": "Lautstärke {v} %",
  "player.volumeMuted": "Stummgeschaltet",
  "player.mute": "Stumm",
  "player.unmute": "Ton an",
  "player.mutePhone": "Stumm (nutze die Lautstärketasten am Handy)",
  "player.unmutePhone": "Ton an (nutze die Lautstärketasten am Handy)",
  "player.openAlbum": "Album öffnen",
  "player.seek": "Spulen",
  "player.previous": "Zurück",
  "player.play": "Abspielen",
  "player.pause": "Pause",
  "player.next": "Weiter",
  "player.shuffle": "Zufall",
  "player.repeat": "Wiederholen",
  "player.repeat.off": "Wiederholen: aus",
  "player.repeat.all": "Wiederholen: alle",
  "player.repeat.one": "Wiederholen: einen",
  "player.unknownTitle": "Unbekannter Titel",
  "player.showDisc": "Platte zeigen",
  "player.autoQuality": "Auto-Qualität: {tier}",
  "player.fixedQuality": "Feste Qualität: {tier}",
  "player.steppedDown": "(runtergestuft wegen langsamer Verbindung)",
  "player.tapToChange": "{tip}. Tippen zum Ändern.",
  "player.rateLabel": "Streaming-Qualität {tag}, ändern",
  "time.justNow": "gerade eben",
  "login.badLogin": "Falscher Benutzername oder falsches Passwort",
  "login.tooMany": "Zu viele Versuche, warte ein paar Minuten",
  "login.navidrome": "Navidrome antwortet nicht",
  "login.button": "Einloggen",
  "login.failed": "Login fehlgeschlagen",
  "login.noConnection": "Keine Verbindung zum Server",
  "login.intro": "Log dich mit deinem Navidrome-Konto ein.",
  "login.username": "Benutzername",
  "login.password": "Passwort",
  "login.remember": "Eingeloggt bleiben",
  "login.privacy": "Admins sehen, wann du dich einloggst und wie viel du hörst. Freunde sehen, was du gerade spielst.",
  "tab.library": "Bibliothek",
  "tab.search": "Suche",
  "tab.favorites": "Favoriten",
  "tab.playlists": "Playlists",
  "tab.upload": "Upload",
  "tab.admin": "Admin",
  "tab.sections": "Bereiche",
  "dice.title": "Zufällige Songs abspielen",
  "dice.failed": "Laden fehlgeschlagen",
  "friends.lastSeen": "zuletzt da {when}",
  "friends.paused": "pausiert: {track}",
  "friends.idle": "inaktiv",
  "friends.none": "Gerade sind keine Freunde online.",
  "friends.nobody": "Gerade ist sonst niemand online.",
  "friends.failed": "Freunde konnten nicht geladen werden",
  "friends.title": "Freunde online",
  "profile.label": "Profil",
  "profile.logout": "Ausloggen",
  "quality.label": "Streaming-Qualität: {q}",
  "quality.byConnection": "je nach Verbindung",
  "quality.originalFile": "Originaldatei",
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

export function relTime(unixSec) {
  const sec = Math.round(unixSec - Date.now() / 1000);
  const abs = Math.abs(sec);
  if (abs < 90) return t("time.justNow");
  if (abs < 3600) return rtf.format(Math.round(sec / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(sec / 3600), "hour");
  if (abs < 2592000) return rtf.format(Math.round(sec / 86400), "day");
  if (abs < 31536000) return rtf.format(Math.round(sec / 2592000), "month");
  return rtf.format(Math.round(sec / 31536000), "year");
}

if (typeof document !== "undefined") document.documentElement.lang = lang;
