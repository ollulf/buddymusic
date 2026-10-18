# buddymusic setup checklist (Ubuntu laptop)

Step by step, from phase 1 (just me) to the later phases. The server laptop runs **Ubuntu Server 24.04 LTS** and is managed over SSH from the Windows PC.

Why Server and not Desktop: no GNOME session that suspends the machine or holds RAM, fewer packages to update, and everything here is a systemd service anyway. The laptop screen is only needed during install. If you want a GUI for tagging with Picard on the laptop itself, Ubuntu Desktop 24.04 works with the same steps, but then also turn off automatic suspend in GNOME settings.

Each step says who does it:

- **You**: things only you can do, such as installing Ubuntu, BIOS settings, account logins, typing your `sudo` password, phones, inviting friends.
- **Claude**: things Claude Code can do on the laptop, such as writing config files, systemd units and scripts, running non-root commands and checking the services. You still approve each command.

Claude Code runs as your normal user and cannot answer `sudo` password prompts. For root steps, Claude writes a script (e.g. `~/ops/10-navidrome.sh`) and shows it to you, and you run it with `sudo bash ~/ops/10-navidrome.sh`. If you prefer, run `sudo -v` in the same terminal before a session so a short sudo cache covers Claude's commands, but the script route is easier to review.

None of the commands below have been tested on the real machine. Package names, paths and the Navidrome `.deb` layout are marked **(check)** where they need confirming first.

Concept: [concepts/self-hosted-streaming.md](concepts/self-hosted-streaming.md)

## Phase 0: Ubuntu and Claude Code on the laptop

- [ ] **0.1 Install Ubuntu Server 24.04 LTS** (You)
  - Download the Ubuntu Server 24.04 LTS ISO on the Windows PC and write it to a USB stick (4 GB+) with Rufus or balenaEtcher.
  - BIOS: boot from USB, Secure Boot can stay on. Set "power on after AC loss" if the BIOS has it.
  - Installer: **wipe the whole disk** (recommended for an always-on server). Dual-boot with Windows works but every Windows update or accidental boot into Windows takes the server offline, so avoid it. Back up anything you still need from the old Windows install first.
  - Pick a hostname (`musicbox`), your user (e.g. `max`), tick **Install OpenSSH server**, skip the extra snaps.
  - If the music goes on a second drive: leave it unformatted in the installer, it is set up in 1.1.
  - Done when: the laptop boots into Ubuntu and you can log in on its console.
- [ ] **0.2 Base system and SSH from the Windows PC** (You)
  - On the laptop: `sudo apt update && sudo apt full-upgrade -y`, then `ip -4 addr` to see its home IP.
  - On the Windows PC (PowerShell): `ssh-keygen -t ed25519` if you have no key yet, then `type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh max@<laptop-ip> "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys"`.
  - Then disable password logins: `/etc/ssh/sshd_config.d/10-keys-only.conf` with `PasswordAuthentication no`, then `sudo systemctl restart ssh`.
  - Done when: `ssh max@<laptop-ip>` from Windows logs in without a password.
- [ ] **0.3 Install Git and Claude Code on the laptop** (You)
  - `sudo apt install -y git tmux curl`
  - Claude Code native installer: `curl -fsSL https://claude.ai/install.sh | bash` **(check against the current Claude Code docs)**
  - `git clone <your buddymusic remote> ~/buddymusic`, `cd ~/buddymusic`, `claude`. Log in: the login link can be opened in the browser on the Windows PC.
  - Run Claude inside `tmux` (`tmux new -s claude`) so a dropped SSH connection doesn't stop it.
  - Done when: `claude --version` prints a version and Claude can read `docs/setup-checklist.md`.
  - Alternative: keep Claude Code on the Windows PC and let it run commands over `ssh musicbox ...`. That works, but Claude then edits files through SSH one command at a time, so running it on the laptop is simpler.
