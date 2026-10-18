# buddymusic setup checklist (Ubuntu Desktop laptop, daily use + music server)

Step by step, from phase 1 (just me) to the later phases. Ubuntu is already installed on the laptop and it stays a **normal everyday Ubuntu Desktop laptop** (assumed: Ubuntu Desktop 24.04 LTS with GNOME). Navidrome runs next to your normal use as a background system service.

Check your version first: open a terminal (Ctrl+Alt+T) and run `lsb_release -a` (or Settings → System → About). If it shows something other than 24.04, tell Claude: most steps still apply, but package names may differ.

What "daily laptop + server" means:

- **Streaming only works while the laptop is awake and online.** When it suspends, when you close it on battery, or when you take it with you, the server is offline (or travels with you). Offline downloads on the phone cover those gaps.
- The default below: no automatic suspend on AC power, lid closed on AC keeps running, and on battery it behaves like a normal laptop. Screen blanking and the lock screen stay on.
- Navidrome is a systemd **system** service, so it streams while you are logged out, on another account or locked. It runs at low CPU/IO priority so it doesn't slow down your own work.

Each step says who does it:

- **You**: things only you can do, such as GNOME settings, account logins, typing your `sudo` password, phones, inviting friends.
- **Claude**: things Claude Code can do on the laptop, such as writing config files, systemd units and scripts, running non-root commands and checking the services. You still approve each command.

Claude Code runs in the normal GNOME Terminal as your user and cannot answer `sudo` password prompts. For root steps, Claude writes a script (e.g. `~/ops/10-navidrome.sh`) and shows it to you, and you run it with `sudo bash ~/ops/10-navidrome.sh`.

None of the commands below have been tested on the real machine. Anything marked **(check)** needs confirming first.

Concept: [concepts/self-hosted-streaming.md](concepts/self-hosted-streaming.md)

## Phase 0: from the running desktop to Claude Code

- [ ] **0.1 Check the version and update** (You)
  - Terminal: `lsb_release -a`, then `sudo apt update && sudo apt full-upgrade -y`, or use the Software Updater app. Reboot if it asks.
  - Settings → System → About: set the device name to `musicbox` (or `sudo hostnamectl set-hostname musicbox`).
  - Done when: `lsb_release -a` shows 24.04 and there are no pending updates.
- [ ] **0.2 Terminal basics, Git and Claude Code** (You)
  - Open GNOME Terminal with Ctrl+Alt+T. Paste with Ctrl+Shift+V.
  - `sudo apt install -y git curl`
  - Claude Code native installer: `curl -fsSL https://claude.ai/install.sh | bash` **(check against the current Claude Code docs)**, then open a new terminal so `claude` is on the PATH.
  - `git clone <your buddymusic remote> ~/buddymusic`, `cd ~/buddymusic`, `claude`, and log in through the browser that opens.
  - No tmux needed: Claude runs in a normal terminal window or tab.
  - Done when: `claude --version` prints a version and Claude can read `docs/setup-checklist.md`.
- [ ] **0.3 Optional: SSH from the Windows PC** (You, only if you want to manage the laptop from the PC)
  - `sudo apt install -y openssh-server`, copy your key from the PC (`type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh max@<laptop-ip> "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys"`), then `PasswordAuthentication no` in `/etc/ssh/sshd_config.d/10-keys-only.conf` and `sudo systemctl restart ssh`.
  - Or skip it and use Tailscale SSH later (step 1.7).
  - Done when: `ssh max@musicbox` from the PC logs in without a password, or you decided to skip it.
- [ ] **0.4 Let Claude check the machine** (Claude)
  - Prompt: `Read docs/setup-checklist.md. Check this Ubuntu Desktop laptop for phase 1: Ubuntu version, disks and free space (lsblk, df -h), whether ffmpeg, navidrome, tailscale, ufw, tlp, power-profiles-daemon and unattended-upgrades are installed, the GNOME power settings (gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type and sleep-inactive-battery-type), the logind lid settings and the battery charge threshold files under /sys/class/power_supply. Don't change anything yet, just report.`
  - Done when: you have a short report of what is missing.

