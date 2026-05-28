# MediaStationGo

> A lightweight, polished, NAS-friendly private media center. Go single-binary backend + React frontend for library management, scraping, playback, subscriptions, downloads, Emby-compatible APIs, AI search, and recommendations.

[中文](README.md) · [Docker Deploy](#docker-deploy-recommended) · [One-Click Scripts](#one-click-deploy-scripts) · [Development](#development-and-build)

## Highlights

- **Modern UI**: A unified bright premium visual system. The home page focuses on Featured, Continue Watching, and Recently Added. The library page groups movies, series, anime, variety shows, and music with automatically generated cover-folder cards.
- **Library scanning and organization**: Recursive scanning, ffprobe metadata, season/episode recognition, variety-show grouping by show/season/episode, duplicate prevention, and local NFO/image-first metadata.
- **Multi-source scraping**: TMDb, TheTVDB, Bangumi, Douban, Fanart.tv, and adult metadata enrichment from JavDB/JavBus pages. Local NFO, poster, fanart, DMM/JAV images are always preferred.
- **Playback experience**: Direct streaming, HTTP Range seeking, HLS transcoding, external subtitles, watch history, Continue Watching, and external-player entry points.
- **External client compatibility**: Emby/Jellyfin-style APIs for Infuse, VidHub, SenPlayer, and other third-party clients.
- **PT and downloads**: Site management, M-Team `x-api-key`, cross-site search, subscriptions, qBittorrent downloads, and intelligent post-download organization.
- **AI assistant**: OpenAI-compatible endpoint configuration for natural-language search and recommendations. Admin-side API settings take effect at runtime.
- **Simple deployment**: Bare-metal one-click scripts, Docker Compose, and multi-architecture Docker image build/push scripts.

## Feature Modules

| Module | Capabilities |
| --- | --- |
| Libraries | Movies, TV, anime, variety, music, adult content; automatic covers and season/episode grouping |
| Scraping | Local NFO/images first, then TMDb/TheTVDB/Bangumi/Douban/Fanart/JavBus/JavDB enrichment |
| Playback | Direct play, HLS, subtitles, resume, external players, history, favorites |
| Discovery | TMDb / Douban / Bangumi recommendation sources and subscription entry points |
| Downloads | qBittorrent, PT sites, RSS/search subscriptions, automatic organization |
| Compatibility | Emby API, DLNA-ready structure, external clients, responsive three-end UI |
| Operations | Tasks, statistics, storage, duplicate files, recycle bin, NFO export |
| AI | OpenAI-compatible Base URL/API Key, smart search, recommendations |

## Docker Deploy (Recommended)

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo
cp config.example.yaml config.yaml
docker compose up -d
```

Default URL: `http://<server-ip>:18080`

Default account: `admin` / `admin123`

> Change the administrator password immediately after the first login, then add media folders from the admin UI.

### Key docker-compose Mounts

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - ./media:/media:ro
```

Override the default port and media path with environment variables:

```bash
MEDIASTATION_HTTP_PORT=18080 MEDIASTATION_MEDIA_DIR=/your/media/path docker compose up -d
```

### Hardware Transcoding

- Intel QSV/VAAPI: mount `/dev/dri:/dev/dri`
- NVIDIA NVENC: install NVIDIA Container Toolkit on the host and enable `gpus: all`
- Software transcoding: no additional setup required

## One-Click Deploy Scripts

### Linux / macOS

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo
chmod +x scripts/deploy.sh
PORT=18080 DATA_DIR=/opt/mediastation/data CACHE_DIR=/opt/mediastation/cache ./scripts/deploy.sh
```

### Windows PowerShell

```powershell
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo
.\scripts\deploy.ps1 -Port 18080 -DataDir D:\MediaStationGo\data -CacheDir D:\MediaStationGo\cache
```

The scripts automatically:

1. Install frontend dependencies and build `web/dist`
2. Compile the Go server into `bin/`
3. Create data and cache directories
4. Stop any previously started process
5. Start the service and verify `/api/health`

## Docker Image Build and Push

The default image is `ghcr.io/shukebta/mediastation-go:latest`:

```bash
docker login ghcr.io
IMAGE=ghcr.io/shukebta/mediastation-go TAG=latest ./scripts/docker-build-push.sh
```

Windows:

```powershell
docker login ghcr.io
.\scripts\docker-build-push.ps1 -Image ghcr.io/shukebta/mediastation-go -Tag latest
```

Build locally without pushing:

```bash
PUSH=0 TAG=dev ./scripts/docker-build-push.sh
```

```powershell
.\scripts\docker-build-push.ps1 -Tag dev -Load
```

## Development and Build

### Requirements

| Component | Version |
| --- | --- |
| Go | 1.25+ |
| Node.js | 20+ |
| FFmpeg / ffprobe | Recommended |
| Docker | Optional |

### Local Build

```bash
cp config.example.yaml config.yaml
cd web && npm ci && npm run build
cd ..
go build -o bin/mediastation-go ./cmd/server
./bin/mediastation-go
```

### Common Commands

```bash
make build       # Build frontend and backend
make test        # Run Go tests
make docker      # docker compose up -d
make deploy      # Linux one-click deploy
make docker-push # Multi-arch buildx push
```

Windows users can run:

```powershell
.\scripts\deploy.ps1
```

## Configuration

Configuration precedence, from low to high:

1. Built-in defaults
2. `config.yaml`
3. `config/*.yaml`
4. `MEDIASTATION_` environment variables
5. Runtime settings stored in the database, such as API keys, sites, and download clients

Common environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `MEDIASTATION_APP_PORT` | `8080` | Web service port |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | Data directory |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite database path |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | Frontend static bundle |
| `MEDIASTATION_CACHE_CACHE_DIR` | `./cache` | Image/transcode cache |
| `MEDIASTATION_SECRETS_JWT_SECRET` | Auto-generated | JWT and encryption seed |

## APIs and External Services

Configure external services from the admin UI:

- TMDb: movie and TV metadata
- Bangumi: anime and series metadata
- TheTVDB: additional TV metadata
- Fanart.tv: high-resolution artwork
- OpenAI Compatible: AI search and recommendations
- Adult/JAV: JavBus/JavDB page scraping, no API key required

For M-Team, generate an API Access Token from `Control Panel → Lab → Access Token` and send it as `x-api-key`. Do not use cookies for the open API.

## Privacy and Repository Safety

The project ignores personal and runtime data by default:

- `data/`, `cache/`, `logs/`
- `.tmp-deploy-data/`, `.tmp-deploy-server.*`, `.mediastation.pid`
- `config.yaml`, `.env*`
- `*.db`, `*.db-wal`, `*.log`
- `web/dist/`, `node_modules/`, `bin/`

Before pushing code, run:

```bash
git status --short
git ls-files | grep -E 'data/|cache/|\\.db|\\.log|jwt_secret|config.yaml|\\.env' || true
```

## License

This project is licensed under `GPL-3.0`. See [LICENSE](LICENSE) for details.
