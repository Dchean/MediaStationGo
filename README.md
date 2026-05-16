<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://img.shields.io/badge/MediaStationGo-你的私人媒体中心-111827?style=for-the-badge&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0id2hpdGUiIGQ9Ik0xMiAyQzYuNDggMiAyIDYuNDggMiAxMnM0LjQ4IDEwIDEwIDEwIDEwLTQuNDggMTAtMTBTMTcuNTIgMiAxMiAyem0tMiAxNWwtNS01IDEuNDEtMS40MUwxMCAxNC4xN2w3LjU5LTcuNTlMMTkgOGwtOSA5eiIvPjwvc3ZnPg=="/>
    <img src="https://img.shields.io/badge/MediaStationGo-你的私人媒体中心-1F2937?style=for-the-badge&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0id2hpdGUiIGQ9Ik0xMiAyQzYuNDggMiAyIDYuNDggMiAxMnM0LjQ4IDEwIDEwIDEwIDEwLTQuNDggMTAtMTBTMTcuNTIgMiAxMiAyem0tMiAxNWwtNS01IDEuNDEtMS40MUwxMCAxNC4xN2w3LjU5LTcuNTlMMTkgOGwtOSA5eiIvPjwvc3ZnPg=="/>
  </picture>
</p>

<h3 align="center"><samp><a href="https://github.com/ShukeBta/MediaStation">MediaStation</a> 的 Go 语言重写版</samp></h3>
<h6 align="center"><samp>轻量 · 快速 · 单二进制部署 · NAS 友好</samp></h6>

<p align="center">
  <a href="README_EN.md"><img src="https://img.shields.io/badge/English-README-blue?style=flat-square" alt="English"></a>
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
  <b>📖 <a href="#-为什么选择-mediastationgo">为什么选择</a></b>
  &nbsp;·&nbsp;
  <b>🚀 <a href="#-快速开始">快速开始</a></b>
  &nbsp;·&nbsp;
  <b>✨ <a href="#-功能特性">功能特性</a></b>
  &nbsp;·&nbsp;
  <b>🏗️ <a href="#-项目结构">项目结构</a></b>
  &nbsp;·&nbsp;
  <b>⚙️ <a href="#-配置说明">配置说明</a></b>
  &nbsp;·&nbsp;
  <b>🗺️ <a href="#-路线图">路线图</a></b>
</p>

---

## 🤔 为什么选择 MediaStationGo？