## Phase 1: just me

- [ ] **1.1 Music folder and permissions** (You run, Claude writes)
  - Library in **`/srv/music`**, not `~/Music`: on 24.04 home folders are private (mode `750`), so the `navidrome` service user can't read `~/Music` without opening up your home. `/srv/music` with a shared `music` group keeps your home private and you still manage the files normally in the Files app.
    ```bash
    sudo groupadd --system music
    sudo usermod -aG music max
    sudo mkdir -p /srv/music/{library,_incoming,_trash}
    sudo chown -R max:music /srv/music
    sudo chmod -R 2775 /srv/music
    ```
  - Log out and back in so your user picks up the `music` group. In Files, open `/srv/music/library` and bookmark it (Ctrl+D) so it sits in the sidebar.
  - Music already in `~/Music`? Move it with `mv ~/Music/* /srv/music/library/`, and if you like add a link back: `ln -s /srv/music/library ~/Music/library`.
  - Second drive (optional): format it with the Disks app and mount it at `/srv/music` via `/etc/fstab` by UUID with `nofail` **(check the device first, formatting erases it)**.
  - Claude: count files and formats, list files without tags or with broken names (`ffprobe` loop).
  - Done when: everything is under `/srv/music/library` and `ls -ld /srv/music/library` shows group `music`.
- [ ] **1.2 Install ffmpeg and Navidrome** (Claude writes, You run with sudo)
  - `sudo apt install -y ffmpeg`
  - Navidrome: official `.deb` from the GitHub releases page (`navidrome_<version>_linux_amd64.deb`), then `sudo apt install ./navidrome_*_linux_amd64.deb`. It creates a `navidrome` system user and a `navidrome.service` systemd unit **(check against the Navidrome install docs for the current release)**.
  - `sudo usermod -aG music navidrome`
  - Done when: `ffmpeg -version` works and `systemctl status navidrome` shows the unit.
- [ ] **1.3 Configure Navidrome, lightweight** (Claude writes, You install with sudo)
  - `/etc/navidrome/navidrome.toml` **(check the path used by the .deb)**:
    ```toml
    MusicFolder = "/srv/music/library"
    DataFolder = "/var/lib/navidrome"
    Address = "0.0.0.0"
    Port = 4533
    ScanSchedule = "@every 6h"
    EnableTranscodingConfig = true
    TranscodingCacheSize = "500MB"
    LogLevel = "warn"
    ```
  - Low priority so it never competes with your apps, `/etc/systemd/system/navidrome.service.d/10-desktop.conf`:
    ```ini
    [Service]
    Nice=10
    CPUWeight=20
    IOWeight=20
    MemoryHigh=512M
    SupplementaryGroups=music
    ```
  - Transcoding: in the Navidrome web UI, set your phone player to Opus 128 kbps. One ffmpeg process per active stream is light; many friends transcoding FLAC at once is not, so keep an eye on it in phase 2.
  - `sudo systemctl daemon-reload && sudo systemctl enable --now navidrome && sudo systemctl restart navidrome`
  - Done when: `curl -sI http://localhost:4533` answers and `systemctl show navidrome -p Nice -p CPUWeight` shows the values **(check `TranscodingCacheSize` against the current Navidrome config docs)**.
- [ ] **1.4 Create the admin account** (You)
  - Open `http://localhost:4533` in Firefox on the laptop, create the admin user, store the password in your password manager. Wait for the first scan.
  - Done when: albums show up and a song plays in the browser.
