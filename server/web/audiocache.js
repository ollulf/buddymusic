const CACHE_NAME = "bm-audio-v1";
const INDEX_KEY = "bm.audioCache";
const CAP_BYTES = 300 * 1024 * 1024;
const RETRY_DELAYS = [0, 1000, 3000, 6000, 12000];

const hasCache = (() => { try { return typeof caches !== "undefined" && !!caches.open; } catch { return false; } })();

function readIndex() {
  try { return JSON.parse(localStorage.getItem(INDEX_KEY) || "{}") || {}; } catch { return {}; }
}
function writeIndex(ix) {
  try { localStorage.setItem(INDEX_KEY, JSON.stringify(ix)); } catch {}
}

const index = readIndex();

export const isCached = (url) => hasCache && !!index[url];

function touch(url) {
  if (!index[url]) return;
  index[url].at = Date.now();
  writeIndex(index);
}

async function evict(cache) {
  let total = Object.values(index).reduce((s, e) => s + (e.size || 0), 0);
  const order = Object.keys(index).sort((a, b) => index[a].at - index[b].at);
  for (const url of order) {
    if (total <= CAP_BYTES) break;
    total -= index[url].size || 0;
    delete index[url];
    await cache.delete(url).catch(() => {});
  }
  writeIndex(index);
}

export async function cachedBlob(url) {
  if (!isCached(url)) return null;
  try {
    const cache = await caches.open(CACHE_NAME);
    const res = await cache.match(url);
    if (!res) { delete index[url]; writeIndex(index); return null; }
    const b = await res.blob();
    touch(url);
    return b;
  } catch { return null; }
}

export async function storeBlob(url, blob) {
  if (!hasCache || !blob || blob.size < 4096 || blob.size > CAP_BYTES / 4) return;
  try {
    const cache = await caches.open(CACHE_NAME);
    await cache.put(url, new Response(blob, { headers: { "Content-Type": blob.type || "application/octet-stream", "Content-Length": String(blob.size) } }));
    index[url] = { size: blob.size, at: Date.now() };
    await evict(cache);
  } catch {}
}

const wait = (ms, signal) => new Promise((resolve, reject) => {
  const id = setTimeout(resolve, ms);
  if (signal) signal.addEventListener("abort", () => { clearTimeout(id); reject(new DOMException("aborted", "AbortError")); }, { once: true });
});

export async function fetchAudio(url, signal) {
  const parts = [];
  let got = 0;
  let total = 0;
  let ranged = false;
  let type = "";
  for (let attempt = 0; attempt < RETRY_DELAYS.length; attempt++) {
    if (attempt) await wait(RETRY_DELAYS[attempt], signal);
    if (!ranged && got) { parts.length = 0; got = 0; }
    try {
      const headers = ranged && got ? { Range: `bytes=${got}-` } : {};
      const res = await fetch(url, { signal, headers });
      if (res.status === 206 && got) {
        const m = /bytes (\d+)-/.exec(res.headers.get("Content-Range") || "");
        if (!m || +m[1] !== got) { parts.length = 0; got = 0; ranged = false; continue; }
      } else if (res.ok) {
        parts.length = 0;
        got = 0;
        ranged = res.headers.get("Accept-Ranges") === "bytes";
        total = +res.headers.get("Content-Length") || 0;
        type = res.headers.get("Content-Type") || "";
      } else if (res.status >= 400 && res.status < 500) {
        throw Object.assign(new Error(String(res.status)), { fatal: true });
      } else continue;
      const reader = res.body.getReader();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        parts.push(value);
        got += value.length;
      }
      if (total && got < total) continue;
      return new Blob(parts, { type });
    } catch (err) {
      if (signal && signal.aborted) throw err;
      if (err && err.fatal) throw err;
    }
  }
  throw new Error("fetch failed");
}
