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
scanning, scraping, direct play/HLS, multi-user, downloads) while making
deployment painless on NAS hardware.

---

## Features (current scaffold)

- ✅ JWT authentication with admin/user roles
- ✅ First-run admin seeding (`admin / admin123`)
- ✅ Library CRUD + recursive filesystem scan
- ✅ ffprobe metadata extraction (duration / resolution / codecs)
- ✅ TMDb scraper with image proxy (poster / backdrop / overview / rating)
- ✅ Direct-play streaming with HTTP `Range` support
- ✅ HLS on-demand transcoding (single ffmpeg job per media)
- ✅ Playback history (resume) + Continue Watching row
- ✅ Favourites + Playlists (CRUD + ordered items)
- ✅ Real-time scan / scrape / transcode progress via WebSocket
- ✅ React SPA with code-splitting: Login / Home / Library / Search /
  Favourites / Playlists / Media detail / Player (HLS + direct) / Admin
- ✅ Single-binary build, multi-arch Docker image, GitHub Actions CI

### Roadmap

| Area | Status |
|------|--------|
| Bangumi / Douban / Fanart scraper providers | ⏳ |
| Hardware-accelerated transcoding (NVENC / QSV / VAAPI) | ⏳ |
| qBittorrent / Transmission / RSS automation | ⏳ |
| Subtitles (extract / search / sync) | ⏳ |
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

The most common keys:

| Key | Default | Purpose |
|------|---------|---------|
| `MEDIASTATION_APP_PORT` | `8080` | HTTP listen port |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | DB / cache / JWT secret root |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | SPA bundle to serve |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite file |
| `MEDIASTATION_SECRETS_JWT_SECRET` | *(auto)* | JWT signing key |
| `MEDIASTATION_APP_CORS_ORIGINS` | *(empty)* | Allow-list, JSON array |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | Bootstrap admin password |

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
│   ├── service/                Business logic (auth/media/scan/stream/ws)
│   ├── middleware/             Gin middleware (CORS / JWT / admin)
│   └── handler/                HTTP route definitions
├── web/                        React 18 + Vite SPA
│   ├── src/api/                axios helpers
│   ├── src/components/         Layout, MediaCard, RequireAuth, …
│   ├── src/pages/              Home / Library / Search / Player / Admin
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