- [ ] **1.5 Power: awake on AC, normal laptop on battery** (You in GNOME settings, Claude writes the logind drop-in, You run with sudo)
  - Tradeoff: streaming only works while the laptop is awake. This default keeps it awake whenever it is plugged in and leaves battery behaviour alone. Sleep targets are **not** masked.
  - GNOME: Settings → Power → **Automatic Suspend**: off for "Plugged In", leave "On Battery Power" on (e.g. 20 min). Screen blank and automatic screen lock stay on, they don't stop Navidrome. Terminal equivalent:
    ```bash
    gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type 'nothing'
    ```
  - Lid closed on AC keeps running, on battery it still suspends. `/etc/systemd/logind.conf.d/10-lid.conf`:
    ```ini
    [Login]
    HandleLidSwitch=suspend
    HandleLidSwitchExternalPower=ignore
    HandleLidSwitchDocked=ignore
    ```
    Then reboot (simplest), or `sudo systemctl restart systemd-logind`, which can end your desktop session, so save your work first **(check: GNOME may handle the lid itself while you are logged in; if closing the lid on AC still suspends, Claude checks `systemd-inhibit --list` and the GNOME settings)**.
  - Optional: stay awake while someone is streaming, even on battery. Claude writes a small script and systemd service that asks Navidrome's `getNowPlaying` endpoint every minute and holds `systemd-inhibit --what=sleep:idle --why="buddymusic stream"` while anything plays. Off by default.
  - When you take the laptop with you, the server goes with it: it keeps streaming over Tailscale while awake and online, and is offline while suspended.
  - Done when: plugged in with the lid closed, music keeps playing for 30 minutes, and on battery closing the lid still suspends.