- [ ] **0.4 Let Claude check the machine** (Claude)
  - Prompt: `Read docs/setup-checklist.md. Check this Ubuntu laptop for phase 1: Ubuntu version, disks and free space (lsblk, df -h), whether ffmpeg, navidrome, tailscale, ufw, tlp and unattended-upgrades are installed, the logind lid settings and the battery charge threshold files under /sys/class/power_supply. Don't change anything yet, just report.`
  - Done when: you have a short report of what is missing.

## Phase 1: just me

- [ ] **1.1 Music drive, folders and permissions** (You run, Claude writes)
  - Second drive (if any): `sudo mkfs.ext4 -L music /dev/sdX1` **(check device with lsblk, this erases it)**, mount it at `/srv/music` via `/etc/fstab` using its UUID (`UUID=... /srv/music ext4 defaults,nofail 0 2`).
  - Folders and a shared group:
    ```bash
    sudo groupadd --system music
    sudo usermod -aG music max
    sudo mkdir -p /srv/music/{library,_incoming,_trash}
    sudo chown -R max:music /srv/music
    sudo chmod -R 2775 /srv/music
    ```
  - Copy music from the Windows PC: `scp -r` or better `rsync -av --progress` from WSL, or a temporary Samba share. Target `/srv/music/library/Artist/Album`.
  - Claude: count files and formats, list files without tags or with broken names (`ffprobe` loop).
  - Done when: everything is under `/srv/music/library` and the report shows no big gaps.
- [ ] **1.2 Install ffmpeg and Navidrome** (Claude writes, You run with sudo)
  - `sudo apt install -y ffmpeg`
  - Navidrome: official `.deb` from the GitHub releases page (`navidrome_<version>_linux_amd64.deb`), then `sudo apt install ./navidrome_*_linux_amd64.deb`. It creates a `navidrome` system user and a `navidrome.service` systemd unit **(check against the Navidrome install docs for the current release)**.
  - `sudo usermod -aG music navidrome` so it can read the library and later the upload folders.
  - Alternative: Docker Compose with the `deluan/navidrome` image and `/srv/music/library` mounted read-only. Fine too, but the `.deb` avoids running Docker on a small laptop and keeps everything in systemd.
  - Done when: `ffmpeg -version` works and `systemctl status navidrome` shows the unit.
- [ ] **1.3 Configure Navidrome** (Claude writes, You install with sudo)
  - `/etc/navidrome/navidrome.toml` **(check path used by the .deb)**:
    ```toml
    MusicFolder = "/srv/music/library"
    DataFolder = "/var/lib/navidrome"
    Address = "0.0.0.0"
    Port = 4533
    ScanSchedule = "@every 1h"
    EnableTranscodingConfig = true
    LogLevel = "info"
    ```
  - `sudo systemctl enable --now navidrome && sudo systemctl restart navidrome`
  - Done when: `curl -sI http://localhost:4533` answers and `journalctl -u navidrome -n 50` shows the scan starting.
- [ ] **1.4 Fallback: own systemd unit** (Claude, only if you used the plain binary tarball instead of the `.deb`)
  - Binary in `/opt/navidrome/navidrome`, system user `navidrome` (`useradd --system --home /var/lib/navidrome --groups music navidrome`), and `/etc/systemd/system/navidrome.service`:
    ```ini
    [Unit]
    Description=Navidrome
    After=network-online.target
    Wants=network-online.target

    [Service]
    User=navidrome
    Group=navidrome
    SupplementaryGroups=music
    ExecStart=/opt/navidrome/navidrome --configfile /etc/navidrome/navidrome.toml
    WorkingDirectory=/var/lib/navidrome
    Restart=on-failure
    ProtectSystem=full
    ReadWritePaths=/var/lib/navidrome

    [Install]
    WantedBy=multi-user.target
    ```
  - `sudo systemctl daemon-reload && sudo systemctl enable --now navidrome`
  - Done when: `systemctl is-enabled navidrome` says `enabled` and `is-active` says `active`.
- [ ] **1.5 Create the admin account** (You)
  - From the Windows PC open `http://<laptop-ip>:4533` (or `http://musicbox:4533` after 1.9), create the admin user, store the password in your password manager. Wait for the first scan.
  - Done when: albums show up in the web UI and a song plays in the browser.
