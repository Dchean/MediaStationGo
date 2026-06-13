# MediaStationGo

<p align="center">
  <img src="web/public/favicon.svg" width="96" height="96" alt="MediaStationGo Logo" />
</p>

<h3 align="center">轻量、好看、适合 NAS 的私人媒体中心</h3>

<p align="center">
  <strong>Docker 一键部署 · 多用户管理 · 媒体库 · 刮削 · 下载整理 · Emby 协议兼容 · 网盘播放</strong>
</p>

<p align="center">
  <a href="README_EN.md">English</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="#docker-compose-推荐部署">Docker 部署</a> ·
  <a href="#常见问题">常见问题</a> ·
  <a href="https://mgo.3jzs.com">在线演示</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111827" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square" />
</p>

---

## 一句话介绍

MediaStationGo 是一个给个人、家庭 NAS、影音爱好者使用的媒体管理系统。

你可以用它做这些事：

- 把电影、电视剧、动漫、综艺、音乐整理成漂亮的媒体库。
- 创建多个用户账号，给家人、朋友或不同设备分别管理登录和权限。
- 自动识别文件、补全海报、简介、年份、季集信息。
- 在网页里播放，也可以直接用 MediaStationGo 账号登录 Infuse、VidHub、SenPlayer、Emby 客户端等支持 Emby 协议的第三方播放器。
- 连接 qBittorrent，做搜索、订阅、下载、整理入库。
- 接入 OpenList / CloudDrive2 / WebDAV 等外部存储，支持 STRMURL 与 302 反代播放。
- 在 NAS、小主机、VPS、Windows Docker Desktop 上用 Docker Compose 快速运行。

> 项目还在快速迭代。重要数据都在 `data` 目录，升级前建议先备份。

---

## 核心特点

- **一个服务端，多端播放**：只部署一次 MediaStationGo，不需要再重复部署 Emby 服务端。
- **兼容 Emby 协议客户端**：第三方播放器按 Emby/Jellyfin 方式添加服务器，直接用 MediaStationGo 账号密码登录。
- **多用户管理**：支持管理员、普通用户、账号启停、有效期、设备管理、Bot 注册/兑换码等家庭共享场景。
- **本地媒体 + 网盘媒体统一管理**：本地硬盘、下载目录、OpenList、CloudDrive2、WebDAV 等资源可以放在同一个后台管理。
- **下载到入库一条龙**：连接 qBittorrent 后，可做搜索、订阅、下载完成整理、刮削入库。
- **NAS 友好**：Docker Compose 部署简单，数据集中在 `data/`，适合低功耗 NAS 和小主机长期运行。

---

## 适合谁

- **新手用户**：只想复制一份 `docker-compose.yml`，改几个路径就跑起来。
- **NAS 用户**：想用低资源占用的媒体中心管理本地硬盘和网盘资源。
- **PT / 下载用户**：想把下载、整理、刮削、播放放到一个后台。
- **外部播放器用户**：想用一个 MediaStationGo 账号登录支持 Emby 协议的第三方播放器 APP。
- **家庭共享用户**：想给不同用户分配账号，不想为每个人重复搭一套媒体服务。
- **开发者**：想研究 Go + React 的自托管媒体项目。

---

## 在线演示