- [ ] **1.6 Battery charge limit** (You + Claude)
  - Ubuntu 24.04 Desktop uses `power-profiles-daemon` (the power mode switch in GNOME's quick settings), which **conflicts with TLP**. Preferred order:
    1. BIOS/UEFI or vendor setting ("battery care", "conservation mode", stored in firmware). No extra software, no conflict.
    2. GNOME: newer GNOME versions have a charge limit switch in Settings → Power; GNOME 46 on 24.04 usually doesn't **(check on your machine)**.
    3. Plain sysfs threshold without TLP: a small systemd oneshot that writes `80` to `/sys/class/power_supply/BAT0/charge_control_end_threshold` at boot. Claude writes it; power-profiles-daemon stays.
    4. Only if you want TLP's full tuning: `sudo apt install -y tlp` (this removes or needs you to disable `power-profiles-daemon`, and the GNOME power mode switch disappears), then `/etc/tlp.d/10-battery.conf` with `START_CHARGE_THRESH_BAT0=60` and `STOP_CHARGE_THRESH_BAT0=80`.
  - Default: option 1 if your laptop has it, otherwise option 3.
  - Done when: the battery stops charging at about 80 %.
- [ ] **1.7 Tailscale on the laptop** (You)
  - `curl -fsSL https://tailscale.com/install.sh | sh`, then `sudo tailscale up` (add `--ssh` only if you want SSH from the PC over Tailscale) and log in in the browser.
  - In the admin console: name the machine `musicbox`, **disable key expiry**, turn on MagicDNS.
  - It is a normal Tailscale node, so you can use it for your own things too, e.g. reaching your Windows PC from the laptop on the road. Leave exit-node features off unless you want them.
  - Done when: `tailscale status` lists the laptop and `tailscale ip -4` prints a `100.x` address.
- [ ] **1.8 Firewall with ufw, desktop friendly** (Claude writes, You run with sudo)
  - Incoming closed except Tailscale. Outgoing stays open, so browsing, updates, network printers and casting keep working. No router port forwarding.
    ```bash
    sudo ufw default deny incoming
    sudo ufw default allow outgoing
    sudo ufw allow in on tailscale0
    sudo ufw enable
    ```
  - Optional, only for things you use: Navidrome on home Wi-Fi without Tailscale `sudo ufw allow from 192.168.0.0/16 to any port 4533 proto tcp`, SSH from the PC `sudo ufw allow from 192.168.0.0/16 to any port 22 proto tcp`, printer/device discovery `sudo ufw allow 5353/udp`, GSConnect/KDE Connect `sudo ufw allow 1714:1764/udp` and `sudo ufw allow 1714:1764/tcp`. Adjust the subnet to your router **(check)**.
  - Done when: `sudo ufw status verbose` shows the rules, the phone reaches port 4533 over Tailscale, and your usual desktop things still work.
- [ ] **1.9 Updates** (You)
  - Keep Ubuntu's defaults: security updates install in the background (`unattended-upgrades`), everything else comes through Software Updater. **No automatic reboot**, it is your laptop.
  - When Ubuntu says a restart is required, reboot when it suits you. Navidrome starts again by itself.
  - Done when: `systemctl is-active unattended-upgrades` says `active`.
- [ ] **1.10 Phone setup** (You)
  - Install Tailscale on the phone and log in with the same account.
  - Install a Subsonic app: Symfonium or Tempo (Android), Amperfy, play:Sub or substreamer (iOS).
  - Server: `http://musicbox:4533` (or the `100.x` address), your Navidrome user.
  - Streaming: Opus 128 kbps on mobile data, original on Wi-Fi. **Download a few playlists for offline** for when the laptop sleeps or travels with you.
  - Done when: music plays with Wi-Fi off on mobile data.
- [ ] **1.11 Backups** (Claude writes, You run with sudo and plug in the USB drive)
  - Music server data: one system timer, because GNOME Backups (Déjà Dup) runs as your user and can't read `/var/lib/navidrome`. `/usr/local/bin/buddymusic-backup` runs `rsync -a --delete /srv/music/ /mnt/backup/music/` and copies `/var/lib/navidrome` (Navidrome stops for a few seconds), as `buddymusic-backup.service` + `.timer` with `OnCalendar=daily` and `Persistent=true`, so a run missed while the laptop was asleep or away happens the next time it is on. It skips if the USB drive (label `backup`, mounted at `/mnt/backup` with `nofail`) isn't plugged in.
  - Your personal files: keep using GNOME Backups (Déjà Dup) for your home folder as usual, independent of this.
  - Off-site (optional, later): restic to Backblaze B2 on a second timer.
  - Done when: `systemctl list-timers` shows the timer, `journalctl -u buddymusic-backup` has a clean run, and you restored one file as a test.
- [ ] **1.12 Final check** (Claude)
  - Prompt: `Verify phase 1 on this Ubuntu Desktop laptop: navidrome enabled and active with the Nice/CPUWeight drop-in, port 4533 answers on localhost and the Tailscale IP, GNOME automatic suspend off on AC and on for battery, logind lid drop-in applied and sleep targets NOT masked, ufw rules as in the checklist, unattended-upgrades active without automatic reboot, battery threshold set, backup timer listed and its last run succeeded. Report anything that is off.`
  - Done when: everything in the report is green.

Example prompts for phase 1:

- `Write ~/ops/10-navidrome.sh that installs ffmpeg, the latest Navidrome .deb from GitHub, adds navidrome to the music group, writes /etc/navidrome/navidrome.toml and the low-priority drop-in from step 1.3. Check the .deb's file list first so the config path is right. Show me the script, I will run it with sudo.`
- `Write ~/ops/20-power-firewall.sh with the logind drop-in from 1.5, the battery threshold oneshot from 1.6 option 3 (only if the sysfs file exists) and the ufw rules from 1.8. Don't mask any sleep targets and don't touch power-profiles-daemon. I will run it with sudo.`
- `Write the backup script, service and timer from step 1.11 into ~/ops/30-backup/ with an install script. After I run it, trigger one backup with systemctl start and show me the journal.`

## Phase 2: first friends (coarser)

- [ ] **2.1 Navidrome account per friend** (You)
  - Create a normal (non-admin) user per friend in the Navidrome web UI. Send the password privately.
  - Done when: the friend's user can log in.
- [ ] **2.2 Share the laptop over Tailscale** (You)
  - Tailscale admin console, machine `musicbox` → Share → send the invite link. Friend installs Tailscale and accepts.
  - Optional: an ACL that lets shared users reach only port 4533 (Claude can write the ACL JSON for you to paste).
  - Done when: the friend's phone shows `musicbox` in Tailscale.
- [ ] **2.3 Friend's phone app** (Friend)
  - Same Subsonic app setup as 1.10 with the shared address.
  - Done when: the friend plays a song on mobile data.
- [ ] **2.4 Shared uploads, simple version** (You + Claude)
  - Syncthing (`apt install syncthing`, run as a user service) syncs each friend's folder into `/srv/music/_incoming/<user>`; Claude writes a script plus systemd timer that checks and moves files into `library/<user>/Artist/Album`.
  - Done when: a file a friend drops in appears in Navidrome after the next scan.

Example prompt: `Write a bash script that takes files from /srv/music/_incoming/<user>, checks them with ffprobe, sorts them into /srv/music/library/<user>/Artist/Album by tags, and moves broken files to _rejected. Dry-run mode first, then a systemd timer every 15 minutes.`

## Phase 3: buddymusic API and relay (coarser)

- [ ] **3.1 Companion API** (Claude)
  - Go + SQLite service in this repo: invites, login, roles, profiles, uploads with dedupe, Navidrome user creation and `startScan`. Runs as a systemd service (`buddymusic-api.service`, user `buddymusic` in group `music`) on `127.0.0.1:8080`, data in `/var/lib/buddymusic`.
  - Prompt: `Following docs/concepts/self-hosted-streaming.md, scaffold the buddymusic API in Go with SQLite in api/: invites, login against Navidrome, roles, profiles, resumable uploads to /srv/music/_incoming with SHA-256 dedupe. Add tests, a systemd unit in deploy/ and a README section on installing it on Ubuntu.`
  - Done when: tests pass and `curl localhost:8080/api/health` answers.
- [ ] **3.2 Caddy on the laptop** (Claude writes, You run with sudo)
  - `sudo apt install -y caddy` (Ubuntu package, or Caddy's own apt repo for a newer version **(check)**). `/etc/caddy/Caddyfile` routes `/api/*` to `127.0.0.1:8080` and `/rest/*` to `127.0.0.1:4533`, bound to the Tailscale address only. Runs as the packaged `caddy.service`.
  - Done when: both routes answer through Caddy from another tailnet device.
- [ ] **3.3 Relay VPS** (You: buy VPS and domain, Claude: write configs)
  - You: rent a small VPS (~4 €/month) with Ubuntu 24.04, buy a domain, point `music.<domain>` at the VPS, install Tailscale there and log in.
  - Claude: Caddyfile for the VPS with automatic HTTPS and rate limiting, forwarding to `musicbox` over Tailscale; ufw allowing only 80/443 and SSH.
  - Done when: `https://music.<domain>/api/health` works from a phone without Tailscale.

## Phase 4: buddymusic app (coarser)

- [ ] **4.1 Flutter toolchain** (You install, Claude checks)
  - On the Windows PC, not the server: Flutter SDK, Android Studio, a phone in developer mode. iOS builds need a Mac and an Apple developer account (You).
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

- Whether GNOME overrides the logind lid setting while you are logged in (closing the lid on AC must not suspend), and whether this laptop has a firmware/vendor charge limit or `charge_control_end_threshold` in sysfs.
- What the Navidrome `.deb` installs (user, unit, config path) and the `TranscodingCacheSize` option; the Claude Code and Tailscale install lines against their current docs.
- The ufw home subnet (`192.168.0.0/16` is a guess) and that the desktop features you use (printer, casting, GSConnect) still work after `ufw enable`.
