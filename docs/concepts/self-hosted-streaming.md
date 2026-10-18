# Self-hosted music streaming

Visual concept page: https://claude.ai/artifact/QjNmhD4zoo48DB1jHPSh9K

Step-by-step laptop setup, with what Claude Code can do for you: [../setup-checklist.md](../setup-checklist.md)

## Goal

An always-on Windows laptop serves the music library. It starts out just for me (phase 1) and grows into **buddymusic**: my own app that friends use too, with profiles and shared uploads.

## Recommendation: option A, Navidrome + companion API

- **Engine:** Navidrome stays the music server. It already handles multiple users, per-user playlists, favourites and play history, transcoding and library scans.
- **buddymusic API:** a small new service (Go, or Node/TypeScript) on the laptop with SQLite. It handles invites, login, roles (admin/friend), avatars and profiles, uploads, uploader records and dedupe. It creates Navidrome users and triggers `startScan` through Navidrome's API.
- **Routing:** Caddy on the laptop sends `/api` to the buddymusic API and `/rest` (Subsonic) to Navidrome, all under one HTTPS address.
- **App:** Flutter for Android and iOS (`just_audio` + `audio_service`, offline cache, upload from phone).
- **Option B (own backend from scratch)** means building the scanner, streaming, transcoding and search yourself: months of work. It stays possible later because the app only talks to `/api` and `/rest`.

## Friends access

- Phase 2: Tailscale node sharing. Free and safe, but every friend has to install and run a VPN app.
- Phase 3+: a small relay VPS (about 4 €/month) running Caddy with TLS on a domain, forwarding over Tailscale to the laptop. Friends only need the app, no home ports are opened, and it works behind carrier-grade NAT. Cloudflare Tunnel is not suitable because its terms forbid heavy media streaming.

## Profiles

- One Navidrome account per person, each with their own playlists, favourites and history. The buddymusic API adds avatar, display name and "added by" on tracks. Public playlists are shared with everyone.

## Shared uploads

- Flow: resumable upload to `/api/uploads`, then `D:\Music\_incoming`, ffprobe check, tags filled in from MusicBrainz/AcoustID, move to `D:\Music\Library\<user>\Artist\Album`, then a Navidrome scan.
- Duplicates are caught by SHA-256 and by artist + title + duration (or AcoustID).
- Accepted formats are FLAC, MP3, M4A/AAC and OGG/Opus, up to 200 MB per file, with a quota per friend.
- Uploaders can delete their own files and admins can delete any. Deleted files sit in `_trash` for 30 days.
- The library lives on a separate drive, with a warning at 85 % full and a nightly backup to USB plus an off-site copy.
- Legal: private, invite-only, purchased music only. No public sharing.

## Roadmap

1. Just me: Navidrome + Tailscale + an existing Subsonic app.
2. First friends: Navidrome accounts, Tailscale node sharing, existing apps.
3. buddymusic API and relay VPS.
4. buddymusic Flutter app.
5. Only if needed: social features, or an own backend behind the same routes.

## Phase 1 base setup (round 1)

- **Server:** Navidrome as a Windows service, library in `D:\Music`, ffmpeg for transcoding.
- **Remote access:** Tailscale on laptop and phone, with no router ports opened.
- **Phone clients:** Symfonium or Tempo (Android), Amperfy, play:Sub or substreamer (iOS). Opus 128 kbps on mobile data.
- **Laptop:** sleep never on AC, lid close does nothing, battery charge limit, nightly backup.
- **Alternatives considered:** Jellyfin, Funkwhale and Ampache for the server; plain WireGuard, Caddy with port forwarding and Cloudflare Tunnel for access.
