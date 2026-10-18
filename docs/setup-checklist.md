# buddymusic setup checklist (Windows laptop)

Step by step, from phase 1 (just me) to the later phases. Each step says who does it:

- **You**: things only you can do, such as account logins, UAC/admin prompts, BIOS settings, phones, inviting friends.
- **Claude**: things Claude Code can do on the laptop, such as writing config files and scripts, running commands, creating scheduled tasks and checking the service. You still approve each command.

None of the commands below have been tested on the real machine. Package IDs, paths and the Navidrome config location are marked **(check)** where they need confirming first.

Concept: [concepts/self-hosted-streaming.md](concepts/self-hosted-streaming.md)

## Phase 0: Claude Code on the laptop

- [ ] **0.1 Install Git and Claude Code** (You)
  - `winget install --id Git.Git -e`
  - Claude Code native installer in PowerShell: `irm https://claude.ai/install.ps1 | iex` **(check against the current Claude Code docs)**
  - Done when: `claude --version` prints a version.
- [ ] **0.2 Log in and open the repo** (You)
  - `git clone <your buddymusic remote> C:\src\buddymusic`, then `cd C:\src\buddymusic` and `claude`. Log in with your account in the browser.
  - Done when: Claude answers inside the repo and can read `docs/setup-checklist.md`.
- [ ] **0.3 Let Claude check the machine** (Claude)
  - Prompt: `Read docs/setup-checklist.md. Check this laptop for phase 1: Windows version, free space per drive, whether winget, ffmpeg, Navidrome and Tailscale are installed, and the current power plan. Don't change anything yet, just report.`
  - Done when: you have a short report of what is missing.

Run Claude Code from a normal terminal. Commands that need admin rights (installing services, `powercfg`, firewall rules) are best run by you in an elevated terminal, or Claude writes them into a `.ps1` script that you run as administrator.

## Phase 1: just me

- [ ] **1.1 Prepare the music library** (You + Claude)
  - You: copy your music to `D:\Music\Library` (separate drive or partition if possible). Optionally tag it with MusicBrainz Picard (`winget install --id MusicBrainz.Picard -e` **(check)**).
  - Claude: count files and formats, list files without tags or with broken names.
  - Done when: everything is under `D:\Music\Library\Artist\Album` and the report shows no big gaps.
- [ ] **1.2 Install ffmpeg and Navidrome** (You approve, Claude runs)
  - `winget install --id Gyan.FFmpeg -e` **(check ID)**
  - Navidrome: Windows MSI from the GitHub releases page (`navidrome_*_windows_amd64_installer.msi`), or `winget search navidrome` to see if a package exists **(check)**. The MSI installs Navidrome as a Windows service.
  - Done when: `ffmpeg -version` works and a `Navidrome` service shows up in `Get-Service *navidrome*`.
- [ ] **1.3 Configure Navidrome** (Claude)
  - Config file in the Navidrome install/data folder (`navidrome.ini` or `navidrome.toml`, **check where the MSI puts it**):
    ```toml
    MusicFolder = 'D:\Music\Library'
    DataFolder = 'C:\ProgramData\Navidrome'
    Port = 4533
    ScanSchedule = '@every 1h'
    EnableTranscodingConfig = true
    LogLevel = 'info'
    ```
  - Same settings work as environment variables (`ND_MUSICFOLDER`, `ND_DATAFOLDER`, `ND_PORT`, `ND_SCANSCHEDULE`).
  - Then `Restart-Service Navidrome` (admin).
  - Done when: `http://localhost:4533` shows the Navidrome login.
- [ ] **1.4 Fallback: run Navidrome as a service yourself** (Claude, only if you used the zip instead of the MSI)
  - WinSW: `navidrome-service.xml` with `<executable>C:\Navidrome\navidrome.exe</executable>`, `<arguments>--configfile C:\Navidrome\navidrome.toml</arguments>`, `<startmode>Automatic</startmode>`, then `navidrome-service.exe install` and `start`.
  - Or NSSM: `nssm install Navidrome C:\Navidrome\navidrome.exe --configfile C:\Navidrome\navidrome.toml`, then `nssm start Navidrome`.
  - Done when: the service is `Running` with start type `Automatic`.
- [ ] **1.5 Create the admin account** (You)
  - Open `http://localhost:4533`, create the admin user, store the password in your password manager. Wait for the first scan.
  - Done when: albums show up in the web UI and a song plays in the browser.
