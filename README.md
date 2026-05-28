# MediaStationGo

<p align="center">
  <img src="web/public/favicon.svg" width="96" height="96" alt="MediaStationGo Logo" />
</p>

<h3 align="center">轻量、漂亮、NAS 友好的私人媒体中心</h3>

<p align="center">
  <strong>Go 单二进制后端 · React 现代化前端 · Docker 一键部署 · Emby API 兼容 · 多源刮削 · PT 订阅下载</strong>
</p>

<p align="center">
  <a href="README_EN.md">English</a> ·
  <a href="#quick-start">快速开始</a> ·
  <a href="#docker-compose-deploy">Docker 部署</a> ·
  <a href="#screenshots">界面预览</a> ·
  <a href="https://mgo.3jzs.com">在线演示</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111827" />
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square" />
  <img alt="Use" src="https://img.shields.io/badge/Use-Non--Commercial-orange?style=flat-square" />
</p>

---

## ✨ 项目简介

MediaStationGo 是一个面向个人、家庭 NAS 与影音爱好者的开源媒体中心。它把「媒体库管理、自动刮削、在线播放、外部客户端兼容、PT 站点检索、订阅下载、AI 推荐」放进一个轻量的 Go 服务里，配合 React 前端提供统一、简洁、漂亮的三端体验。

它适合这些场景：

- 家里有 NAS / Windows 主机 / Linux 小主机，希望统一管理电影、剧集、动漫、综艺、成人内容。
- 想使用 TMDb、豆瓣、Bangumi、TheTVDB、Fanart、JavBus/JavDB 等多源元数据补全海报、简介、分季分集信息。
- 想把 PT 站点搜索、订阅、下载器、下载后整理集中到一个 Web 面板中。
- 想让 Infuse、VidHub、SenPlayer 等外部客户端通过 Emby 风格接口访问媒体库。
- 想要一个部署简单、便于二次开发、不会把密钥和私有 Token 暴露到前端的开源项目。

> 当前项目仍在快速迭代中，建议固定镜像版本部署，并定期备份 `/data` 目录。

---

## 🌱 开源承诺

MediaStationGo 采用完全开源路线，核心媒体库、刮削、播放、订阅、下载、外部客户端兼容与运维能力均在本仓库持续迭代。项目参考了 MoviePilot 等优秀开源项目在「站点聚合、订阅下载、媒体整理、Emby/Jellyfin 客户端兼容」上的产品思路，但本项目会保持独立实现，不直接复制不兼容代码。

本项目当前基础许可证为 `GPL-3.0`，欢迎基于 GPL-3.0 协议参与改造、适配站点、提交刮削规则与优化 UI。项目作者同时倡议：本项目面向个人学习、家庭 NAS、自建影音与非商业场景使用，未经作者明确书面许可，不得将本项目或其衍生版本用于商业售卖、商业托管、付费 SaaS、预装售卖设备、闭源二次分发或其他商业化牟利用途。

> 说明：GPL-3.0 是自由软件许可证，其正式授权范围以仓库 [LICENSE](LICENSE) 文件为准；上方「非商用承诺」表达项目维护者的使用边界与商业合作要求。如需商业合作、企业部署或二次发行，请先联系作者获得额外授权。

---

## 🚀 在线演示

