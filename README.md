<h1 align="center">🎬 MediaStationGo</h1>
<p align="center">A Go rewrite of <a href="https://github.com/ShukeBta/MediaStation">MediaStation</a> — your private home media center.</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react" alt="React">
  <img src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript" alt="TypeScript">
  <img src="https://img.shields.io/badge/SQLite-WAL-003B57?style=flat-square&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/Docker-Alpine_3.19-2496ED?style=flat-square&logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square" alt="License">
</p>

---

## Why a rewrite?

The original MediaStation is a Python/FastAPI + Vue project. **MediaStationGo**
is a from-scratch reimplementation that adopts the lighter, single-binary
deployment model used by [`cropflre/nowen-video`](https://github.com/cropflre/nowen-video):

- **Backend**: Go 1.25 + Gin + GORM + SQLite (WAL).
- **Frontend**: React 18 + Vite + Tailwind + Zustand.
- **Distribution**: one ~30 MB static binary (CGO disabled), or a
  multi-arch Alpine Docker image.

The goal is to keep the user-facing feature surface familiar (libraries,
scanning, scraping, direct play/HLS, multi-user, downloads, RSS) while
making deployment painless on NAS hardware.

---

## Features

### Authentication & users
- ✅ JWT auth with admin/user roles
- ✅ First-run admin seeding (`admin / admin123`, override via `ADMIN_INITIAL_PASSWORD`)
- ✅ Profile page (email / avatar / change password)
- ✅ Admin user table with role promotion / demotion
- ✅ Audit log written for sensitive actions (login, library CRUD, downloads, …)

### Library management
- ✅ Library CRUD + recursive filesystem scan
- ✅ ffprobe metadata extraction (duration / resolution / codecs / container)
- ✅ Scene-noise filename cleaner with year + season/episode parsing
- ✅ TMDb scraper (movies) + Bangumi scraper (anime), provider chain by library type
- ✅ Image proxy with disk cache for TMDb / Bangumi / Douban / Fanart / TheTVDB
- ✅ TV / anime libraries grouped by season with episode listing
- ✅ fsnotify-based filesystem watcher with 5 s coalescing debouncer

### Playback
- ✅ Direct-play streaming with HTTP `Range` support
- ✅ HLS on-demand transcoding (single ffmpeg job per media)
- ✅ External subtitle discovery (.srt / .vtt / .ass / .ssa) with on-the-fly WebVTT conversion
- ✅ Resume position written every 10 s + Continue Watching row on home
- ✅ Favourites (toggle) + ordered Playlists (CRUD)

### Automation
- ✅ qBittorrent download integration (add / list / delete via Web UI API)
- ✅ RSS subscriptions with regex filters, GUID dedup and 10-minute polling

### Operations
- ✅ Real-time scan / scrape / transcode / download / subscription events over WebSocket
- ✅ Operator dashboard at `/stats` (CPU / memory / disk / library counts / Goroutines)
- ✅ Single-binary build, multi-arch Docker image, GitHub Actions CI + GHCR publish

### Frontend
- ✅ React SPA with code-splitting: Login / Home / Library / Search / Favourites /
  Playlists / Media detail / Player (HLS + direct + subtitles) / Profile /
  Downloads / Subscriptions / Stats / Admin
- ✅ Global toast notifications driven by the WebSocket hub
- ✅ Initial bundle ~250 KB / 83 KB gzipped (hls.js loaded only on first HLS playback)

### Roadmap

| Area | Status |
|------|--------|
| Hardware-accelerated transcoding (NVENC / QSV / VAAPI) profiles | ⏳ |
| Douban / Fanart / TheTVDB providers | ⏳ |
| Subtitle search (online providers) | ⏳ |
| Emby/Jellyfin compatibility layer | ⏳ |
| DLNA / Chromecast | ⏳ |
| AI metadata enhancement & smart search | ⏳ |

---

## Quick start

### Docker

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# (optional) edit docker-compose.yml to mount your media root at /media
docker compose up -d
```

Open <http://localhost:8080> and log in with `admin / admin123`.

### Bare metal

```bash
# requirements: Go 1.25+, Node 20+, ffmpeg
make build       # produces bin/mediastation-go and web/dist
./bin/mediastation-go
```

### Local development

```bash
make dev         # backend on :8080, MEDIASTATION_APP_DEBUG=true
make dev-web     # vite dev server on :3000, proxies /api -> :8080
```

---

## Configuration

Configuration is layered — defaults < `config.yaml` < `config/*.yaml` <
environment variables prefixed with `MEDIASTATION_`.

### Most-used keys

| Key | Default | Purpose |
|------|---------|---------|
| `MEDIASTATION_APP_PORT` | `8080` | HTTP listen port |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | DB / cache / JWT secret root |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | SPA bundle to serve |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite file |
| `MEDIASTATION_SECRETS_JWT_SECRET` | *(auto)* | JWT signing key |
| `MEDIASTATION_SECRETS_TMDB_API_KEY` | *(empty)* | Enables movie scraping |
| `MEDIASTATION_SECRETS_BANGUMI_ACCESS_TOKEN` | *(empty)* | Optional, raises Bangumi rate limit |
| `MEDIASTATION_APP_CORS_ORIGINS` | *(empty)* | Allow-list, JSON array |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | Bootstrap admin password |

### Runtime settings (admin → 设置)

These live in the `settings` table and can be edited from the admin UI:

| Key | Purpose |
|-----|---------|
| `qbittorrent.url` | qBittorrent Web UI base URL |
| `qbittorrent.username` | qBittorrent user |
| `qbittorrent.password` | qBittorrent password |
| `qbittorrent.savepath` | Optional default save path for new torrents |

After editing, hit **下载 → 重新加载配置** (or `POST /api/downloads/reload`) so
the qBittorrent client picks up the new credentials.

See [`config.example.yaml`](config.example.yaml) for the full surface.

---

## Project layout

```
MediaStationGo/
├── cmd/server/main.go          Application entry point
├── internal/
│   ├── config/                 Viper-based config loader
│   ├── database/               GORM + SQLite (WAL) bootstrap
│   ├── model/                  GORM data models + AutoMigrate registry
│   ├── repository/             Thin data-access layer
│   ├── service/                Business logic
│   │   ├── auth.go             login / register / JWT / seed admin
│   │   ├── media.go            library + media CRUD
│   │   ├── scanner.go          fs walker + ffprobe + scrape kick
│   │   ├── ffprobe.go          ffprobe wrapper
│   │   ├── tmdb.go             TMDb provider
│   │   ├── bangumi.go          Bangumi provider
│   │   ├── scraper.go          orchestrator + filename cleaner
│   │   ├── stream.go           direct play + HLS playlist / segment
│   │   ├── transcoder.go       per-media ffmpeg HLS job manager
│   │   ├── subtitle.go         external subtitle discovery + .vtt conversion
│   │   ├── image_proxy.go      cached, allow-listed image proxy
│   │   ├── playback.go         history / favourites / playlists
│   │   ├── watcher.go          fsnotify debouncer
│   │   ├── qbittorrent.go      qBittorrent v2 API client
│   │   ├── downloads.go        download orchestrator + WS poller
│   │   ├── subscription.go     RSS poller
│   │   ├── stats.go            dashboard snapshot
│   │   ├── profile.go          non-credential user mutations
│   │   ├── audit.go            audit log writer
│   │   ├── ws_hub.go           pub/sub broker for the WS
│   │   └── walk.go / episode_parser.go  helpers
│   ├── middleware/             Gin middleware (CORS / JWT / admin)
│   └── handler/                HTTP route definitions (one file per concern)
├── web/                        React 18 + Vite SPA
│   ├── src/api/                axios helpers (one per service)
│   ├── src/components/         Layout, MediaCard, GlobalEvents, RequireAuth
│   ├── src/hooks/              useWebSocket, …
│   ├── src/pages/              Home / Library / Search / Player / Downloads / …
│   ├── src/stores/             Zustand (auth)
│   └── src/types/              Domain types mirrored from Go
├── Dockerfile                  Multi-stage, multi-arch build
├── docker-compose.yml          NAS-friendly deployment
├── Makefile                    build / dev / docker / test
├── config.example.yaml         Full configuration template
└── .github/workflows/          CI + GHCR publish
```

---

## License

Released under the [GNU GPL v3.0](LICENSE).