- [ ] **1.6 Keep the laptop awake** (Claude writes, You run as admin)
  - ```powershell
    powercfg /change standby-timeout-ac 0
    powercfg /change hibernate-timeout-ac 0
    powercfg /change monitor-timeout-ac 5
    powercfg /setacvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 0
    powercfg /setactive SCHEME_CURRENT
    ```
  - Windows Update: set active hours, allow restarts only at night.
  - Done when: `powercfg /query SCHEME_CURRENT SUB_BUTTONS LIDACTION` shows AC index `0x0` and the laptop keeps serving music with the lid closed for an hour.
- [ ] **1.7 Battery charge limit** (You)
  - In the vendor tool (Lenovo Vantage, Dell Power Manager, ASUS MyASUS, ...) or BIOS: limit charging to 60-80 %.
  - Done when: the battery stays at the limit while plugged in.
- [ ] **1.8 Firewall** (Claude writes, You run as admin)
  - Allow Navidrome only from Tailscale and the home network, never from the internet. No router port forwarding.
  - `New-NetFirewallRule -DisplayName "Navidrome tailnet" -Direction Inbound -Protocol TCP -LocalPort 4533 -RemoteAddress 100.64.0.0/10 -Action Allow`
  - Optional for home Wi-Fi: same rule with `-RemoteAddress LocalSubnet -Profile Private`.
  - Done when: a phone on home Wi-Fi or Tailscale reaches port 4533 and the router has no forwarding rule.
- [ ] **1.9 Tailscale on the laptop** (You)
  - `winget install --id tailscale.tailscale -e` **(check ID)**, then log in with your Tailscale account.
  - In the admin console: rename the laptop (e.g. `musicbox`) and **disable key expiry** for it. Turn on MagicDNS.
  - Done when: `tailscale status` lists the laptop and `tailscale ip -4` prints a `100.x` address.
- [ ] **1.10 Phone setup** (You)
  - Install Tailscale on the phone and log in with the same account.
  - Install a Subsonic app: Symfonium or Tempo (Android), Amperfy, play:Sub or substreamer (iOS).
  - Server: `http://musicbox:4533` (or the `100.x` address), your Navidrome user.
  - Streaming: Opus 128 kbps on mobile data, original on Wi-Fi. Download a few playlists for offline.
  - Done when: music plays with Wi-Fi off on mobile data.
- [ ] **1.11 Backups** (Claude writes, You approve the scheduled task)
  - Local copy to USB with robocopy, as a script `C:\buddymusic\backup.ps1`:
    ```powershell
    robocopy D:\Music E:\Backup\Music /MIR /R:2 /W:5 /NP /LOG:C:\buddymusic\logs\backup-music.log
    robocopy C:\ProgramData\Navidrome E:\Backup\Navidrome /MIR /R:2 /W:5 /NP /LOG:C:\buddymusic\logs\backup-nd.log
    ```
  - Nightly task: `schtasks /Create /TN "buddymusic backup" /TR "powershell -NoProfile -File C:\buddymusic\backup.ps1" /SC DAILY /ST 03:00 /RU SYSTEM`
  - Off-site (optional): restic to Backblaze B2 or another disk (`winget install --id restic.restic -e` **(check)**, `restic init`, `restic backup D:\Music C:\ProgramData\Navidrome`). You create the B2 account and keep the restic password safe.
  - Done when: the task ran once, the log has no errors and you restored one file as a test.
- [ ] **1.12 Final check** (Claude)
  - Prompt: `Verify phase 1: Navidrome service running and automatic, port 4533 answers on localhost and the Tailscale IP, power settings as in the checklist, backup task exists and its last run succeeded. Report anything that is off.`
  - Done when: everything in the report is green.

Example prompts for phase 1:

- `Write the Navidrome config for D:\Music\Library as in docs/setup-checklist.md step 1.3, find out where the installed MSI expects it, and show me the diff before restarting the service.`
- `Write C:\buddymusic\setup-power-firewall.ps1 with the powercfg and firewall commands from steps 1.6 and 1.8. I will run it as admin.`
- `Create the backup script and scheduled task from step 1.11, with E: as the USB drive. Run it once and show me the log.`

## Phase 2: first friends (coarser)

