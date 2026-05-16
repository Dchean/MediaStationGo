<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://img.shields.io/badge/MediaStationGo-Your_Private_Media_Center-111827?style=for-the-badge&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0id2hpdGUiIGQ9Ik0xMiAyQzYuNDggMiAyIDYuNDggMiAxMnM0LjQ4IDEwIDEwIDEwIDEwLTQuNDggMTAtMTBTMTcuNTIgMiAxMiAyem0tMiAxNWwtNS01IDEuNDEtMS40MUwxMCAxNC4xN2w3LjU5LTcuNTlMMTkgOGwtOSA5eiIvPjwvc3ZnPg=="/>
    <img src="https://img.shields.io/badge/MediaStationGo-Your_Private_Media_Center-1F2937?style=for-the-badge&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0id2hpdGUiIGQ9Ik0xMiAyQzYuNDggMiAyIDYuNDggMiAxMnM0LjQ4IDEwIDEwIDEwIDEwLTQuNDggMTAtMTBTMTcuNTIgMiAxMiAyem0tMiAxNWwtNS01IDEuNDEtMS40MUwxMCAxNC4xN2w3LjU5LTcuNTlMMTkgOGwtOSA5eiIvPjwvc3ZnPg=="/>
  </picture>
</p>

<h3 align="center"><samp>A Go rewrite of <a href="https://github.com/ShukeBta/MediaStation">MediaStation</a></samp></h3>
<h6 align="center"><samp>Lightweight · Fast · Single Binary · NAS Ready</samp></h6>

<p align="center">
  <a href="README.md"><img src="https://img.shields.io/badge/中文-README-blue?style=flat-square" alt="Chinese"></a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/React-18-61DAFB?style=for-the-badge&logo=react&logoColor=black" alt="React">
  <img src="https://img.shields.io/badge/TypeScript-5-3178C6?style=for-the-badge&logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/SQLite-WAL-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite">
  <img src="https://img.shields.io/badge/Docker-Alpine-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker">
  <img src="https://img.shields.io/badge/License-GPLv3-8B5CF6?style=for-the-badge&logo=gnu&logoColor=white" alt="GPL v3">
</p>

---

<p align="center">
  <b>📖 <a href="#-why-mediastationgo">Why</a></b>
  &nbsp;·&nbsp;
  <b>🚀 <a href="#-quick-start">Quick Start</a></b>
  &nbsp;·&nbsp;
  <b>✨ <a href="#-features">Features</a></b>
  &nbsp;·&nbsp;
  <b>🏗️ <a href="#-project-layout">Layout</a></b>
  &nbsp;·&nbsp;
  <b>⚙️ <a href="#-configuration">Config</a></b>
  &nbsp;·&nbsp;
  <b>🗺️ <a href="#-roadmap">Roadmap</a></b>
</p>

---

## 🤔 Why MediaStationGo?

