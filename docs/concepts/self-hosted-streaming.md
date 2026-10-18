# Self-hosted music streaming

Visual concept page: https://claude.ai/artifact/QjNmhD4zoo48DB1jHPSh9K

## Recommendation

- **Server:** Navidrome on the always-on Windows laptop, installed as a Windows service, library in `D:\Music`, ffmpeg for transcoding.
- **Remote access:** Tailscale on laptop and phone. No router ports opened. Same address at home (direct over Wi-Fi) and on the go (WireGuard tunnel).
- **Phone clients (Subsonic API):** Symfonium or Tempo on Android, Amperfy, play:Sub or substreamer on iOS. Use offline downloads for favourite playlists.
- **Mobile data:** transcode to Opus 128 kbps (about 58 MB per hour); original quality on Wi-Fi.
- **Laptop:** sleep never on AC, lid close does nothing, battery charge limit, nightly backup to USB disk plus an off-site copy.

## Alternatives considered

- Jellyfin (better if films/TV matter), Funkwhale and Ampache (too heavy), a custom server (too much work).
- Caddy with a domain and HTTPS (good for sharing public links, but exposes a login page), Cloudflare Tunnel (no open ports, but media streaming is against its terms and traffic is decrypted at Cloudflare).

## Where buddymusic fits

Keep Navidrome as the engine and build buddymusic as a custom client on the Subsonic API, or as a companion service for features existing clients lack.
