# MediaStationGo

<p align="center">
  <img src="web/public/favicon.svg" width="96" height="96" alt="MediaStationGo Logo" />
</p>

<h3 align="center">A lightweight, polished, NAS-friendly private media center</h3>

<p align="center">
  <strong>Docker-first setup · Multi-user management · Media library · Metadata · Downloads · Emby-protocol clients · Cloud playback</strong>
</p>

<p align="center">
  <a href="README.md">中文</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#docker-compose-recommended">Docker Compose</a> ·
  <a href="#faq">FAQ</a> ·
  <a href="https://mgo.3jzs.com">Live Demo</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111827" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square" />
</p>

---

## What is it?

MediaStationGo is a self-hosted media center for personal libraries, home NAS, and home-theater users.

It helps you:

- Manage movies, TV shows, anime, variety shows, music, and adult libraries.
- Create multiple user accounts for family members, friends, or different devices.
- Scan files and enrich posters, summaries, years, seasons, and episodes.
- Play in the web UI, or log in with a MediaStationGo account from Emby-protocol apps such as Infuse, VidHub, SenPlayer, and Emby clients.
- Connect qBittorrent for search, subscriptions, downloads, and post-download organization.
- Connect OpenList, CloudDrive2, WebDAV, and other storage backends with STRMURL or 302 redirect playback.
- Run on NAS, mini PCs, VPS, Linux, Windows Docker Desktop, or any Docker-friendly host.

> The project is moving fast. Keep a backup of the `data` directory before upgrades.

---

## Key Highlights

- **One server, many clients**: deploy MediaStationGo once; you do not need to run a separate Emby server.
- **Emby-protocol compatibility**: add the server in third-party players as an Emby/Jellyfin-compatible server, then log in with your MediaStationGo username and password.
- **Multi-user management**: supports admins, regular users, account enable/disable, expiry dates, device management, Bot registration, and redeem codes.
- **Local + cloud media in one place**: manage local disks, download folders, OpenList, CloudDrive2, WebDAV, and other storage backends from one panel.
- **Download-to-library workflow**: connect qBittorrent for search, subscriptions, download completion organization, and metadata matching.
- **NAS-friendly**: simple Docker Compose deployment, important data stored under `data/`, suitable for low-power NAS and mini PCs.

---

## Who is it for?

- **Beginners** who want to edit one `docker-compose.yml` and start the service.
- **NAS users** who want a low-resource media center for local disks and cloud storage.
- **PT/download users** who want downloads, organization, metadata, and playback in one panel.
- **External-player users** who want to log in to Emby-protocol third-party apps with one MediaStationGo account.
- **Family-sharing users** who want separate user accounts without deploying a separate media server for each person.
- **Developers** who want to study or extend a Go + React self-hosted media app.

---

## Live Demo