> MediaStationGo 是 [MediaStation](https://github.com/ShukeBta/MediaStation) 从零开始的 Go 语言重写版，在保持完整功能体验的同时，将部署复杂度压缩到极致。

<table>
<tr>
<td width="50%">

### 原版 MediaStation
- 🐍 Python / FastAPI + Vue
- 📦 依赖 Python 运行时和虚拟环境
- 🐳 必须使用 Docker 或复杂的 Python 环境
- 📊 部署包 > 500 MB
- 🔧 需要 pip / npm 双构建链

</td>
<td width="50%">

### MediaStationGo ✨
- 🚀 Go 1.25 + React 18
- 📦 **单一静态二进制**（约 30 MB）
- 🐳 可选 Docker，也支持裸机直接运行
- 🔥 零外部依赖（CGO 禁用）
- ⚡ `go build` 一条命令编译

</td>
</tr>
</table>

| 指标 | 原版 | MediaStationGo |
|------|:----:|:----:|
| 二进制体积 | — | ≈ 30 MB |
| 内存占用（空闲） | ~200 MB | ~30 MB |
| 冷启动时间 | ~3s | ~0.3s |
| 部署步骤 | 5+ | 1 |
| 前端体积 (gzip) | ~250 KB | ~83 KB |

---

## ✨ 功能特性

<details open>
<summary><b>🔐 认证与用户管理</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| JWT 双角色认证 | admin / user 角色隔离，支持 refresh token |
| 一键管理员创建 | 首次运行自动种子 `admin` / `admin123` |
| 个人信息管理 | 邮箱、头像、密码修改 |
| 用户管理面板 | 角色提升 / 降级、账户启用 / 禁用 |
| 审计日志 | 记录登录、媒体库操作、下载等敏感行为 |

</details>

<details open>
<summary><b>📚 媒体库管理</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| 媒体库 CRUD | 支持 movie / tv / anime / music 四种类型 |
| 递归扫描 | 文件系统遍历 + ffprobe 元数据提取 |
| 智能文件名清洗 | 年份 + 季/集号自动识别 |
| 多源刮削 | TMDb → TheTVDB → Bangumi 链式刮削 |
| 高清海报升级 | Fanart.tv 可选降级 |
| 图片代理 | TMDb / Bangumi / 豆瓣 / Fanart 代理 + 磁盘缓存 |
| 电视剧分组 | 按季分组 + 剧集列表视图 |
| 实时文件监听 | fsnotify 驱动，5 秒防抖合并 |

</details>

<details open>
<summary><b>🎬 播放体验</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| 直链播放 | HTTP Range 支持，拖动秒开 |
| HLS 按需转码 | 单文件单作业，支持硬件加速 |
| 外挂字幕 | .srt / .vtt / .ass / .ssa 自动识别 → WebVTT 实时转换 |
| 续播 | 每 10 秒自动存位，首页「继续观看」 |
| 收藏 & 播放列表 | 一键切换收藏 / 有序播放列表 |

</details>

<details open>
<summary><b>🌐 PT 站点管理</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| 6 种站点类型 | nexusphp · gazelle · unit3d · mteam · discuz · custom_rss |
| 3 种认证方式 | Cookie / API Key / Auth Header |
| 站点配置 | 完整 CRUD + 连接测试 + 启用开关 |
| 跨站搜索 | 一键搜索所有已配置站点的种子 |
| 扩展配置 | Extra JSON：UA / RSS URL / 超时 / 优先级 / 代理 / 下载器 |

</details>

<details open>
<summary><b>🤖 自动化</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| 下载集成 | qBittorrent Web UI API（添加 / 列表 / 删除） |
| RSS 订阅 | 正则过滤 + GUID 去重 + 10 分钟轮询 |
| 文件整理 | 下载完成后自动分类 move / copy / hardlink / symlink |

</details>

<details open>
<summary><b>📊 运维监控</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| 实时事件推送 | WebSocket 推送扫描 / 刮削 / 转码 / 下载 / 订阅状态 |
| 系统仪表盘 | CPU / 内存 / 磁盘 / 媒体库数量 / Goroutines |
| 任务面板 | 实时展示 ffmpeg 作业 + qBittorrent 种子 |
| NFO 导出 | Kodi / Jellyfin 兼容格式，单文件或整库 |
| 硬件加速 | Software / NVENC / Intel QSV / VAAPI 编码器配置 |
| GitHub Actions | CI 自动化 + GHCR 多架构镜像发布 |

</details>

<details open>
<summary><b>🧠 AI 智能</b></summary>
<br>

| 功能 | 说明 |
|------|------|
| TMDb 发现 | 首页热门 / 流行推荐 |
| AI 搜索 | OpenAI 兼容接口 → 自然语言转结构化查询 |
| AI 推荐 | 基于观看历史的个性化推荐 |

</details>

---

## 🚀 快速开始

### 🐳 Docker 部署（推荐）

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# 编辑 docker-compose.yml，将媒体目录挂载到 /media
docker compose up -d
```

> 🌐 打开 <http://localhost:8080>，使用 `admin` / `admin123` 登录。

### 💻 裸机部署

| 前置要求 | 版本 |
|----------|------|
| Go | ≥ 1.25 |
| Node.js | ≥ 20 |
| FFmpeg | 任意 |

```bash
git clone https://github.com/ShukeBta/MediaStationGo.git
cd MediaStationGo

# 编译后端 + 构建前端
make build

# 启动服务
./bin/mediastation-go
```

### 🛠️ 本地开发

```bash
# 终端 1：Go 后端（端口 8080，DEBUG 模式）
make dev

# 终端 2：Vite 前端（端口 3000，API 自动代理）
make dev-web
```

---

## ⚙️ 配置说明

配置加载优先级：**默认值** < `config.yaml` < `config/*.yaml` < **环境变量**（`MEDIASTATION_` 前缀）

### 常用环境变量

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `MEDIASTATION_APP_PORT` | `8080` | HTTP 监听端口 |
| `MEDIASTATION_APP_DATA_DIR` | `./data` | 数据目录（DB / 缓存 / JWT） |
| `MEDIASTATION_APP_WEB_DIR` | `./web/dist` | 前端 SPA 静态文件 |
| `MEDIASTATION_DATABASE_DB_PATH` | `./data/mediastation.db` | SQLite 数据库路径 |
| `MEDIASTATION_SECRETS_JWT_SECRET` | *(自动生成)* | JWT 签名密钥 |
| `MEDIASTATION_SECRETS_TMDB_API_KEY` | *(空)* | TMDb 刮削（必填） |
| `MEDIASTATION_SECRETS_BANGUMI_ACCESS_TOKEN` | *(空)* | Bangumi 速率提升 |
| `MEDIASTATION_APP_CORS_ORIGINS` | *(空)* | 跨域白名单（JSON 数组） |
| `ADMIN_INITIAL_PASSWORD` | `admin123` | 初始管理员密码 |

### 运行时设置

管理后台 → 系统设置，存储在 `settings` 表中：

| 键 | 说明 |
|----|------|
| `qbittorrent.url` | qBittorrent Web UI 地址 |
| `qbittorrent.username` | 用户名 |
| `qbittorrent.password` | 密码 |
| `qbittorrent.savepath` | 默认保存路径（可选） |

> 💡 修改后点击 **下载 → 重新加载配置** 或 `POST /api/downloads/reload`。

📖 完整配置模板：[`config.example.yaml`](config.example.yaml)

---

## 🏗️ 项目结构

```
MediaStationGo/
├── cmd/server/main.go          ← 应用入口
├── internal/
│   ├── config/                 ← Viper 配置层
│   ├── database/               ← GORM + SQLite (WAL) 初始化
│   ├── model/                  ← 数据模型 + AutoMigrate 注册
│   ├── repository/             ← 数据访问层
│   ├── service/                ← 业务逻辑（核心）
│   │   ├── auth.go             登录 / 注册 / JWT
│   │   ├── media.go            媒体库 + 媒体 CRUD
│   │   ├── scanner.go          文件扫描 + ffprobe
│   │   ├── scraper.go          刮削调度 + 文件名清洗
│   │   ├── tmdb.go / bangumi.go  第三方数据源
│   │   ├── site.go             站点管理 CRUD + 跨站搜索
│   │   ├── site_adapter.go     6 种 PT 站点适配器
│   │   ├── stream.go           直链 + HLS 播放
│   │   ├── transcoder.go       ffmpeg 转码作业管理
│   │   ├── subtitle.go         外挂字幕 → WebVTT
│   │   ├── image_proxy.go      图片代理缓存
│   │   ├── playback.go         播放历史 / 收藏 / 播放列表
│   │   ├── watcher.go          fsnotify 文件监听
│   │   ├── qbittorrent.go      qBittorrent API 客户端
│   │   ├── downloads.go        下载管理
│   │   ├── subscription.go     RSS 订阅轮询
│   │   ├── organizer.go        媒体文件自动整理
│   │   ├── stats.go            系统仪表盘
│   │   ├── profile.go          用户信息
│   │   ├── audit.go            审计日志
│   │   └── ws_hub.go           WebSocket 发布/订阅
│   ├── middleware/             ← JWT / CORS / admin 中间件
│   └── handler/                ← HTTP 路由（按功能拆分）
├── web/                        ← React 18 + Vite + Tailwind CSS
│   ├── src/api/                axios 接口封装
│   ├── src/components/         通用组件（Card / Layout / APIConfigsPanel）
│   ├── src/hooks/              useWebSocket 等
│   ├── src/pages/              首页 · 媒体库 · 搜索 · 播放器 · 下载 · 管理 · 站点
│   ├── src/stores/             Zustand（auth）
│   └── src/types/              前端类型定义
├── Dockerfile                  ← 多阶段多架构构建
├── docker-compose.yml          ← NAS 一键部署
├── Makefile                    ← build / dev / docker / test
├── config.example.yaml         ← 完整配置模板
└── .github/workflows/          ← CI + GHCR 发布
```

---

## 🗺️ 路线图

| 特性 | 状态 |
|------|:---:|
| Jellyfin / Emby 双向兼容层 | 🔨 进行中 |
| DLNA / Chromecast 投屏 | 📋 计划中 |
| 在线字幕搜索 | 📋 计划中 |
| 多码率 ABR 转码 | 📋 计划中 |
| STRM 直链播放 (WebDAV / Alist / S3) | ✅ 已完成 |

---

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！在提交 PR 之前请阅读 [贡献指南](CONTRIBUTING.md)。

---

## 📄 许可证

[GNU General Public License v3.0](LICENSE)

> ⚠️ 许可证授权功能由独立服务器 [MediaStationLicenseServer](https://github.com/ShukeBta/MediaStationLicenseServer) 管理，本项目不内置授权/验权逻辑。

---

<p align="center">
  <sub>Made with ❤️ by MediaStationGo Team</sub>
</p>