- 地址：[https://mgo.3jzs.com](https://mgo.3jzs.com)
- 账号：`admin`
- 密码：`admin123`

> 演示站只用于看功能，请不要填写私人 API Key、站点 Cookie 或真实隐私信息。

---

## 快速开始

最推荐新手使用 Docker Compose。不要一开始就折腾 `.env`、裸机运行、源码编译。

```bash
mkdir -p MediaStationGo
cd MediaStationGo
curl -fsSL https://raw.githubusercontent.com/ShukeBta/MediaStationGo/main/docker-compose.yml -o docker-compose.yml
```

编辑 `docker-compose.yml`：

```bash
vi docker-compose.yml
```

然后启动：

```bash
docker compose up -d
```

浏览器打开：

```text
http://服务器IP:18080
```

默认登录：

```text
账号：admin
密码：admin123
```

---

## Docker Compose 推荐部署

仓库里的 `docker-compose.yml` 已经是最简单模板：默认不用 `.env`。

### 镜像地址怎么选

两种镜像地址都可以用，选择其中一种写到 `image:` 即可：

| 来源 | 镜像地址 | 适合场景 |
| --- | --- | --- |
| Docker Hub | `shukbet/mediastationgo:latest` | 最常用，直接 `docker compose pull` |
| GitHub 仓库镜像 GHCR | `ghcr.io/shukebta/mediastation-go:latest` | Docker Hub 拉取慢或不可用时备用 |

如果想固定版本，推荐使用 Docker Hub 已发布版本标签，例如：

```yaml
image: shukbet/mediastationgo:MediaStationGo-v0.0.72
```

GHCR 固定版本请以仓库 Packages 页面已发布的标签为准；如果只想简单部署，直接使用 `latest` 即可。

手动拉取示例：

```bash
# Docker Hub
docker pull shukbet/mediastationgo:latest

# GitHub 仓库镜像
docker pull ghcr.io/shukebta/mediastation-go:latest
```

你只需要重点看 `volumes` 这一段：

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - ./media:/media:ro
  - ./downloads:/downloads
```

含义很简单：

| 左边 | 右边 | 说明 |
| --- | --- | --- |
| `./data` | `/data` | 程序数据库、配置、账号信息；一定要备份 |
| `./cache` | `/cache` | 缓存目录；可清理 |
| `./media` | `/media` | 媒体库目录；网页里添加媒体库时填 `/media/...` |
| `./downloads` | `/downloads` | 下载目录；文件管理和自动整理会用 |

如果你的媒体在 NAS 真实目录，例如：

```text
/vol1/1000/Media
/vol1/1000/Downloads
```

就把 compose 改成：

```yaml
volumes:
  - ./data:/data
  - ./cache:/cache
  - /vol1/1000/Media:/media:ro
  - /vol1/1000/Downloads:/downloads

environment:
  MEDIASTATION_MEDIA_DIR: /vol1/1000/Media
  MEDIASTATION_DOWNLOAD_DIR: /vol1/1000/Downloads
```

注意：

- `volumes` 左边是宿主机 / NAS 的真实路径。
- `volumes` 右边是容器里的路径，建议固定用 `/media` 和 `/downloads`。
- 在网页里新建媒体库时，填容器路径，例如 `/media/电影`、`/media/电视剧`。
- 不要把 NAS 绝对路径写成 `./vol1/...`，`./` 表示当前部署目录下面的相对路径。
- Windows Docker Desktop 可以写成 `D:/Media:/media:ro`、`D:/Downloads:/downloads`。

### 最简单 compose 示例

仓库根目录的 `docker-compose.yml` 就是这个思路。你也可以手动创建：

```yaml
services:
  mediastation-go:
    # 镜像二选一：
    # Docker Hub：
    image: shukbet/mediastationgo:latest
    # GitHub 仓库镜像 GHCR：
    # image: ghcr.io/shukebta/mediastation-go:latest

    container_name: mediastation-go
    restart: unless-stopped
    init: true

    # 访问端口：浏览器打开 http://服务器IP:18080
    ports:
      - "18080:8080"

    # 让容器可以访问宿主机上的 qBittorrent：
    # qB 地址可填 http://host.docker.internal:8085
    extra_hosts:
      - "host.docker.internal:host-gateway"

    volumes:
      # 程序数据，升级前备份这个目录。
      - ./data:/data
      - ./cache:/cache

      # 新手先用当前目录下的 media/downloads。
      # NAS 用户把左边改成真实绝对路径。
      - ./media:/media:ro
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

      # 如果上面的 ./media / ./downloads 改成 NAS 真实路径，
      # 这里也改成同样的宿主机真实路径。
      MEDIASTATION_MEDIA_DIR: ./media
      MEDIASTATION_MEDIA_CONTAINER_DIR: /media
      MEDIASTATION_DOWNLOAD_DIR: ./downloads
      MEDIASTATION_DOWNLOAD_CONTAINER_DIR: /downloads
```

---

## 首次进入后怎么配置

1. **新建媒体库**
   - 进入「媒体库」页面。
   - 路径填容器路径，例如 `/media/电影`。
   - 点扫描。

2. **配置下载器**
   - 进入「下载器管理」。
   - 如果 qBittorrent 在宿主机上，地址通常填 `http://host.docker.internal:8085`。

3. **配置刮削源**
   - 进入「系统设置 / 外部 API」。
   - 按需填写 TMDb、Bangumi、TheTVDB、Fanart、豆瓣等配置。

4. **配置外部播放器**
   - 第三方客户端按 Emby/Jellyfin 方式添加服务器。
   - 地址填 `http://服务器IP:18080`。
   - 用户名和密码填 MediaStationGo 后台创建的账号，不需要单独部署 Emby 服务端。
   - 管理员可以在后台/Bot 创建普通用户，让不同用户用自己的账号登录第三方播放器。

5. **配置网盘播放**
   - 进入「外部存储」配置 OpenList、CloudDrive2、WebDAV 等。
   - 后台播放策略可以选择 STRMURL 或 302 反代。
   - 开启哪个，就优先走哪个；都关闭时走普通服务端播放链路。

---

## 更新、备份、日志

### 更新

```bash
docker compose pull
docker compose up -d
```

### 查看日志

```bash
docker logs -f mediastation-go
```

### 备份

重点备份：

```text
data/
```

这里面有数据库、用户、设置、部分运行状态。`cache/` 通常不用备份。

### 停止

```bash
docker compose down
```

---

## 常见问题

### 1. 页面打不开？

先看容器是否启动：

```bash
docker ps
docker logs --tail=100 mediastation-go
```

确认浏览器访问的是：

```text
http://服务器IP:18080
```

### 2. 媒体库扫描不到文件？

最常见原因是路径写错。

- Docker `volumes` 右边是 `/media`。
- 网页媒体库路径就应该填 `/media/电影`，不要填 NAS 原始路径。
- 如果 qB 下载目录是 `/downloads`，自动整理源目录也优先填 `/downloads`。

### 3. qBittorrent 连不上？

如果 qB 在宿主机上，地址试试：

```text
http://host.docker.internal:8085
```

如果 qB 在另一台机器上，填那台机器的局域网 IP。

### 4. NAS CPU 占用高？

建议先在系统设置里确认：

- `ffprobe.max_concurrent` 设为 `1`。
- 自动整理、扫描后刮削、启动后扫描网盘按需开启。
- 大媒体库不要频繁全量扫描，优先手动扫描或夜间同步。

### 5. 要不要用 `.env`？

新手不建议。直接改 `docker-compose.yml` 最直观。

`.env` 适合进阶用户在多台机器复用同一份 compose。仓库保留 `docker-compose.simple.env.example`，但它不是推荐主线。

---

## 功能概览

| 分类 | 功能 |
| --- | --- |
| 媒体库 | 电影、电视剧、动漫、综艺、音乐、成人内容 |
| 元数据 | NFO、本地图片、TMDb、TheTVDB、Bangumi、豆瓣、Fanart、JavBus/JavDB |
| 播放 | Web 播放、Range 拖动、HLS 转码、直链、STRMURL、302 反代 |
| 外部客户端 | Emby 协议兼容接口，MediaStationGo 账号可直接登录第三方播放器 |
| 用户管理 | 多用户、管理员/普通用户、账号有效期、设备管理、Bot 注册与兑换码 |
| 下载 | qBittorrent、站点搜索、订阅、下载完成后整理 |
| 文件管理 | 浏览、整理、复制、移动、硬链接、软链接 |
| 运维 | 任务队列、回收站、重复文件、通知渠道、运行日志 |
| AI | OpenAI Compatible API、AI 搜索、推荐、助手 |

---

## 截图

<details open>
<summary><strong>界面预览</strong></summary>

| 登录 | 首页 |
| --- | --- |
| <img src="docs/screenshots/00-login.jpg" alt="登录" width="100%"> | <img src="docs/screenshots/01-home.jpg" alt="首页" width="100%"> |

| 媒体库 | 播放器 |
| --- | --- |
| <img src="docs/screenshots/02-libraries.jpg" alt="媒体库" width="100%"> | <img src="docs/screenshots/06-player.jpg" alt="播放器" width="100%"> |

</details>

---

## 开发者运行

普通用户请优先使用 Docker。开发者可以这样运行：

```bash
go run ./cmd/server
```

前端：

```bash
cd web
npm install
npm run dev
```

测试：

```bash
go test ./...
cd web && npm run build
```

---

## 社区与友链

- Telegram MediaStationGo交流群：<https://t.me/MediaStationGo>
- NodeSeek：[https://www.nodeseek.com/](https://www.nodeseek.com/)
- LINUX DO：[https://linux.do/](https://linux.do/)

---

## 赞赏

如果这个项目节省了你的时间，欢迎请作者吃桶泡面。

<img width="200" height="200" alt="微信赞赏码" src="https://github.com/user-attachments/assets/d6077de5-8305-400d-8b82-470ef05d926e" />

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

## 许可证与非商用声明

本项目基础许可证遵循 `GPL-3.0`，详见 [LICENSE](LICENSE)。

项目维护者同时声明并倡议：

- 本项目主要面向个人学习、家庭 NAS、自建影音、非商业研究与社区共建场景。
- 未经作者明确书面许可，不得将本项目或衍生版本用于商业售卖、商业托管、付费 SaaS、预装售卖设备、闭源二次分发或其他商业化牟利用途。
- 如需商业合作、企业内部部署、定制开发、集成发行或商业授权，请先联系作者确认授权边界。
- 若 README 的非商用声明与 `GPL-3.0` 正式许可文本存在解释差异，代码授权以 [LICENSE](LICENSE) 文件为准，商业使用请额外取得作者许可。

---

<p align="center">Made with ❤️ by ShukeBta</p>