- URL: [https://mgo.3jzs.com](https://mgo.3jzs.com)
- Username: `admin`
- Password: `admin123`

> The demo is for feature preview only. Do not save private API keys, tracker cookies, or personal data there.

---

## Quick Start

Docker Compose is the recommended path. Beginners do not need `.env`, bare-metal binaries, or source builds.

```bash
mkdir -p MediaStationGo
cd MediaStationGo
curl -fsSL https://raw.githubusercontent.com/ShukeBta/MediaStationGo/main/docker-compose.yml -o docker-compose.yml
```

Edit `docker-compose.yml`:

```bash
vi docker-compose.yml
```

Start:

```bash
docker compose up -d
```

Open:

```text
http://SERVER_IP:18080
```

Default login:

```text
Username: admin
Password: admin123
```

---

## Docker Compose Recommended

The repository `docker-compose.yml` is intentionally simple and does not require `.env`.

### Choose an image source

Both image sources are supported. Pick one and put it in `image:`:

| Source | Image | Best for |
| --- | --- | --- |
| GitHub Container Registry (GHCR) | `ghcr.io/shukebta/mediastation-go:latest` | Recommended default, follows repository releases |
| Docker Hub | `shukbet/mediastationgo:latest` | Backup source when GHCR is slow or unavailable |

To pin a version, first confirm the tag exists on the repository Packages page. Use this format:

```yaml
image: ghcr.io/shukebta/mediastation-go:<version-tag>
# If GHCR does not have that tag, use Docker Hub as the backup:
# image: shukbet/mediastationgo:MediaStationGo-v0.0.72
```

For the simplest setup, keep GHCR `latest`.

Manual pull examples:

```bash
# GitHub Container Registry
docker pull ghcr.io/shukebta/mediastation-go:latest

# Docker Hub backup
docker pull shukbet/mediastationgo:latest
```

Focus on this part:

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - ./media:/media
  - ./downloads:/downloads
```

Meaning:

| Host path | Container path | Purpose |
| --- | --- | --- |
| `./data` | `/data` | Database, users, settings; back this up |
| `./cache` | `/cache` | Cache; safe to clean when needed |
| `./media` | `/media` | Media libraries; use `/media/...` in the web UI |
| `./downloads` | `/downloads` | Download directory and organization source |

If your NAS paths are:

```text
/vol1/1000/Media
/vol1/1000/Downloads
```

change the compose file to:

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - /vol1/1000/Media:/media
  - /vol1/1000/Downloads:/downloads

environment:
  MEDIASTATION_MEDIA_DIR: /vol1/1000/Media
  MEDIASTATION_DOWNLOAD_DIR: /vol1/1000/Downloads
```

Rules:

- The left side of `volumes` is the real path on your host/NAS.
- The right side is the container path. Keep `/media` and `/downloads` unless you know why you are changing them.
- In the web UI, create libraries with container paths such as `/media/Movies` or `/media/TV`.
- Do not write NAS absolute paths as `./vol1/...`; `./` means a folder under the current compose directory.
- On Windows Docker Desktop, paths like `D:/Media:/media` and `D:/Downloads:/downloads` are fine.
- If you only scan/play existing media and never organize into the library, you may add `:ro`; if you use organize/rename/ingest, the media mount must stay writable.

### Minimal compose example

The root `docker-compose.yml` follows this style:

```yaml
services:
  mediastation-go:
    # Pick one image source:
    # GitHub Container Registry (GHCR):
    image: ghcr.io/shukebta/mediastation-go:latest
    # Docker Hub backup:
    # image: shukbet/mediastationgo:latest

    container_name: mediastation-go
    restart: unless-stopped
    init: true

    # Browser: http://SERVER_IP:18080
    ports:
      - "18080:8080"

    # Let the container reach qBittorrent running on the host:
    # qB URL example: http://host.docker.internal:8085
    extra_hosts:
      - "host.docker.internal:host-gateway"

    volumes:
      # Application data. Back this up before upgrades.
      - ./data:/data
      - ./cache:/cache

      # Beginners can keep ./media and ./downloads.
      # NAS users should replace the left side with real absolute paths.
      - ./media:/media
      - ./downloads:/downloads

    environment:
      TZ: Asia/Shanghai
      PUID: "1000"
      PGID: "1000"

      MEDIASTATION_APP_HOST: 0.0.0.0
      MEDIASTATION_APP_PORT: 8080
      MEDIASTATION_APP_WEB_DIR: /app/web/dist
      MEDIASTATION_APP_DATA_DIR: /data
      MEDIASTATION_DATABASE_DB_PATH: /data/mediastation.db
      MEDIASTATION_CACHE_CACHE_DIR: /cache

      # If you changed ./media or ./downloads above,
      # set these to the same real host paths.
      MEDIASTATION_MEDIA_DIR: ./media
      MEDIASTATION_MEDIA_CONTAINER_DIR: /media
      MEDIASTATION_DOWNLOAD_DIR: ./downloads
      MEDIASTATION_DOWNLOAD_CONTAINER_DIR: /downloads
```

---

## First-time Setup

1. **Create a library**
   - Go to the library page.
   - Use a container path such as `/media/Movies`.
   - Start a scan.

2. **Connect qBittorrent**
   - Go to download client settings.
   - If qBittorrent runs on the host, try `http://host.docker.internal:8085`.

3. **Configure metadata providers**
   - Go to system settings / external APIs.
   - Add TMDb, Bangumi, TheTVDB, Fanart, Douban, or other providers when needed.

4. **Use external players**
   - Add the server as an Emby/Jellyfin-compatible server.
   - Server URL: `http://SERVER_IP:18080`.
   - Use the username and password created in MediaStationGo. No separate Emby server is required.
   - Admins can create regular users in the web UI or Bot so each person can log in with their own account.

5. **Use cloud playback**
   - Configure OpenList, CloudDrive2, WebDAV, or another provider in storage settings.
   - Choose STRMURL or 302 redirect playback in the admin settings.
   - The enabled option takes priority. If both are disabled, playback falls back to the normal server playback path.

---

## Update, Backup, Logs

### Update

```bash
docker compose pull
docker compose up -d
```

### Logs

```bash
docker logs -f mediastation-go
```

### Backup

Back up:

```text
data/
```

It contains the database, users, settings, and runtime state. `cache/` is usually not important.

### Stop

```bash
docker compose down
```

---

## FAQ

### 1. The web page does not open

Check the container:

```bash
docker ps
docker logs --tail=100 mediastation-go
```

Then open:

```text
http://SERVER_IP:18080
```

### 2. The library cannot find files

Most cases are path mistakes.

- Docker maps media to `/media`.
- In the web UI, use `/media/Movies`, not the original NAS path.
- Docker maps downloads to `/downloads`; use `/downloads` as the organization source when possible.

### 3. qBittorrent cannot connect

If qBittorrent is on the host, try:

```text
http://host.docker.internal:8085
```

If qBittorrent is on another machine, use that machine's LAN IP.

### 4. NAS CPU usage is high

Suggested settings:

- Set `ffprobe.max_concurrent` to `1`.
- Enable automatic organization, scrape-after-scan, and boot cloud scan only when you really need them.
- Avoid frequent full-library scans on large libraries. Prefer manual scan or scheduled night sync.

### 5. Should I use `.env`?

Beginners should not. Editing `docker-compose.yml` directly is easier to understand.

`.env` is useful only for advanced users who reuse the same compose file on multiple machines. The repository keeps `docker-compose.simple.env.example`, but it is not the main path.

---

## Features

| Area | Features |
| --- | --- |
| Libraries | Movies, TV shows, anime, variety, music, adult content |
| Metadata | NFO, local artwork, TMDb, TheTVDB, Bangumi, Douban, Fanart, JavBus/JavDB |
| Playback | Web playback, HTTP Range, HLS transcoding, direct links, STRMURL, 302 redirect |
| External clients | Emby-protocol compatible APIs; MediaStationGo accounts can log in to third-party players |
| User management | Multi-user accounts, admin/regular users, expiry dates, device management, Bot registration and redeem codes |
| Downloads | qBittorrent, site search, subscriptions, post-download organization |
| File manager | Browse, organize, copy, move, hardlink, symlink |
| Operations | Task queue, recycle bin, duplicate files, notifications, logs |
| AI | OpenAI-compatible API, AI search, recommendations, assistant |

---

## Screenshots

<details open>
<summary><strong>Preview</strong></summary>

| Login | Home |
| --- | --- |
| <img src="docs/screenshots/00-login.jpg" alt="Login" width="100%"> | <img src="docs/screenshots/01-home.jpg" alt="Home" width="100%"> |

| Libraries | Player |
| --- | --- |
| <img src="docs/screenshots/02-libraries.jpg" alt="Libraries" width="100%"> | <img src="docs/screenshots/06-player.jpg" alt="Player" width="100%"> |

</details>

---

## Development

Regular users should use Docker. Developers can run:

```bash
go run ./cmd/server
```

Frontend:

```bash
cd web
npm install
npm run dev
```

Tests:

```bash
go test ./...
cd web && npm run build
```

---

## Community and Friends

- Telegram group: <https://t.me/MediaStationGo>
- NodeSeek: [https://www.nodeseek.com/](https://www.nodeseek.com/)
- LINUX DO: [https://linux.do/](https://linux.do/)

---

## Donation

If MediaStationGo saves you time, feel free to buy the author a bowl of noodles.

<img width="200" height="200" alt="WeChat Donation QR" src="https://github.com/user-attachments/assets/d6077de5-8305-400d-8b82-470ef05d926e" />

---

## Star History

<a href="https://www.star-history.com/?repos=ShukeBta%2FMediaStationGo&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&legend=top-left" />
 </picture>
</a>

---

## License and Non-Commercial Statement

This project uses `GPL-3.0` as its base license. See [LICENSE](LICENSE).

The maintainers also state and request:

- The project is intended for personal learning, home NAS, self-hosted media, non-commercial research, and community collaboration.
- Without explicit written permission from the author, do not use this project or derivative versions for commercial resale, paid hosting, paid SaaS, pre-installed commercial devices, closed-source redistribution, or other profit-oriented commercial use.
- For commercial cooperation, enterprise deployment, custom development, integrated redistribution, or commercial authorization, contact the author first.
- If there is any interpretive difference between this README and the formal `GPL-3.0` license text, the code license is governed by [LICENSE](LICENSE); commercial usage should additionally obtain author permission.

---

<p align="center">Made with ❤️ by ShukeBta</p>