- [ ] **2.1 Navidrome account per friend** (You, or Claude via the Navidrome UI steps)
  - Create a normal (non-admin) user per friend in the Navidrome web UI. Send the password privately.
  - Done when: the friend can log in at `http://localhost:4533` from the laptop with their user.
- [ ] **2.2 Share the laptop over Tailscale** (You)
  - Tailscale admin console, machine `musicbox` → Share → send the invite link. Friend installs Tailscale and accepts.
  - Optional: an ACL that lets shared users reach only port 4533 (Claude can write the ACL JSON for you to paste).
  - Done when: the friend's phone shows `musicbox` in Tailscale.
- [ ] **2.3 Friend's phone app** (Friend)
  - Same Subsonic app setup as 1.10 with the shared address.
  - Done when: the friend plays a song on mobile data.
- [ ] **2.4 Shared uploads, simple version** (You + Claude)
  - A shared folder (e.g. via Syncthing or a share) that lands in `D:\Music\_incoming`; Claude writes a script that checks and moves files into `Library\<user>\Artist\Album`.
  - Done when: a file a friend drops in appears in Navidrome after the next scan.

Example prompt: `Write a PowerShell script that takes files from D:\Music\_incoming\<user>, checks them with ffprobe, sorts them into D:\Music\Library\<user>\Artist\Album by tags, and moves broken files to _rejected. Dry-run mode first.`

## Phase 3: buddymusic API and relay (coarser)

- [ ] **3.1 Companion API** (Claude)
  - Go + SQLite service in this repo: invites, login, roles, profiles, uploads with dedupe, Navidrome user creation and `startScan`. Runs as a Windows service (WinSW/NSSM) on `localhost:8080`.
  - Prompt: `Following docs/concepts/self-hosted-streaming.md, scaffold the buddymusic API in Go with SQLite in api/: invites, login against Navidrome, roles, profiles, resumable uploads to D:\Music\_incoming with SHA-256 dedupe. Add tests and a README section on running it as a Windows service.`
  - Done when: tests pass and `/api/health` answers locally.
- [ ] **3.2 Caddy on the laptop** (Claude writes, You run as admin)
  - `winget install --id CaddyServer.Caddy -e` **(check ID)**. Caddyfile routes `/api/*` to `localhost:8080` and `/rest/*` to `localhost:4533`, listening on the Tailscale address only.
  - Done when: both routes answer through Caddy.
- [ ] **3.3 Relay VPS** (You: buy VPS and domain, Claude: write configs)
  - You: rent a small VPS (~4 €/month), buy a domain, point `music.<domain>` at the VPS, install Tailscale there and log in.
  - Claude: Caddyfile for the VPS with automatic HTTPS and rate limiting, forwarding to `musicbox` over Tailscale; firewall only 80/443 and SSH.
  - Done when: `https://music.<domain>/api/health` works from a phone without Tailscale.

## Phase 4: buddymusic app (coarser)

- [ ] **4.1 Flutter toolchain** (You install, Claude checks)
  - Flutter SDK, Android Studio, a phone in developer mode. iOS builds need a Mac and an Apple developer account (You).
  - Done when: `flutter doctor` is clean for Android.
- [ ] **4.2 App** (Claude)
  - Prompt: `Scaffold the buddymusic Flutter app in app/: login against /api/login, browse and stream via Subsonic /rest with just_audio and audio_service, offline downloads, upload from phone, profile pages. Server URL configurable.`
  - Done when: the app plays a song on your phone through the relay.
- [ ] **4.3 Hand it to friends** (You)
  - Android: share the APK or use an internal testing track. iOS: TestFlight. Send invite codes.
  - Done when: a friend signs up with an invite and uploads a song.

## Phase 5: only if needed

- [ ] Social features (activity feed, listening together) in the API, or an own backend behind the same `/api` and `/rest` routes. Claude can draft a plan when you get there.

## Check before approving

- winget IDs (`Gyan.FFmpeg`, `tailscale.tailscale`, `CaddyServer.Caddy`, `restic.restic`, `MusicBrainz.Picard`) and whether Navidrome has a winget package: confirm with `winget search` first.
- Where the Navidrome MSI keeps its config and data folder, and whether its service is named `Navidrome`.
- The Claude Code install command, the firewall rule scope (`100.64.0.0/10`) and the `schtasks` line running as SYSTEM with access to the USB drive.
