# MediaStationGo

> 轻量、漂亮、NAS 友好的私人媒体中心。Go 单二进制后端 + React 前端，覆盖媒体库管理、刮削、播放、订阅下载、Emby API 兼容、AI 搜索与推荐。

[English](README_EN.md) · [Docker 部署](#docker-部署推荐) · [一键脚本](#一键脚本部署) · [开发构建](#开发与构建)

## 亮点

- **现代 UI**：统一浅色高级视觉方案，首页聚焦「本周力荐 / 继续观看 / 最近入库」，媒体库页按电影、剧集、动漫、综艺等分类展示，并自动用库内海报生成文件夹封面。
- **媒体库扫描与整理**：递归扫描、ffprobe 元数据、季/集识别、综艺按节目/季/集展示、重复扫描去重、本地 NFO/图片优先。
- **多源刮削**：TMDb、TheTVDB、Bangumi、豆瓣、Fanart.tv、JavDB/JavBus 成人内容页面直爬补全；优先读取本地 NFO、poster、fanart、DMM/JAV 图片。
- **播放体验**：直链播放、Range 拖动、HLS 转码、外挂字幕、播放历史、继续观看、外部播放器入口。
- **外部客户端兼容**：提供 Emby/Jellyfin 风格 API，便于 Infuse、VidHub、SenPlayer 等客户端访问。
- **PT 与下载**：站点管理、M-Team `x-api-key`、跨站搜索、订阅、qBittorrent 下载、下载后智能分类整理。
- **AI 助手**：OpenAI 兼容接口，支持自然语言搜索、智能推荐；后台 API 配置实时生效。
- **部署简单**：裸机一键脚本、Docker Compose、多架构镜像构建/推送脚本。

## 功能模块

| 模块 | 能力 |
| --- | --- |
| 媒体库 | 电影、电视剧、动漫、综艺、音乐、成人内容；自动封面、合集分季分集 |
| 刮削 | 本地 NFO/图片优先，TMDb/TheTVDB/Bangumi/豆瓣/Fanart/JavBus/JavDB 补全 |
| 播放 | 直链、HLS、字幕、续播、外部播放器、历史与收藏 |
| 发现 | TMDb / 豆瓣 / Bangumi 推荐入口，多源发现与订阅 |
| 下载 | qBittorrent、PT 站点、RSS/搜索订阅、自动整理 |
| 兼容 | Emby API、DLNA、外部客户端与三端 UI |
| 运维 | 任务、统计、存储、重复文件、回收站、NFO 导出 |
| AI | OpenAI 兼容 Base URL/API Key，智能搜索与推荐 |

## Docker 部署（推荐）

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo
cp config.example.yaml config.yaml
docker compose up -d
```

默认地址：`http://<服务器IP>:18080`

默认账号：`admin` / `admin123`

> 首次登录后请立即修改管理员密码，并在「媒体与用户」中添加媒体目录。

### docker-compose 关键挂载

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - ./media:/media:ro
```

可通过环境变量覆盖默认路径和端口：

```bash
MEDIASTATION_HTTP_PORT=18080 MEDIASTATION_MEDIA_DIR=/your/media/path docker compose up -d
```

### 硬件转码

- Intel QSV/VAAPI：挂载 `/dev/dri:/dev/dri`
- NVIDIA NVENC：宿主机安装 NVIDIA Container Toolkit，并启用 `gpus: all`
- 软件转码：无需额外配置

## 一键脚本部署

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

脚本会自动：

1. 安装前端依赖并构建 `web/dist`
2. 编译 Go 服务端到 `bin/`
3. 创建数据与缓存目录
4. 停止旧进程并启动新进程
5. 调用 `/api/health` 做健康检查

## Docker 镜像打包与推送

默认推送到 `ghcr.io/shukebta/mediastation-go:latest`：

```bash
docker login ghcr.io
IMAGE=ghcr.io/shukebta/mediastation-go TAG=latest ./scripts/docker-build-push.sh
```

Windows：

```powershell
docker login ghcr.io
.\scripts\docker-build-push.ps1 -Image ghcr.io/shukebta/mediastation-go -Tag latest
```

仅本地构建不推送：

```bash
PUSH=0 TAG=dev ./scripts/docker-build-push.sh
```

```powershell
.\scripts\docker-build-push.ps1 -Tag dev -Load
```

## 开发与构建

### 环境要求

| 组件 | 版本 |
| --- | --- |
| Go | 1.25+ |
| Node.js | 20+ |
| FFmpeg / ffprobe | 推荐安装 |
| Docker | 可选 |

### 本地构建

```bash
cp config.example.yaml config.yaml
cd web && npm ci && npm run build
cd ..
go build -o bin/mediastation-go ./cmd/server
./bin/mediastation-go
```

### 常用命令

```bash
make build       # 构建前后端
make test        # Go 测试
make docker      # docker compose up -d
make deploy      # Linux 一键部署
make docker-push # buildx 多架构推送
```

Windows 可直接使用：

```powershell
.\scripts\deploy.ps1
```

## 配置说明

配置优先级从低到高：

1. 内置默认值
2. `config.yaml`
3. `config/*.yaml`
4. `MEDIASTATION_` 环境变量
5. 后台数据库配置（API Key、站点、下载器等运行时配置）

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MEDIASTATION_APP_PORT` | `8080` | Web 服务端口 |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | 数据目录 |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite 数据库 |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | 前端静态资源 |
| `MEDIASTATION_CACHE_CACHE_DIR` | `./cache` | 图片/转码缓存 |
| `MEDIASTATION_SECRETS_JWT_SECRET` | 自动生成 | JWT 与密钥加密种子 |

## API 与外部服务

在「外部 API 配置」中可配置：

- TMDb：影视元数据
- Bangumi：番剧与动漫
- TheTVDB：剧集补充
- Fanart.tv：高清艺术图
- OpenAI Compatible：AI 搜索/推荐
- Adult/JAV：JavBus/JavDB 页面直爬，不需要 API

M-Team 站点请使用「控制台 → 实验室 → 存取令牌」生成 API Access Token，并通过 `x-api-key` 使用；不要使用 Cookie 调用开放 API。

## 隐私与仓库安全

项目默认忽略以下个人/运行数据：

- `data/`、`cache/`、`logs/`
- `.tmp-deploy-data/`、`.tmp-deploy-server.*`
- `config.yaml`、`.env*`
- `*.db`、`*.db-wal`、`*.log`
- `web/dist/`、`node_modules/`、`bin/`

推送代码前建议执行：

```bash
git status --short
git ls-files | grep -E 'data/|cache/|\\.db|\\.log|jwt_secret|config.yaml|\\.env' || true
```

## 许可证

本项目遵循 `GPL-3.0` 许可证。详见 [LICENSE](LICENSE)。