> MediaStationGo is a from-scratch Go rewrite of [MediaStation](https://github.com/ShukeBta/MediaStation) — same full-featured media center experience, radically simpler deployment.

<table>
<tr>
<td width="50%">

### Original MediaStation
- 🐍 Python / FastAPI + Vue
- 📦 Requires Python runtime & virtualenv
- 🐳 Docker mandatory or complex Python setup
- 📊 Deployment footprint > 500 MB
- 🔧 pip + npm dual build chains

</td>
<td width="50%">

### MediaStationGo ✨
- 🚀 Go 1.25 + React 18
- 📦 **Single static binary** (~30 MB)
- 🐳 Docker optional — runs natively bare-metal
- 🔥 Zero external dependencies (CGO off)
- ⚡ One-command build: `go build`

</td>
</tr>
</table>

| Metric | Original | MediaStationGo |
|--------|:---:|:---:|
| Binary size | — | ≈ 30 MB |
| Memory (idle) | ~200 MB | ~30 MB |
| Cold start | ~3s | ~0.3s |
| Deploy steps | 5+ | 1 |
| Frontend (gzip) | ~250 KB | ~83 KB |

---

## ✨ Features

<details open>
<summary><b>🔐 Authentication & Users</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| JWT dual-role auth | admin / user with refresh token support |
| One-click admin bootstrap | Auto-seeded `admin` / `admin123` on first run |
| Profile management | Email, avatar, password change |
| User admin panel | Role promotion / demotion, enable / disable |
| Audit log | Login, library ops, downloads — all tracked |

</details>

<details open>
<summary><b>📚 Library Management</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| Library CRUD | movie / tv / anime / music types supported |
| Recursive scanning | Filesystem walk + ffprobe metadata extraction |
| Smart filename parsing | Year + season/episode auto-detection |
| Multi-source scraping | TMDb → TheTVDB → Bangumi chain |
| HD poster upgrade | Optional Fanart.tv high-res fallback |
| Image proxy | TMDb / Bangumi / Douban / Fanart with disk cache |
| TV grouping | Season grouping with episode listing |
| Live fs watching | fsnotify-powered, 5-second coalesced debounce |

</details>

<details open>
<summary><b>🎬 Playback</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| Direct streaming | HTTP Range support for instant seeking |
| HLS on-demand | Per-media ffmpeg job with HW acceleration |
| External subtitles | .srt / .vtt / .ass / .ssa → real-time WebVTT |
| Resume playback | Auto-saved every 10s + Continue Watching on home |
| Favorites & playlists | One-tap toggle / ordered playlists (CRUD) |

</details>

<details open>
<summary><b>🌐 PT Site Management</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| 6 site types | nexusphp · gazelle · unit3d · mteam · discuz · custom_rss |
| 3 auth methods | Cookie / API Key / Auth Header |
| Site config | Full CRUD + connection test + enable toggle |
| Cross-site search | One-click search across all configured trackers |
| Extended config | Extra JSON: UA / RSS URL / timeout / priority / proxy / downloader |

</details>

<details open>
<summary><b>🤖 Automation</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| Download client | qBittorrent Web UI API (add / list / delete) |
| RSS subscriptions | Regex filter + GUID dedup + 10-min polling |
| File organizer | Auto-categorize downloads: move / copy / hardlink / symlink |

</details>

<details open>
<summary><b>📊 Operations & Monitoring</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| Live events | WebSocket push for scan / scrape / transcode / download / subscribe |
| Dashboard | CPU / Memory / Disk / Library counts / Goroutines |
| Task panel | Real-time ffmpeg jobs + qBittorrent torrents |
| NFO export | Kodi / Jellyfin compatible — single media or whole library |
| HW acceleration | Software / NVENC / Intel QSV / VAAPI encoder profiles |
| CI/CD | GitHub Actions + multi-arch GHCR release |

</details>

<details open>
<summary><b>🧠 AI & Discovery</b></summary>
<br>

| Feature | Description |
|---------|-------------|
| TMDb Discover | Trending / popular rails on homepage |
| AI smart search | OpenAI-compatible → natural language → structured query |
| AI recommendations | Personalized picks from your watch history |

</details>

---

## 🚀 Quick Start

### 🐳 Docker (Recommended)

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# Edit docker-compose.yml to mount your media at /media
docker compose up -d
```

> 🌐 The server auto-detects both your **local network IP** and **public IP** on startup (NAS / VPS supported):
> ```json
> {"msg":"server is ready","local":"http://192.168.1.4:8080","public":"http://1.2.3.4:8080"}
> ```
> Log in with `admin` / `admin123`.

### 💻 Bare Metal

| Prerequisite | Version |
|-------------|---------|
| Go | ≥ 1.25 |
| Node.js | ≥ 20 |
| FFmpeg | Any |

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# Build backend + frontend
make build

# Start the server
./bin/mediastation-go
```

### 🛠️ Local Development

```bash
# Terminal 1: Go backend (port 8080, DEBUG mode)
make dev

# Terminal 2: Vite frontend (port 3000, proxies API calls)
make dev-web
```

---

## ⚙️ Configuration

Config precedence: **defaults** < `config.yaml` < `config/*.yaml` < **env vars** (prefix `MEDIASTATION_`)

### Key Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MEDIASTATION_APP_PORT` | `8080` | HTTP listen port |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | Data root (DB / cache / JWT) |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | SPA bundle directory |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite file path |
| `MEDIASTATION_SECRETS_JWT_SECRET` | *(auto)* | JWT signing key |
| `MEDIASTATION_SECRETS_TMDB_API_KEY` | *(empty)* | TMDb scraping (required) |
| `MEDIASTATION_SECRETS_BANGUMI_ACCESS_TOKEN` | *(empty)* | Bangumi rate limit boost |
| `MEDIASTATION_APP_CORS_ORIGINS` | *(empty)* | Allow-list (JSON array) |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | Initial admin password |

### Runtime Settings

Admin panel → Settings tab, stored in the `settings` table:

| Key | Purpose |
|-----|---------|
| `qbittorrent.url` | qBittorrent Web UI URL |
| `qbittorrent.username` | Username |
| `qbittorrent.password` | Password |
| `qbittorrent.savepath` | Default save path (optional) |

> 💡 After editing, hit **Downloads → Reload Config** or `POST /api/downloads/reload`.

📖 Full config template: [`config.example.yaml`](config.example.yaml)

---

## 🏗️ Project Layout

```
MediaStationGo/
├── cmd/server/main.go          ← Entry point
├── internal/
│   ├── config/                 ← Viper configuration layer
│   ├── database/               ← GORM + SQLite (WAL) bootstrap
│   ├── model/                  ← Data models + AutoMigrate registry
│   ├── repository/             ← Data access layer
│   ├── service/                ← Business logic (core)
│   │   ├── auth.go             login / register / JWT
│   │   ├── media.go            library + media CRUD
│   │   ├── scanner.go          filesystem walker + ffprobe
│   │   ├── scraper.go          scrape orchestrator + filename cleaner
│   │   ├── tmdb.go / bangumi.go  third-party providers
│   │   ├── site.go             site CRUD + cross-site search
│   │   ├── site_adapter.go     6 PT site adapters
│   │   ├── stream.go           direct play + HLS
│   │   ├── transcoder.go       per-media ffmpeg job manager
│   │   ├── subtitle.go         external subs → WebVTT
│   │   ├── image_proxy.go      cached image proxy
│   │   ├── playback.go         history / favorites / playlists
│   │   ├── watcher.go          fsnotify debouncer
│   │   ├── qbittorrent.go      qBittorrent v2 API client
│   │   ├── downloads.go        download orchestrator
│   │   ├── subscription.go     RSS poller
│   │   ├── organizer.go        media file organizer
│   │   ├── stats.go            dashboard snapshot
│   │   ├── profile.go          user profile mutations
│   │   ├── audit.go            audit log writer
│   │   └── ws_hub.go           WebSocket pub/sub broker
│   ├── middleware/             ← JWT / CORS / admin middleware
│   └── handler/                ← HTTP route definitions (by concern)
├── web/                        ← React 18 + Vite + Tailwind CSS
│   ├── src/api/                axios service wrappers
│   ├── src/components/         Card / Layout / APIConfigsPanel / etc.
│   ├── src/hooks/              useWebSocket & friends
│   ├── src/pages/              Home · Library · Search · Player · Downloads · Admin · Sites
│   ├── src/stores/             Zustand (auth)
│   └── src/types/              TypeScript domain types
├── Dockerfile                  ← Multi-stage, multi-arch build
├── docker-compose.yml          ← NAS-friendly one-command deploy
├── Makefile                    ← build / dev / docker / test
├── config.example.yaml         ← Full configuration reference
└── .github/workflows/          ← CI + GHCR publish
```

---

## 🗺️ Roadmap

| Feature | Status |
|---------|:---:|
| Jellyfin / Emby compatibility layer | 🔨 In Progress |
| DLNA / Chromecast casting | 📋 Planned |
| Online subtitle search providers | 📋 Planned |
| Multi-bitrate ABR transcode | 📋 Planned |
| STRM direct streaming (WebDAV / Alist / S3) | ✅ Complete |

---

## 🤝 Contributing

Issues and PRs are welcome! Please read the [Contribution Guidelines](CONTRIBUTING.md) before submitting.

---

## 📄 License

[GNU General Public License v3.0](LICENSE)

> ⚠️ License activation/validation is handled by a separate server: [MediaStationLicenseServer](https://github.com/ShukeBta/MediaStationLicenseServer). This project contains no license generation or validation logic.

---

<p align="center">
  <sub>Made with ❤️ by MediaStationGo Team</sub>
</p>
