self.addEventListener("push", (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch {}
  event.waitUntil(self.registration.showNotification(data.title || "buddymusic", {
    body: data.body || "",
    icon: "/apple-touch-icon.png",
    tag: data.tag || undefined,
    data: { url: data.url || "/" },
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = new URL(event.notification.data?.url || "/", self.location.origin).href;
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const c of all) {
      if (new URL(c.url).origin === self.location.origin) {
        c.postMessage({ type: "open-recs" });
        return c.focus();
      }
    }
    return self.clients.openWindow(url);
  })());
});