- [ ] **1.6 Keep the laptop awake with the lid closed** (Claude writes, You run with sudo)
  - `/etc/systemd/logind.conf.d/10-lid.conf`:
    ```ini
    [Login]
    HandleLidSwitch=ignore
    HandleLidSwitchExternalPower=ignore
    HandleLidSwitchDocked=ignore
    ```
  - Then `sudo systemctl restart systemd-logind` and `sudo systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target`.
  - Screen off after a minute on the console: kernel parameter `consoleblank=60` in `/etc/default/grub` (`GRUB_CMDLINE_LINUX_DEFAULT`), then `sudo update-grub`.
  - Done when: `systemctl status sleep.target` shows `masked`, and the laptop keeps serving music with the lid closed for an hour.
- [ ] **1.7 Battery charge limit** (You + Claude)
  - Claude checks whether `/sys/class/power_supply/BAT0/charge_control_end_threshold` exists (ThinkPads, many ASUS, Dell and newer laptops).
  - If yes: `sudo apt install -y tlp`, then `/etc/tlp.d/10-battery.conf` with `START_CHARGE_THRESH_BAT0=60` and `STOP_CHARGE_THRESH_BAT0=80`, `sudo systemctl enable --now tlp`. TLP only touches power settings, it doesn't suspend the machine.
  - If no: set the limit in the BIOS (many vendors have "battery care" there). Without either, accept the battery wear or run it without battery if the laptop allows.
  - Done when: `cat /sys/class/power_supply/BAT0/charge_control_end_threshold` prints 80 (or the BIOS shows the limit) and the battery stops charging there.
- [ ] **1.8 Tailscale on the laptop** (You)
  - `curl -fsSL https://tailscale.com/install.sh | sh`, then `sudo tailscale up --ssh` and open the printed login link on the Windows PC.
  - `--ssh` turns on Tailscale SSH, so you can also reach the laptop from anywhere with `ssh max@musicbox` (allowed by your tailnet ACL). Optional.
  - In the admin console: name the machine `musicbox`, **disable key expiry** for it and turn on MagicDNS. Install Tailscale on the Windows PC as well.
  - Done when: `tailscale status` lists the laptop and `tailscale ip -4` prints a `100.x` address.
- [ ] **1.9 Firewall with ufw** (Claude writes, You run with sudo)
  - Allow only Tailscale and SSH from the home network, never the internet. No router port forwarding.
    ```bash
    sudo ufw default deny incoming
    sudo ufw default allow outgoing
    sudo ufw allow in on tailscale0
    sudo ufw allow from 192.168.0.0/16 to any port 22 proto tcp
    sudo ufw enable
    ```
  - Optional for home Wi-Fi without Tailscale: `sudo ufw allow from 192.168.0.0/16 to any port 4533 proto tcp`. Adjust the subnet to your router **(check)**.
  - Run this while connected over SSH only after the SSH rule is in, or do it on the laptop console.
  - Done when: `sudo ufw status verbose` shows the rules, the phone reaches port 4533 over Tailscale, and the router has no forwarding rule.
- [ ] **1.10 Automatic security updates** (Claude writes, You run with sudo)
  - `sudo apt install -y unattended-upgrades` (usually preinstalled), `sudo dpkg-reconfigure -plow unattended-upgrades`.
  - `/etc/apt/apt.conf.d/52buddymusic`: `Unattended-Upgrade::Automatic-Reboot "true";` and `Unattended-Upgrade::Automatic-Reboot-Time "04:00";`.
  - Done when: `systemctl status unattended-upgrades` is active and `/var/log/unattended-upgrades/` gets a log after a day.
- [ ] **1.11 Phone setup** (You)
  - Install Tailscale on the phone and log in with the same account.
  - Install a Subsonic app: Symfonium or Tempo (Android), Amperfy, play:Sub or substreamer (iOS).
  - Server: `http://musicbox:4533` (or the `100.x` address), your Navidrome user.
  - Streaming: Opus 128 kbps on mobile data, original on Wi-Fi. Download a few playlists for offline.
  - Done when: music plays with Wi-Fi off on mobile data.