- 演示站：[https://mgo.3jzs.com](https://mgo.3jzs.com)
- 默认账号：`admin`
- 默认密码：`admin123`

> 演示环境仅用于功能体验，请勿上传真实隐私信息或配置私人 API Key。

---

## 🧭 功能总览

| 模块 | 能力 |
| --- | --- |
| 媒体库 | 电影、电视剧、动漫、综艺、音乐、成人内容；支持文件夹封面、合集、季、集展示 |
| 扫描识别 | 递归扫描、ffprobe 探测、文件名解析、季集识别、综艺节目识别、重复扫描去重 |
| 本地元数据 | 优先读取 NFO、poster、fanart、season poster、episode image、本地成人影片图片 |
| 在线刮削 | TMDb、TheTVDB、Bangumi、豆瓣、Fanart.tv、JavBus/JavDB 页面直爬补全 |
| 播放体验 | 直链播放、HTTP Range 拖动、HLS 转码、外挂字幕、播放进度、继续观看、外部播放器 |
| 发现与搜索 | TMDb / 豆瓣 / Bangumi 多源推荐，智能搜索，详情页订阅入口 |
| PT 站点 | 站点管理、M-Team API Token、站点搜索、种子链接解析、下载器联动 |
| 订阅下载 | RSS / 站点搜索订阅，分辨率/质量/特效/发布组/排除词规则，洗版开关与优先级 |
| 下载中心 | qBittorrent 任务状态、速度、进度、上传下载体积、小卡片海报展示、私有 URL 脱敏 |
| 外部兼容 | Emby/Jellyfin 风格 API，兼容 Infuse、VidHub、SenPlayer 等外部客户端 |
| AI 能力 | OpenAI Compatible API 配置，AI 搜索、推荐、运维助手 |
| 运维工具 | 运行状态、任务队列、重复文件、回收站、文件管理、存储配置、通知渠道 |

---

<a id="screenshots"></a>

## 🖼️ 界面预览

> 以下截图使用 Codex 内置浏览器从当前运行实例采集，并已对个人媒体内容、本地路径、账号信息、API Key/Token/密钥等敏感信息做图像级打码处理。

<details open>
<summary><strong>核心体验</strong></summary>

| 登录与首页 | 媒体库总览 |
| --- | --- |
| <img src="docs/screenshots/00-login.jpg" alt="登录界面" width="100%"> | <img src="docs/screenshots/01-home.jpg" alt="系统首页" width="100%"> |
| 媒体库总览 | 媒体库详情 |
| <img src="docs/screenshots/02-libraries.jpg" alt="媒体库总览" width="100%"> | <img src="docs/screenshots/03-library-detail.jpg" alt="媒体库详情" width="100%"> |
| 海报墙 | 媒体详情 |
| <img src="docs/screenshots/04-poster-wall.jpg" alt="海报墙" width="100%"> | <img src="docs/screenshots/05-media-detail.jpg" alt="媒体详情" width="100%"> |
| 播放器 | 精彩发现 |
| <img src="docs/screenshots/06-player.jpg" alt="播放器" width="100%"> | <img src="docs/screenshots/07-discover.jpg" alt="精彩发现" width="100%"> |
| 智能搜索 | DLNA 投屏 |
| <img src="docs/screenshots/08-search.jpg" alt="智能搜索" width="100%"> | <img src="docs/screenshots/09-dlna.jpg" alt="DLNA 投屏" width="100%"> |

</details>

<details>
<summary><strong>个人空间与播放管理</strong></summary>

| AI 助理 | 我的收藏 |
| --- | --- |
| <img src="docs/screenshots/10-ai.jpg" alt="AI 助理" width="100%"> | <img src="docs/screenshots/11-favourites.jpg" alt="我的收藏" width="100%"> |
| 播放列表 | 观看历史 |
| <img src="docs/screenshots/12-playlists.jpg" alt="播放列表" width="100%"> | <img src="docs/screenshots/13-history.jpg" alt="观看历史" width="100%"> |
| 账号信息 | 下载中心 |
| <img src="docs/screenshots/14-profile.jpg" alt="账号信息" width="100%"> | <img src="docs/screenshots/15-downloads.jpg" alt="下载中心" width="100%"> |

</details>

<details>
<summary><strong>下载订阅与站点</strong></summary>

| 下载器管理 | 订阅管理 |
| --- | --- |
| <img src="docs/screenshots/16-download-clients.jpg" alt="下载器管理" width="100%"> | <img src="docs/screenshots/17-subscriptions.jpg" alt="订阅管理" width="100%"> |
| 站点检索 | 站点与下载器 |
| <img src="docs/screenshots/18-site-search.jpg" alt="站点检索" width="100%"> | <img src="docs/screenshots/20-sites.jpg" alt="站点与下载器" width="100%"> |

</details>

<details>
<summary><strong>管理与运维</strong></summary>

| 媒体与用户 | 整理与维护 |
| --- | --- |
| <img src="docs/screenshots/19-admin.jpg" alt="媒体与用户" width="100%"> | <img src="docs/screenshots/21-tools.jpg" alt="整理与维护" width="100%"> |
| 存储与文件 | 运行状态 |
| <img src="docs/screenshots/22-storage.jpg" alt="存储与文件" width="100%"> | <img src="docs/screenshots/23-stats.jpg" alt="运行状态" width="100%"> |
| 系统设置 | 任务队列 |
| <img src="docs/screenshots/24-settings.jpg" alt="系统设置" width="100%"> | <img src="docs/screenshots/25-tasks.jpg" alt="任务队列" width="100%"> |
| 重复文件 | 回收站 |
| <img src="docs/screenshots/26-duplicates.jpg" alt="重复文件" width="100%"> | <img src="docs/screenshots/27-recycle.jpg" alt="回收站" width="100%"> |
| 调度任务 | 文件管理 |
| <img src="docs/screenshots/28-scheduler.jpg" alt="调度任务" width="100%"> | <img src="docs/screenshots/29-files.jpg" alt="文件管理" width="100%"> |
| STRM 管理 | 存储配置 |
| <img src="docs/screenshots/30-strm.jpg" alt="STRM 管理" width="100%"> | <img src="docs/screenshots/31-storage-config.jpg" alt="存储配置" width="100%"> |
| 通知渠道 | AI 运维助手 |
| <img src="docs/screenshots/32-notify-channels.jpg" alt="通知渠道" width="100%"> | <img src="docs/screenshots/33-assistant.jpg" alt="AI 运维助手" width="100%"> |

</details>

---

## 🧱 技术栈

| 层级 | 技术 | 说明 |
| --- | --- | --- |
| 后端语言 | Go 1.25+ | 单二进制部署，启动快，资源占用低 |
| Web 框架 | Gin | REST API、鉴权中间件、静态资源托管 |
| 数据库 | SQLite + GORM | 适合个人/NAS 场景，数据文件易备份 |
| 前端框架 | React 18 + TypeScript | 组件化 UI，类型安全 |
| 构建工具 | Vite | 前端快速开发与生产打包 |
| 样式系统 | Tailwind CSS | 统一浅色高级视觉方案与响应式布局 |
| 状态管理 | Zustand | 轻量全局状态与鉴权状态维护 |
| 播放链路 | HTML5 Video / HLS / FFmpeg | 直链、Range、HLS 转码、字幕 |
| 元数据 | TMDb / 豆瓣 / Bangumi / TheTVDB / Fanart / JavBus / JavDB | 多源补全海报、简介、评分、分季分集 |
| 下载联动 | qBittorrent / PT Site Adapter | 站点搜索、订阅、下载任务展示与脱敏 |
| 外部兼容 | Emby-style API / DLNA | 面向外部播放器与三端客户端 |
| 部署 | Docker / Docker Compose / Shell / PowerShell | NAS、Linux、Windows 均可部署 |
| CI/CD | GitHub Actions / GHCR | 仅版本标签触发多架构镜像与 Release 包发布 |

---

<a id="quick-start"></a>

## 📦 快速开始

<a id="docker-compose-deploy"></a>

### Docker Compose 部署（推荐）

Docker 是最稳定、最容易迁移的部署方式。默认会创建四类目录：

| 宿主机目录 | 容器目录 | 作用 |
| --- | --- | --- |
| `./data` | `/data` | 数据库、JWT secret、运行时配置，必须备份 |
| `./cache` | `/cache` | 海报、背景图、刮削缓存、转码缓存 |
| `./media` | `/media` | 媒体库根目录，默认只读挂载 |
| `./downloads` | `/downloads` | 订阅/站点下载保存目录 |

#### Linux / NAS 从零部署教程（不克隆源码）

适合 Ubuntu、Debian、CentOS、AlmaLinux、Rocky Linux、群晖/威联通类 Linux 环境。以下命令默认在服务器终端执行。

> Docker 安装脚本来自第三方镜像脚本，适合国内网络快速安装；如果你是生产服务器，也可以改用 Docker 官方文档安装。

1. 安装 Docker：

```bash
bash <(curl -sSL https://cdn.jsdelivr.net/gh/SuperManito/LinuxMirrors@main/DockerInstallation.sh)

docker --version
```

2. 安装 Docker Compose：

```bash
curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose

docker-compose version
```

> 如果写入 `/usr/local/bin/docker-compose` 提示权限不足，请在 `curl` 和 `chmod` 前加 `sudo`。

> 如果你的系统已经支持 `docker compose version`，可以继续使用 `docker compose`；如果只安装了上面的独立二进制，请把后续命令中的 `docker compose` 替换为 `docker-compose`。

3. 创建部署目录：

```bash
mkdir -p ~/MediaStationGo
cd ~/MediaStationGo
mkdir -p data cache media downloads
```

4. 创建 `.env`，按需修改媒体库和下载目录：

```bash
cat > .env <<'EOF'
MEDIASTATION_IMAGE_TAG=latest
MEDIASTATION_HTTP_PORT=18080
MEDIASTATION_DATA_DIR=./data
MEDIASTATION_CACHE_DIR=./cache
MEDIASTATION_MEDIA_DIR=./media
MEDIASTATION_DOWNLOAD_DIR=./downloads
TZ=Asia/Shanghai
PUID=1000
PGID=1000
EOF
```

如果你的媒体文件已经在 NAS 路径中，建议改成类似：

```env
MEDIASTATION_MEDIA_DIR=/mnt/nas/media
MEDIASTATION_DOWNLOAD_DIR=/mnt/nas/downloads
```

5. 下载默认 `docker-compose.yml`：

```bash
curl -fsSL https://raw.githubusercontent.com/ShukeBta/MediaStationGo/main/docker-compose.yml -o docker-compose.yml
```

如果无法访问 GitHub Raw，也可以手动创建：

```bash
vi docker-compose.yml
# 或
vim docker-compose.yml
```

然后粘贴下面的模板。

<details>
<summary><strong>展开查看完整 docker-compose.yml 模板</strong></summary>

```yaml
# MediaStationGo 默认 Docker Compose 部署文件。
#
# 快速开始：
#   1. 按需修改下方 volumes 的宿主机路径。
#   2. docker compose pull
#   3. docker compose up -d
#   4. 浏览器打开 http://<服务器IP>:18080
#
# 默认账号：
#   admin / admin123
#   首次部署后请立即到「个人资料/用户管理」修改密码。
#
# 镜像版本：
#   默认拉取 latest；如需固定版本，创建 .env 并写入：
#     MEDIASTATION_IMAGE_TAG=MediaStationGo-v0.0.4
#
# 路径映射总览：
#   /data      程序数据目录。保存 SQLite 数据库、JWT secret、系统配置等，必须持久化。
#   /cache     缓存目录。保存海报、刮削图片、转码缓存等，建议放在空间较大的磁盘。
#   /media     媒体库只读挂载目录。网页中添加媒体库时填写容器内路径，例如 /media/Movies。
#   /downloads 下载目录。下载器保存路径建议填写容器内路径，例如 /downloads/Movies。
#
# 宿主机路径建议：
#   MEDIASTATION_DATA_DIR=./data
#   MEDIASTATION_CACHE_DIR=./cache
#   MEDIASTATION_MEDIA_DIR=/mnt/nas/media
#   MEDIASTATION_DOWNLOAD_DIR=/mnt/nas/downloads
#
# 注意：
#   1. 如果 qBittorrent/Transmission/Aria2 不在本 compose 内，请确保它们能访问同一份
#      下载目录；容器内保存路径和下载器实际保存路径需要保持一致或可被媒体库扫描到。
#   2. 媒体库默认以只读方式挂载，避免误删原始媒体；如果需要整理/移动文件，可将
#      /media 的 :ro 改为 :rw，或把整理目标放到 /downloads 后再手动迁移。
#   3. PUID/PGID 用于匹配 NAS/Linux 宿主机用户权限，避免下载或缓存文件权限异常。

services:
  mediastation-go:
    image: ghcr.io/shukebta/mediastation-go:${MEDIASTATION_IMAGE_TAG:-latest}
    # 默认只在本地没有镜像时拉取，避免每次重启都访问 GHCR。
    # 需要升级时手动执行：docker compose pull && docker compose up -d
    pull_policy: missing
    container_name: mediastation-go
    restart: unless-stopped

    ports:
      # 宿主机端口:容器端口。默认访问 http://<服务器IP>:18080
      - "${MEDIASTATION_HTTP_PORT:-18080}:8080"

    volumes:
      # 程序持久化数据：数据库、JWT secret、运行时配置。
      - ${MEDIASTATION_DATA_DIR:-./data}:/data

      # 海报/背景图/转码缓存。可删除重建，但会重新下载图片和生成缓存。
      - ${MEDIASTATION_CACHE_DIR:-./cache}:/cache

      # 媒体库根目录。添加媒体库时使用容器内路径：
      #   电影：/media/Movies
      #   剧集：/media/TV
      #   动漫：/media/Anime
      #   综艺：/media/Variety
      # 如宿主机目录不同，请在 .env 中设置 MEDIASTATION_MEDIA_DIR。
      - ${MEDIASTATION_MEDIA_DIR:-./media}:/media:ro

      # 下载保存目录。订阅/站点下载的保存路径建议使用：
      #   /downloads/Movies
      #   /downloads/TV
      #   /downloads/Anime
      #   /downloads/Variety
      # 如果外部下载器也运行在 Docker 中，请给下载器挂载同一个宿主机目录。
      - ${MEDIASTATION_DOWNLOAD_DIR:-./downloads}:/downloads

    environment:
      # Web 服务监听配置。容器内固定监听 8080，对外端口由上方 ports 控制。
      MEDIASTATION_APP_HOST: 0.0.0.0
      MEDIASTATION_APP_PORT: 8080
      MEDIASTATION_APP_WEB_DIR: /app/web/dist

      # 数据与缓存目录。需与 volumes 中的容器路径一致。
      MEDIASTATION_APP_DATA_DIR: /data
      MEDIASTATION_DATABASE_DB_PATH: /data/mediastation.db
      MEDIASTATION_CACHE_CACHE_DIR: /cache

      # 日志级别：debug / info / warn / error。
      MEDIASTATION_LOGGING_LEVEL: ${MEDIASTATION_LOGGING_LEVEL:-info}

      # 转码配置。留空表示自动/软件转码；硬件加速见下方 Intel/NVIDIA 示例。
      MEDIASTATION_TRANSCODER_ENCODER: ${MEDIASTATION_TRANSCODER_ENCODER:-}
      MEDIASTATION_TRANSCODER_MAX_HEIGHT: ${MEDIASTATION_TRANSCODER_MAX_HEIGHT:-1080}

      # 跨域来源。通常无需设置；反向代理或三端客户端异常时再按需填写。
      MEDIASTATION_APP_CORS_ORIGINS: ${MEDIASTATION_APP_CORS_ORIGINS:-}

      # 宿主机文件权限映射。Linux/NAS 常用 1000:1000，可用 id 命令查看。
      PUID: ${PUID:-1000}
      PGID: ${PGID:-1000}
      TZ: ${TZ:-Asia/Shanghai}

    # Intel QSV / VAAPI 硬件加速示例：
    #   1. Linux/NAS 宿主机存在 /dev/dri。
    #   2. 在 .env 中设置 MEDIASTATION_TRANSCODER_ENCODER=vaapi。
    #   3. 取消下方 devices/group_add 注释。
    # devices:
    #   - /dev/dri:/dev/dri
    # group_add:
    #   - "${RENDER_GID:-989}"

    # NVIDIA NVENC 硬件加速示例：
    #   1. 宿主机安装 NVIDIA Container Toolkit。
    #   2. 在 .env 中设置 MEDIASTATION_TRANSCODER_ENCODER=nvenc。
    #   3. 取消下方 gpus 注释。
    # gpus: all

    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8080/api/health || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 30s
```

</details>

6. 启动服务：

```bash
# Docker Compose v2
docker compose pull
docker compose up -d

# 如果你的系统只有 docker-compose 命令，则使用：
# docker-compose pull
# docker-compose up -d
```

7. 查看状态与日志：

```bash
docker compose ps
docker compose logs -f mediastation-go

# 或：
# docker-compose ps
# docker-compose logs -f mediastation-go
```

8. 浏览器访问：

```text
http://<服务器IP>:18080
```

默认账号：

```text
用户名：admin
密码：admin123
```

> 首次登录后请立即修改管理员密码。如果局域网无法访问，请检查服务器防火墙、安全组、NAS 防火墙，以及 `18080:8080` 端口映射是否生效。

#### 已有源码仓库的快速启动

如果你是开发者或已经克隆源码，也可以直接使用仓库内置的 `docker-compose.yml`：

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

docker compose pull
docker compose up -d
```

### 固定版本部署

建议生产环境固定版本，避免 `latest` 自动变化：

```bash
cat > .env <<'EOF'
MEDIASTATION_IMAGE_TAG=MediaStationGo-v0.0.4
MEDIASTATION_HTTP_PORT=18080
MEDIASTATION_MEDIA_DIR=/mnt/nas/media
MEDIASTATION_DOWNLOAD_DIR=/mnt/nas/downloads
MEDIASTATION_DATA_DIR=./data
MEDIASTATION_CACHE_DIR=./cache
TZ=Asia/Shanghai
EOF

docker compose up -d
```

### 媒体库路径怎么填

如果 compose 中这样挂载：

```yaml
- /mnt/nas/media:/media:ro
- /mnt/nas/downloads:/downloads
```

那么在 Web 管理页面中添加媒体库时应填写容器内路径，例如：

| 类型 | 推荐路径 |
| --- | --- |
| 电影 | `/media/Movies` |
| 剧集 | `/media/TV` |
| 动漫 | `/media/Anime` |
| 综艺 | `/media/Variety` |
| 成人内容 | `/media/Adult` |
| 下载入库 | `/downloads/Movies`、`/downloads/TV` 等 |

### 下载器路径怎么填

如果 qBittorrent 也运行在 Docker 中，必须让 qBittorrent 与 MediaStationGo 看到同一份下载目录。

建议统一约定：

```text
宿主机：/mnt/nas/downloads
MediaStationGo 容器：/downloads
qBittorrent 容器：/downloads
```

订阅保存路径可填写：

```text
/downloads/Movies
/downloads/TV
/downloads/Anime
/downloads/Variety
```

---

## 🐳 Docker Compose 配置示例

项目已内置详细注释版 `docker-compose.yml`，可直接使用。常用变量如下：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MEDIASTATION_IMAGE_TAG` | `latest` | 镜像标签，建议固定为 Release 版本 |
| `MEDIASTATION_HTTP_PORT` | `18080` | 宿主机访问端口 |
| `MEDIASTATION_DATA_DIR` | `./data` | 数据持久化目录 |
| `MEDIASTATION_CACHE_DIR` | `./cache` | 图片和转码缓存目录 |
| `MEDIASTATION_MEDIA_DIR` | `./media` | 媒体库宿主机目录 |
| `MEDIASTATION_DOWNLOAD_DIR` | `./downloads` | 下载保存宿主机目录 |
| `PUID` / `PGID` | `1000` / `1000` | Linux/NAS 文件权限映射 |
| `TZ` | `Asia/Shanghai` | 容器时区 |

查看日志：

```bash
docker logs -f mediastation-go
```

更新镜像：

```bash
docker compose pull
docker compose up -d
```

停止服务：

```bash
docker compose down
```

备份数据：

```bash
tar -czf mediastationgo-data-backup.tgz ./data
```

---

## 🖥️ 一键脚本部署

如果不想使用 Docker，也可以裸机运行。脚本会自动构建前端、编译后端、启动服务并检查健康状态。

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

脚本执行内容：

1. 安装前端依赖并构建 `web/dist`
2. 编译 Go 服务端到 `bin/`
3. 创建数据目录和缓存目录
4. 停止旧进程并启动新进程
5. 请求 `/api/health` 验证服务状态

---

## 🧩 Release 包部署

每个 Release 会提供多平台压缩包：

| 平台 | 包名示例 |
| --- | --- |
| Linux x86_64 | `MediaStationGo-v0.0.4-linux-amd64.tar.gz` |
| Linux ARM64 | `MediaStationGo-v0.0.4-linux-arm64.tar.gz` |
| Windows x86_64 | `MediaStationGo-v0.0.4-windows-amd64.zip` |
| macOS Intel | `MediaStationGo-v0.0.4-darwin-amd64.tar.gz` |
| macOS Apple Silicon | `MediaStationGo-v0.0.4-darwin-arm64.tar.gz` |

部署步骤：

```bash
# Linux 示例
tar -xzf MediaStationGo-v0.0.4-linux-amd64.tar.gz
cd MediaStationGo-v0.0.4-linux-amd64
MEDIASTATION_APP_PORT=18080 ./mediastation-go
```

Windows：

```powershell
Expand-Archive .\MediaStationGo-v0.0.4-windows-amd64.zip
cd .\MediaStationGo-v0.0.4-windows-amd64
$env:MEDIASTATION_APP_PORT = "18080"
.\mediastation-go.exe
```

> Release 二进制默认监听 `8080`，如果希望和 Docker 示例保持一致，请按上方设置 `MEDIASTATION_APP_PORT=18080`。

---

## 🛠️ 本地开发

### 环境要求

| 组件 | 版本 | 用途 |
| --- | --- | --- |
| Go | 1.25+ | 后端编译与测试 |
| Node.js | 20+ | 前端构建 |
| FFmpeg / ffprobe | 推荐安装 | 媒体探测与转码 |
| Docker | 可选 | 容器部署与多架构镜像 |
| qBittorrent | 可选 | 下载器联动测试 |

### 本地构建

```bash
cp config.example.yaml config.yaml
cd web
npm ci
npm run build
cd ..
go build -o bin/mediastation-go ./cmd/server
./bin/mediastation-go
```

Windows：

```powershell
Copy-Item config.example.yaml config.yaml
Set-Location web
npm ci
npm run build
Set-Location ..
go build -o bin\mediastation-go.exe .\cmd\server
.\bin\mediastation-go.exe
```

### 常用命令

```bash
make build       # 构建前后端
make test        # 运行 Go 测试
make smoke       # 冒烟测试
make docker      # docker compose up -d
make deploy      # Linux 一键部署
make docker-push # buildx 多架构推送
```

---

## 🏗️ 项目结构

```text
MediaStationGo/
├── cmd/server/                 # 服务入口
├── internal/
│   ├── config/                  # 配置加载与默认值
│   ├── database/                # SQLite 初始化与迁移
│   ├── handler/                 # HTTP API / Emby API / 管理接口
│   ├── middleware/              # 鉴权、权限、日志中间件
│   ├── model/                   # GORM 数据模型
│   ├── repository/              # 数据访问层
│   └── service/                 # 扫描、刮削、播放、下载、订阅等业务逻辑
├── web/
│   ├── public/                  # favicon 等静态资源
│   ├── src/                     # React 页面、组件、API、状态管理
│   └── dist/                    # 前端构建产物，默认不入库
├── scripts/                     # 部署、打包、Docker 构建脚本
├── docs/                        # 设计文档、截图与架构说明
├── docker-compose.yml           # 默认 Docker Compose 部署文件
├── Dockerfile                   # 多阶段镜像构建
├── config.example.yaml          # 配置模板
└── README.md / README_EN.md     # 项目文档
```

---

## ⚙️ 配置说明

配置优先级从低到高：

1. 内置默认值
2. `config.yaml`
3. `config/*.yaml`
4. `MEDIASTATION_` 环境变量
5. 后台数据库运行时配置

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MEDIASTATION_APP_HOST` | `0.0.0.0` | 服务监听地址 |
| `MEDIASTATION_APP_PORT` | `8080` | 服务监听端口 |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | 前端静态资源目录 |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | 程序数据目录 |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite 数据库路径 |
| `MEDIASTATION_CACHE_CACHE_DIR` | `./cache` | 图片/转码缓存目录 |
| `MEDIASTATION_SECRETS_JWT_SECRET` | 自动生成 | JWT 和敏感配置加密种子 |
| `MEDIASTATION_APP_CORS_ORIGINS` | 空 | 额外允许的跨域来源 |

后台可运行时配置：

- API Key：TMDb、Bangumi、TheTVDB、Fanart、OpenAI Compatible 等。
- 站点：M-Team、NexusPHP、Unit3D、自定义 RSS 等。
- 下载器：qBittorrent、Transmission、Aria2。
- 通知渠道：Telegram、Bark、Webhook、Email 等。
- 播放配置、权限配置、调度任务、存储配置。

---

## 🔍 刮削与元数据策略

MediaStationGo 的刮削顺序尽量避免重复请求和错误覆盖：

1. 优先读取本地 NFO、poster、fanart、season poster、episode image。
2. 根据文件名识别电影、剧集、动漫、综艺、成人内容。
3. 使用 TMDb / TheTVDB / Bangumi / 豆瓣补全缺失元数据。
4. 使用 Fanart.tv 补充更高清的艺术图。
5. 成人内容优先读取本地 NFO 与图片，再通过 JavBus/JavDB 等公开页面补全。
6. 已有本地元数据不会被无意义重复刮削覆盖。

推荐目录结构：

```text
/media/Movies/Inception (2010)/Inception (2010).mkv
/media/TV/Some Show/Season 01/Some Show S01E01.mkv
/media/Anime/Anime Title/Season 01/Anime Title S01E01.mkv
/media/Variety/Show Name/Season 2026/Show Name S2026E01.mkv
/media/Adult/ABCD-123/ABCD-123.mp4
```

本地图片常见命名：

```text
poster.jpg
fanart.jpg
folder.jpg
season01-poster.jpg
S01E01-thumb.jpg
movie.nfo
tvshow.nfo
episode.nfo
```

---

## 🔎 发现、搜索与订阅下载

### 多源发现

精彩发现支持：

- TMDb：趋势、热门电影、热门剧集、高分电影。
- 豆瓣：热门电影、高分电影、热门剧集。
- Bangumi：每日放送、动漫条目。

### 智能搜索

智能搜索会同时考虑：

- 本地媒体库已有内容。
- TMDb / 豆瓣 / Bangumi 等在线结果。
- 可订阅关键词与媒体类型。

### 订阅规则

订阅支持以下规则：

| 规则 | 说明 |
| --- | --- |
| 媒体类型 | 电影、剧集、动漫、综艺，支持自动识别 |
| 搜索模式 | 标题关键词或 IMDB ID |
| 分辨率 | 自动择优、2160p、1080p、720p |
| 质量 | REMUX、BluRay、WEB-DL、HDTV 等 |
| 特效 | HDR、Dolby Vision、Atmos 等 |
| 发布组 | 白名单发布组 |
| 排除词 | 排除 CAM、TS、枪版等低质资源 |
| 洗版 | 默认关闭，可按分辨率、质量、特效、做种数优先 |

下载与订阅卡片只展示安全标题、海报、进度、速度、体积等信息，不展示原始种子 URL，避免多用户场景泄露私人 Tracker Token。

---

## 🔌 外部客户端与 Emby 兼容

项目提供 Emby/Jellyfin 风格 API，用于外部客户端连接：

```text
http://<服务器IP>:18080
```

可尝试的客户端：

- Infuse
- VidHub
- SenPlayer
- 其他支持 Emby/Jellyfin 服务器的播放器

建议检查：

1. Docker 端口是否映射为 `18080:8080`。
2. 防火墙是否允许局域网访问 `18080`。
3. 账号密码是否正确。
4. 反向代理是否正确转发 `/api`、视频流和 Range 请求。

### 与 MoviePilot 的功能参考关系

MediaStationGo 在外部客户端兼容与媒体生态联动上参考了 MoviePilot 的成熟产品路径：通过统一媒体库、订阅下载、下载后整理和 Emby/Jellyfin 兼容接口，把 Web 管理端与 Infuse、VidHub、SenPlayer 等客户端串起来。本项目的目标不是替代 Emby/Jellyfin，而是在轻量 Go 服务中提供足够常用的媒体浏览、播放、海报墙、剧集分季分集、播放进度和外部客户端访问能力。

当前兼容重点：

- 媒体库、合集、季、集的层级输出。
- 海报、背景图、简介、年份、评分等基础元数据输出。
- 视频流地址、HTTP Range、播放进度与继续观看。
- 外部客户端登录、媒体浏览和播放所需的 Emby/Jellyfin 风格接口。

仍在持续补齐：

- 更完整的 Emby/Jellyfin 设备能力协商。
- 更细的转码 Profile 与字幕能力声明。
- 多用户权限、媒体库过滤与播放历史同步。
- 与订阅下载、自动整理、洗版规则之间的闭环联动。

> MoviePilot 项目使用 GPL-3.0 许可，本项目只参考其公开产品思路与交互路径，不复制私有数据、密钥、站点账号或不兼容实现。

---

## 🧠 AI 与外部服务配置

在后台「外部 API 配置」中可配置：

| 服务 | 作用 |
| --- | --- |
| TMDb | 电影、剧集、海报、背景图、简介 |
| Bangumi | 动漫、番剧、中文条目 |
| TheTVDB | 剧集季集补充 |
| Fanart.tv | 高清 Logo、背景图、艺术图 |
| 豆瓣 | 中文影视搜索与推荐补充 |
| OpenAI Compatible | AI 搜索、推荐、运维助手 |

M-Team 建议使用 API Access Token：

```text
控制台 → 实验室 → 存取令牌
HTTP Header: x-api-key
```

不建议使用 Cookie 调用开放 API，避免账号风险。

---

## 🔐 隐私与安全

默认不会提交以下数据：

- `data/`、`cache/`、`logs/`
- `.tmp-deploy-data/`、`.tmp-deploy-server.*`
- `.mediastation.pid`
- `config.yaml`、`.env*`
- `*.db`、`*.db-wal`、`*.log`
- `web/dist/`、`node_modules/`、`bin/`
- API Key、Cookie、Token、密码、证书等敏感文件

提交前建议检查：

```bash
git status --short
git ls-files | grep -E 'data/|cache/|\.db|\.log|jwt_secret|config.yaml|\.env|token|apikey|password' || true
```

---

## ❓ 常见问题

### Q: 拉取 GHCR 镜像时出现 `EOF` 怎么办？

`EOF` 通常表示服务器到 GHCR 的网络连接中途断开，不是 compose 文件语法错误。建议按顺序处理：

```bash
# 1. 清理可能异常的 GHCR 登录状态
docker logout ghcr.io || true

# 2. 单独拉取镜像，确认是网络/ registry 问题还是 compose 问题
docker pull ghcr.io/shukebta/mediastation-go:latest

# 3. 如果是 x86_64/AMD64 主机，也可以显式指定平台重试
docker pull --platform linux/amd64 ghcr.io/shukebta/mediastation-go:latest

# 4. 拉取成功后再启动
docker compose up -d
```

如果服务器在国内网络或 NAS 网络环境中，建议为 Docker daemon 配置可访问 GHCR 的代理；仅设置终端代理通常不一定会被 Docker 服务进程继承。默认 compose 已使用 `pull_policy: missing`，避免容器重启时反复访问 GHCR。

另外，你示例中的路径如果是 NAS 绝对路径，建议写成 `/vol1/...`，不要写 `./vol1/...`；前者是系统根目录路径，后者是当前 compose 目录下面的相对路径。

### Q: Docker 部署后浏览器打不开？

检查容器状态和端口：

```bash
docker ps
docker logs -f mediastation-go
```

确认访问的是宿主机端口，例如 `http://192.168.1.4:18080`。

### Q: 外部客户端提示服务器未响应？

优先检查防火墙、Docker 端口映射、反向代理和局域网 IP。容器内监听 `8080`，宿主机默认映射为 `18080`。

### Q: 媒体库没有海报？

请确认：

1. 本地是否有 `poster.jpg`、`fanart.jpg`、NFO。
2. TMDb / Bangumi / 豆瓣是否可连接。
3. 代理是否正确配置。
4. 媒体文件名是否包含清晰标题、年份、季集信息。

### Q: 下载任务为什么不显示原始链接？

PT 下载 URL 常包含私有 Token。下载中心和订阅管理会主动隐藏原始 URL，只显示安全标题、海报、速度、进度、体积等信息。

### Q: 可以只保留哪个 Docker 包？

保留 `ghcr.io/shukebta/mediastation-go` 即可；旧的 `mediastationgo` 包可以删除，避免用户拉错镜像。

---

## 🗺️ 路线图

- 更完整的 Emby/Jellyfin 客户端兼容。
- 更强的成人内容本地元数据和公开页面补全。
- 更细粒度的订阅洗版和下载后整理规则。
- 更完善的移动端/电视端交互。
- 插件化站点适配器和通知渠道。
- 更完整的端到端测试与截图自动化。

---

## 🤝 贡献

欢迎提交 Issue、Pull Request、站点适配、刮削规则、UI 改进与文档修正。

建议贡献前先运行：

```bash
go test ./...
cd web && npm run build
```

---

## 👥 开发群组

- Telegram：<https://t.me/MediaStationGo>

---

## 🍜 赞赏

如果这个项目节省了你的时间，欢迎请作者吃桶泡面。

<img width="200" height="200" alt="微信赞赏码" src="https://github.com/user-attachments/assets/d6077de5-8305-400d-8b82-470ef05d926e" />

---

## ⭐ Star History

<a href="https://www.star-history.com/?repos=ShukeBta%2FMediaStationGo&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=ShukeBta/MediaStationGo&type=date&legend=top-left" />
 </picture>
</a>

---

## 📄 许可证与非商用声明

本项目基础许可证遵循 `GPL-3.0`，详见 [LICENSE](LICENSE)。项目维护者同时声明并倡议：

- 本项目主要面向个人学习、家庭 NAS、自建影音、非商业研究与社区共建场景。
- 未经作者明确书面许可，不得将本项目或衍生版本用于商业售卖、商业托管、付费 SaaS、预装售卖设备、闭源二次分发或其他商业化牟利用途。
- 如需商业合作、企业内部部署、定制开发、集成发行或商业授权，请先联系作者确认授权边界。
- 若 README 的非商用声明与 `GPL-3.0` 正式许可文本存在解释差异，代码授权以 [LICENSE](LICENSE) 文件为准，商业使用请额外取得作者许可。

---

<p align="center">Made with ❤️ by ShukeBta</p>
