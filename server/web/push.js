import { api, apiJSON } from "./dom.js";
import { lang } from "./i18n.js";

export const pushSupported = "serviceWorker" in navigator && "PushManager" in window && "Notification" in window && window.isSecureContext;
const standalone = window.matchMedia?.("(display-mode: standalone)").matches || navigator.standalone === true;
const ios = /iP(hone|ad|od)/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
export const needsHomeScreen = ios && !standalone;

function keyBytes(b64) {
  const s = atob(b64.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (b64.length % 4)) % 4));
  return Uint8Array.from(s, (c) => c.charCodeAt(0));
}

export function registerWorker() {
  if (!("serviceWorker" in navigator) || !window.isSecureContext) return Promise.resolve(null);
  return navigator.serviceWorker.register("/sw.js").catch(() => null);
}

export function onOpenRecs(fn) {
  if ("serviceWorker" in navigator) navigator.serviceWorker.addEventListener("message", (e) => { if (e.data?.type === "open-recs") fn(); });
  const params = new URLSearchParams(location.search);
  if (params.has("recs")) {
    params.delete("recs");
    const q = params.toString();
    history.replaceState(history.state, "", location.pathname + (q ? "?" + q : "") + location.hash);
    setTimeout(fn, 0);
  }
}

export async function pushEnabled() {
  if (!pushSupported || Notification.permission !== "granted") return false;
  const reg = await navigator.serviceWorker.getRegistration();
  return !!(reg && await reg.pushManager.getSubscription());
}

export async function enablePush() {
  if (await Notification.requestPermission() !== "granted") throw new Error("denied");
  await registerWorker();
  const reg = await navigator.serviceWorker.ready;
  const { key } = await api("/api/push/key");
  let sub = await reg.pushManager.getSubscription();
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
  await apiJSON("POST", "/api/push/subscription", { ...sub.toJSON(), lang });
}

export async function disablePush() {
  const reg = await navigator.serviceWorker.getRegistration();
  const sub = reg && await reg.pushManager.getSubscription();
  if (!sub) return;
  try { await apiJSON("DELETE", "/api/push/subscription", { endpoint: sub.endpoint }); } catch {}
  await sub.unsubscribe();
}