- [ ] **1.12 Backups** (Claude writes, You run with sudo and plug in the USB drive)
  - USB drive formatted ext4 with label `backup`, mounted at `/mnt/backup` via fstab with `nofail`.
  - `/usr/local/bin/buddymusic-backup`:
    ```bash
    #!/bin/bash
    set -euo pipefail
    mountpoint -q /mnt/backup
    rsync -a --delete /srv/music/ /mnt/backup/music/
    systemctl stop navidrome
    rsync -a --delete /var/lib/navidrome/ /mnt/backup/navidrome/
    systemctl start navidrome
    ```
  - `/etc/systemd/system/buddymusic-backup.service` (`Type=oneshot`, `ExecStart=/usr/local/bin/buddymusic-backup`) and `buddymusic-backup.timer` (`OnCalendar=*-*-* 03:00`, `Persistent=true`), then `sudo systemctl enable --now buddymusic-backup.timer`.
  - Off-site (optional): `sudo apt install -y restic`, `restic -r b2:<bucket>:buddymusic init`, then a second service + timer running `restic backup /srv/music /var/lib/navidrome`. You create the B2 account and keep the restic password safe (e.g. in `/root/.restic-env`, mode 600).
  - Done when: `systemctl list-timers` shows the timer, `journalctl -u buddymusic-backup` has a clean run, and you restored one file as a test.
- [ ] **1.13 Final check** (Claude)
  - Prompt: `Verify phase 1 on this Ubuntu laptop: navidrome enabled and active, port 4533 answers on localhost and the Tailscale IP, sleep targets masked and logind lid settings applied, ufw rules as in the checklist, unattended-upgrades active, battery threshold set, backup timer listed and its last run succeeded. Report anything that is off.`
  - Done when: everything in the report is green.

Example prompts for phase 1:

- `Write ~/ops/10-navidrome.sh that installs ffmpeg, the latest Navidrome .deb from GitHub, adds navidrome to the music group and writes /etc/navidrome/navidrome.toml as in step 1.3. Check the .deb's file list first so the config path is right. Show me the script, I will run it with sudo.`
- `Write ~/ops/20-power-firewall.sh with the logind drop-in, sleep target masking, TLP thresholds and ufw rules from steps 1.6, 1.7 and 1.9. Make sure the SSH rule comes before ufw enable. I will run it with sudo.`
- `Write the backup script, service and timer from step 1.12 into ~/ops/30-backup/ with an install script. After I run it, trigger one backup with systemctl start and show me the journal.`

## Phase 2: first friends (coarser)

- [ ] **2.1 Navidrome account per friend** (You)
  - Create a normal (non-admin) user per friend in the Navidrome web UI. Send the password privately.
  - Done when: the friend's user can log in.
- [ ] **2.2 Share the laptop over Tailscale** (You)
  - Tailscale admin console, machine `musicbox` → Share → send the invite link. Friend installs Tailscale and accepts.
  - Optional: an ACL that lets shared users reach only port 4533 (Claude can write the ACL JSON for you to paste).
  - Done when: the friend's phone shows `musicbox` in Tailscale.
- [ ] **2.3 Friend's phone app** (Friend)
  - Same Subsonic app setup as 1.11 with the shared address.
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

- The Navidrome `.deb`: whether it really creates the `navidrome` user and `navidrome.service`, and whether it reads `/etc/navidrome/navidrome.toml` and uses `/var/lib/navidrome` (`dpkg -c` on the file shows this). Also the Claude Code and Tailscale install commands against their current docs.
- That this laptop has `charge_control_end_threshold` in sysfs (otherwise BIOS only), and that the lid setting really keeps it running with the lid closed on this model.
- The ufw home subnet (`192.168.0.0/16` is a guess) and that SSH stays reachable after `ufw enable`; the backup stopping Navidrome for a few seconds at 03:00.
