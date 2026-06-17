# 旧版完整功能迁移清单

> 基于旧版 Python/FastAPI + Vue 3 源代码分析，供 MediaStationGo（Go/Gin + React）重写参考。
>
> 分析日期：2025-07-09

---

## 目录

- [1. 项目概览](#1-项目概览)
- [2. 后端功能清单](#2-后端功能清单)
  - [2.1 用户与认证模块](#21-用户与认证模块)
  - [2.2 媒体库模块](#22-媒体库模块)
  - [2.3 播放模块](#23-播放模块)
  - [2.4 下载模块](#24-下载模块)
  - [2.5 订阅与站点模块](#25-订阅与站点模块)
  - [2.6 系统模块](#26-系统模块)
  - [2.7 管理后台模块](#27-管理后台模块)
  - [2.8 统计模块](#28-统计模块)
  - [2.9 播放列表模块](#29-播放列表模块)
  - [2.10 STRM 文件支持模块](#210-strm-文件支持模块)
  - [2.11 DLNA/投屏模块](#211-dlna投屏模块)
  - [2.12 授权管理模块](#212-授权管理模块)
  - [2.13 Emby API 兼容层](#213-emby-api-兼容层)
  - [2.14 发现/探索模块](#214-发现探索模块)
- [3. 数据模型清单](#3-数据模型清单)
- [4. 前端功能清单](#4-前端功能清单)
- [5. 部署配置清单](#5-部署配置清单)
- [6. 中间件与基础设施](#6-中间件与基础设施)
- [7. 配置系统](#7-配置系统)
- [8. 技术栈对照表](#8-技术栈对照表)

---

## 1. 项目概览

### 原版架构
- **后端**: Python 3.11+ / FastAPI / SQLAlchemy (async) / APScheduler
- **前端**: Vue 3 + Pinia + Vue Router + TypeScript
- **数据库**: SQLite（默认）/ PostgreSQL（可选）
- **部署**: Docker / Nginx 反向代理 / Helm Chart

### 核心定位
MediaStationGo 是一个轻量级家庭媒体服务器，融合 **媒体播放 + 自动化订阅下载 + 多平台资源聚合**。

---

## 2. 后端功能清单

### 2.1 用户与认证模块

> 源文件：`backend/app/user/` (router.py, service.py, repository.py, auth.py, models.py, schemas.py)
> 依赖注入：`backend/app/deps.py`

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 用户登录 | POST | `/api/auth/login` | 用户名+密码登录，返回 JWT access_token + refresh_token | 公开 |
| Token 刷新 | POST | `/api/auth/refresh` | 通过 refresh_token 获取新的 access_token | 公开 |
| 获取当前用户 | GET | `/api/auth/me` | 返回当前登录用户信息 | 登录 |
| 修改密码 | POST | `/api/auth/change-password` | 当前用户修改自己的密码 | 登录 |
| 更新资料 | PATCH | `/api/auth/profile` | 更新头像等个人资料 | 登录 |
| 获取权限 | GET | `/api/auth/permissions` | 获取当前用户的功能权限列表 | 登录 |
| 用户列表 | GET | `/api/users` | 获取所有用户列表 | 管理员 |
| 创建用户 | POST | `/api/users` | 创建用户（免费版限30人） | 管理员 |
| 更新用户 | PUT | `/api/users/{id}` | 更新用户信息 | 管理员 |
| 删除用户 | DELETE | `/api/users/{id}` | 删除用户 | 管理员 |
| 获取用户权限 | GET | `/api/users/{id}/permissions` | 获取指定用户的功能权限 | 管理员 |
| 更新用户权限 | PUT | `/api/users/{id}/permissions` | 更新指定用户的功能权限 | 管理员 |
| 重置用户权限 | POST | `/api/users/{id}/permissions/reset` | 重置为默认权限 | 管理员 |
| 系统配置(用户) | GET | `/api/system/config` | 获取系统级用户配置（FREE/PLUS） | 管理员 |
| 更新系统配置 | PUT | `/api/system/config` | 更新系统配置（tier/最大用户数） | 管理员 |
| 观看历史统计 | GET | `/api/watch-history/stats` | 获取当前用户观看历史统计 | 登录 |
| 观看历史列表 | GET | `/api/watch-history` | 分页获取观看历史 | 登录 |
| 继续观看列表 | GET | `/api/watch-history/continue` | 获取未看完的媒体列表 | 登录 |
| 删除单条历史 | DELETE | `/api/watch-history/{id}` | 删除单条历史（管理员可删任何人的） | 登录 |
| 清空历史 | DELETE | `/api/watch-history` | 清空历史（可指定媒体ID） | 登录 |

**认证机制细节**：
- JWT (HS256) access_token（60分钟）+ refresh_token（30天）
- 密码哈希：pbkdf2_sha256
- 依赖注入：`get_current_user`, `require_admin`, `require_permission(permission_field)`, `get_user_permissions`

**权限系统（19 项细粒度权限）**：
- 基础权限（默认开启）：`can_view_dashboard`, `can_play_media`, `can_cast`, `can_external_player`, `can_favorite`, `can_view_history`
- 受限功能（默认关闭）：`can_edit_media`, `can_rescrape`, `can_use_ai`, `can_capture_frames`, `can_manage_downloads`, `can_view_discover`, `can_manage_subscriptions`, `can_manage_sites`, `can_use_ai_assistant`, `can_manage_users`, `can_manage_files`, `can_manage_strm`, `can_access_settings`
- Plus 用户（tier=plus）自动获得所有权限
- 管理员（role=admin）自动获得所有权限

**用户角色与层级**：
- 角色：admin / user
- 层级：free / plus（免费版限30用户，Plus 无限）

---

### 2.2 媒体库模块

> 源文件：`backend/app/media/` (router.py, service.py, repository.py, models.py, schemas.py, scanner.py, scraper.py, organizer.py, watcher.py, subtitle_service.py, duplicate.py, image_proxy.py, bangumi_scraper.py, douban_scraper.py, parse_code.py, providers/)

#### 2.2.1 媒体库管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 媒体库列表 | GET | `/api/libraries` | 获取所有媒体库 | 登录 |
| 创建媒体库 | POST | `/api/libraries` | 创建新媒体库 | 管理员 |
| 扫描媒体库 | POST | `/api/libraries/{id}/scan` | 触发媒体库扫描+自动刮削 | 管理员 |
| 更新媒体库 | PUT | `/api/libraries/{id}` | 更新媒体库配置 | 管理员 |
| 删除媒体库 | DELETE | `/api/libraries/{id}` | 删除媒体库 | 管理员 |

#### 2.2.2 媒体条目管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 媒体列表 | GET | `/api/media` | 分页+多维筛选（类型/类型/年份/评分/排序） | 登录 |
| 最近添加 | GET | `/api/media/recent` | 获取最近添加的媒体 | 登录 |
| 媒体统计 | GET | `/api/media/stats` | 获取媒体数量统计 | 登录 |
| 媒体详情 | GET | `/api/media/{id}` | 获取媒体详情（含季/集/字幕） | 登录 |
| 删除媒体 | DELETE | `/api/media/{id}` | 删除媒体条目 | 管理员 |
| 更新媒体 | PUT | `/api/media/{id}` | 手动编辑媒体元数据 | 管理员 |
| 视频截帧 | GET | `/api/media/{id}/thumbnail` | FFmpeg 视频截帧（缩略图） | 登录 |

#### 2.2.3 元数据刮削

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 刮削媒体 | POST | `/api/media/{id}/scrape` | 手动触发刮削（可指定 TMDb ID） | 管理员 |
| 搜索 TMDb | GET | `/api/search/tmdb` | 搜索 TMDb 数据库 | 登录 |
| 搜索豆瓣 | GET | `/api/search/douban` | 搜索豆瓣影视 | 登录 |
| 搜索 Bangumi | GET | `/api/search/bangumi` | 搜索 Bangumi 动漫数据库 | 登录 |
| Adult 刮削测试 | POST | `/api/media/scrape/test` | 测试 Adult Provider 刮削 | 管理员 |

**元数据 Provider Chain（多源聚合）**：
- `TMDbProvider` — TMDb 主数据源（电影/剧集）
- `DoubanProvider` — 豆瓣中文元数据补充
- `BangumiProvider` — Bangumi 番剧/动画数据源
- `AdultProvider` — 18+ 番号刮削（多层 Fallback：JavBus → JavDB → 微服务）
- Provider Chain 支持优先级调度和自动降级

#### 2.2.4 搜索功能

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 全局搜索 | GET | `/api/search` | 搜索本地媒体库 | 登录 |
| 高级搜索 | GET | `/api/search/advanced` | 多条件组合搜索（标题/类型/年份/评分/分辨率/字幕） | 登录 |
| 混合搜索 | GET | `/api/search/mixed` | 并发搜索本地+TMDb | 登录 |

#### 2.2.5 推荐系统

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 智能推荐 | GET | `/api/recommend` | 基于高评分+热度的推荐 | 登录 |
| 相似推荐 | GET | `/api/recommend/similar/{id}` | 基于同类型/标签/年代的相似内容 | 登录 |

#### 2.2.6 字幕管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 字幕列表 | GET | `/api/media/{id}/subtitles` | 获取媒体字幕列表 | 登录 |
| 扫描外挂字幕 | POST | `/api/media/{id}/subtitles/scan` | 扫描外挂字幕文件 | 管理员 |
| 检测内嵌字幕 | POST | `/api/media/{id}/subtitles/extract` | 检测内嵌字幕流 | 管理员 |
| 提取内嵌字幕 | POST | `/api/media/{id}/subtitles/extract/{idx}` | 提取内嵌字幕为 SRT | 管理员 |
| 上传字幕 | POST | `/api/media/{id}/subtitles/upload` | 上传字幕文件 | 管理员 |
| 获取字幕内容 | GET | `/api/subtitles/{id}/content` | 获取字幕文件内容 | 登录 |
| 删除字幕 | DELETE | `/api/subtitles/{id}` | 删除字幕（可选删除文件） | 管理员 |

#### 2.2.7 收藏功能

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 添加收藏 | POST | `/api/media/{id}/favorite` | 添加收藏 | 登录 |
| 取消收藏 | DELETE | `/api/media/{id}/favorite` | 取消收藏 | 登录 |
| 检查收藏状态 | GET | `/api/media/{id}/favorite/status` | 检查是否已收藏 | 登录 |
| 收藏列表 | GET | `/api/favorites` | 分页获取收藏列表 | 登录 |

#### 2.2.8 重复检测

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 计算文件哈希 | POST | `/api/libraries/{id}/duplicates/hash` | 计算文件哈希（重复检测前置） | 管理员 |
| 检测重复 | POST | `/api/libraries/{id}/duplicates/scan` | 检测并标记重复文件 | 管理员 |
| 重复文件列表 | GET | `/api/libraries/{id}/duplicates` | 获取重复文件列表 | 登录 |
| 取消重复标记 | DELETE | `/api/libraries/{id}/duplicates` | 取消所有重复标记 | 管理员 |
| 取消单项重复标记 | POST | `/api/media/{id}/duplicate/unmark` | 取消单个条目重复标记 | 管理员 |

#### 2.2.9 文件整理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 整理文件 | POST | `/api/media/organize` | 手动触发文件整理到媒体库 | 管理员 |

#### 2.2.10 图片代理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 本地图片访问 | GET | `/api/media/image/{filename}` | 访问本地保存的图片（防路径遍历） | 登录 |
| 图片代理 | GET | `/api/media/proxy-image` | 代理外部图片（绕过防盗链） | 登录 |

---

### 2.3 播放模块

> 源文件：`backend/app/playback/` (router.py, external.py, service.py, transcoder.py, models.py)

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 播放信息 | GET | `/api/playback/{id}/info` | 获取媒体播放信息 | 登录 |
| 视频流 | GET | `/api/playback/{id}/stream` | 直接视频流（支持 Range 断点续传 + query token） | 登录 |
| 外部播放器直链 | GET | `/api/playback/{id}/external-url` | 生成带 token 的外部播放直链 | 登录 |
| 外部播放器协议 | GET | `/api/playback/{id}/external-players` | 生成各播放器协议直链（PotPlayer/VLC/IINA/Infuse/NPlayer/MX/MPV/MPC-HC） | 登录 |
| 外部播放流 | GET | `/api/playback/{id}/external-stream` | 外部播放器流式传输（支持 Range） | Token |
| HLS 播放列表 | GET | `/api/playback/hls/{job}/playlist.m3u8` | HLS m3u8 播放列表 | Token |
| HLS 分片 | GET | `/api/playback/hls/{job}/{segment}` | HLS ts 分片 | Token |
| 转码状态 | GET | `/api/playback/transcode/{job}/status` | 获取转码任务状态 | 登录 |
| 字幕文件 | GET | `/api/playback/subtitles/{id}` | 获取字幕文件流 | 登录 |
| 上报进度 | POST | `/api/playback/{id}/progress` | 上报播放进度 | 登录 |

**播放功能特性**：
- HTTP Range 断点续传（206 Partial Content）
- 多种认证方式：Bearer Token / Query Token / 一次性票据
- 硬件加速转码（auto/qsv/vaapi/nvenc/videotoolbox/none）
- HLS 转码输出
- 外部播放器协议直链（8种播放器）
- 转码并发控制（max_transcode_jobs）
- 转码缓存自动清理

---

### 2.4 下载模块

> 源文件：`backend/app/download/` (router.py, service.py, clients.py, models.py, schemas.py)

#### 2.4.1 下载客户端管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 客户端列表 | GET | `/api/download/clients` | 获取所有下载客户端 | 登录 |
| 创建客户端 | POST | `/api/download/clients` | 添加下载客户端 | 管理员 |
| 获取客户端 | GET | `/api/download/clients/{id}` | 获取客户端详情 | 登录 |
| 更新客户端 | PUT | `/api/download/clients/{id}` | 更新客户端配置 | 管理员 |
| 删除客户端 | DELETE | `/api/download/clients/{id}` | 删除客户端 | 管理员 |
| 测试连接 | POST | `/api/download/clients/{id}/test` | 测试客户端连接 | 管理员 |

#### 2.4.2 下载任务管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 任务列表 | GET | `/api/download/tasks` | 分页获取下载任务 | 登录 |
| 添加任务 | POST | `/api/download/add` | 添加下载任务 | 登录 |
| 暂停任务 | POST | `/api/download/{id}/pause` | 暂停下载 | 登录 |
| 恢复任务 | POST | `/api/download/{id}/resume` | 恢复下载 | 登录 |
| 删除任务 | DELETE | `/api/download/{id}` | 删除任务（可选删除文件） | 登录 |
| 同步状态 | POST | `/api/download/sync` | 手动同步下载状态 | 管理员 |
| 自动同步 | POST | `/api/download/start-auto-sync` | 启动后台自动进度同步（5秒间隔） | 登录 |

#### 2.4.3 Aria2 扩展

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| Aria2 统计 | GET | `/api/download/aria2/stats` | Aria2 全局统计（活跃/等待/停止/速度） | 登录 |

#### 2.4.4 整理入库

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 批量整理 | POST | `/api/download/organize` | 手动触发所有已完成任务整理入库 | 管理员 |
| 单个整理 | POST | `/api/download/{id}/organize` | 手动整理单个下载任务 | 管理员 |

**下载客户端适配器**：
- qBittorrent（WebUI API）
- Transmission（RPC API）
- Aria2（JSON-RPC）

---

### 2.5 订阅与站点模块

> 源文件：`backend/app/subscribe/` (router.py, service.py, site_adapter.py, notifier.py, models.py, schemas.py)

#### 2.5.1 站点管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 站点列表 | GET | `/api/sites` | 获取所有站点配置 | 登录 |
| 创建站点 | POST | `/api/sites` | 添加站点 | 管理员 |
| 更新站点 | PUT | `/api/sites/{id}` | 更新站点配置 | 管理员 |
| 删除站点 | DELETE | `/api/sites/{id}` | 删除站点 | 管理员 |
| 测试站点 | POST | `/api/sites/{id}/test` | 测试站点连接 | 管理员 |
| 浏览站点资源 | GET | `/api/sites/{id}/resource` | 分页浏览站点资源列表 | 登录 |
| 刷新用户数据 | GET | `/api/sites/{id}/userdata` | 获取站点用户数据（上传/下载量等） | 管理员 |

**支持站点类型**：
- **NexusPHP** — 国内绝大多数 PT 站
- **Gazelle/Luminance** — HDBits/OPS 等
- **UNIT3D** — BeyondHD/BluTopia 等
- **MTeam** — 馒头专用 REST API
- **Discuz** — 论坛型资源站
- **Custom RSS** — 自定义 RSS

**认证方式**：
- Cookie / API Key / Authorization Header

#### 2.5.2 资源搜索（跨站聚合）

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 跨站搜索 | GET | `/api/search/sites` | 多站点资源聚合搜索 | 登录 |

#### 2.5.3 订阅管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 订阅列表 | GET | `/api/subscriptions` | 获取订阅列表 | 登录 |
| 创建订阅 | POST | `/api/subscriptions` | 创建订阅 | 登录 |
| 更新订阅 | PUT | `/api/subscriptions/{id}` | 更新订阅 | 登录 |
| 删除订阅 | DELETE | `/api/subscriptions/{id}` | 删除订阅 | 管理员 |
| 按媒体查订阅 | GET | `/api/subscriptions/media/{mediaid}` | 支持 tmdb:/douban:/bangumi: 前缀 | 登录 |
| 触发搜索 | POST | `/api/subscriptions/{id}/search` | 手动触发订阅搜索 | 登录 |
| 分享订阅 | POST | `/api/subscriptions/{id}/share` | 创建订阅分享 | 登录 |
| 复制订阅 | POST | `/api/subscriptions/{id}/fork` | 从分享链接复制订阅 | 登录 |

**订阅过滤条件**：
- 画质优先级列表
- 最小/最大文件大小
- 包含/排除关键词

#### 2.5.4 通知渠道

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 渠道列表 | GET | `/api/notify/channels` | 获取通知渠道列表 | 登录 |
| 创建渠道 | POST | `/api/notify/channels` | 创建通知渠道 | 管理员 |
| 更新渠道 | PUT | `/api/notify/channels/{id}` | 更新通知渠道 | 管理员 |
| 删除渠道 | DELETE | `/api/notify/channels/{id}` | 删除通知渠道 | 管理员 |
| 测试渠道 | POST | `/api/notify/channels/{id}/test` | 发送测试通知 | 管理员 |

**通知渠道类型**：
- Telegram
- 微信（Server酱）
- Bark (iOS)
- Webhook
- Email

#### 2.5.5 RSS

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 拉取 RSS | POST | `/api/rss/pull` | 手动拉取所有站点 RSS | 管理员 |

---

### 2.6 系统模块

> 源文件：`backend/app/system/` (router.py, settings_router.py, settings_service.py, api_config_router.py, api_config_service.py, api_config_models.py, scheduler.py, events.py, crypto.py, models.py)

#### 2.6.1 系统信息

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 健康检查 | GET | `/api/health` | 健康检查端点 | 公开 |
| 系统信息 | GET | `/api/system/info` | 获取系统详细信息 | 登录 |
| 系统状态 | GET | `/api/system/status` | CPU/内存/磁盘使用率 | 登录 |
| 系统配置 | GET | `/api/system/config` | 获取可编辑系统配置（密钥掩码） | 管理员 |
| 更新系统配置 | PATCH | `/api/system/config` | 更新系统配置（写入 .env） | 管理员 |

#### 2.6.2 SSE 实时事件

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 获取 SSE 票据 | GET | `/api/system/events/ticket` | 生成一次性 SSE 票据（OTP，10秒有效） | 登录 |
| SSE 事件流 | GET | `/api/system/events` | SSE 实时事件推送 | 登录 |

**SSE 安全机制**：
- 一次性票据（OTP）认证（推荐，防 Nginx 日志泄露 JWT）
- 兼容 Authorization Header 认证
- 兼容 URL query token 认证（旧版）

**事件类型**：下载进度、扫描进度、通知消息等

#### 2.6.3 定时任务

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 调度器信息 | GET | `/api/system/scheduler` | 获取定时任务列表 | 管理员 |
| 触发任务 | POST | `/api/system/scheduler/{id}/trigger` | 手动触发定时任务 | 管理员 |

**内置定时任务**：
| 任务 ID | 名称 | 间隔 | 说明 |
|---------|------|------|------|
| `media_scan` | 媒体库扫描 | 60分钟 | 扫描+增量刮削 |
| `subscription_search` | 订阅搜索 | 60分钟 | 处理所有订阅 |
| `download_sync` | 下载状态同步 | 30秒 | 同步下载进度 |
| `rss_pull` | RSS 拉取 | 30分钟 | 拉取所有站点 RSS |
| `cache_cleanup` | 转码缓存清理 | 每天3:00 | 清理24小时以上的转码缓存 |
| `download_complete` | 下载完成整理 | 5分钟 | 检测完成并自动整理入库 |

#### 2.6.4 整理与刮削配置

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 获取所有配置 | GET | `/api/settings` | 获取所有整理/刮削配置 | 管理员 |
| 配置 Schema | GET | `/api/settings/schema` | 获取配置表单 Schema | 管理员 |
| 获取单个配置 | GET | `/api/settings/{key}` | 获取单个配置 | 管理员 |
| 更新单个配置 | PUT | `/api/settings/{key}` | 更新单个配置 | 管理员 |
| 批量更新 | PATCH | `/api/settings` | 批量更新配置 | 管理员 |
| 重置配置 | DELETE | `/api/settings/{key}` | 重置为默认值 | 管理员 |
| 重置所有 | DELETE | `/api/settings` | 重置所有配置 | 管理员 |

#### 2.6.5 API 配置管理

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 配置列表 | GET | `/api/api-config` | 获取所有 API 配置 | 管理员 |
| 获取配置 | GET | `/api/api-config/{provider}` | 获取指定 Provider 配置 | 管理员 |
| 获取生效配置 | GET | `/api/api-config/{provider}/effective` | 获取合并后的生效配置 | 管理员 |
| 更新配置 | POST | `/api/api-config/{provider}` | 更新 API 配置 | 管理员 |
| 清除配置 | DELETE | `/api/api-config/{provider}` | 清除 API Key | 管理员 |
| 测试连接 | POST | `/api/api-config/{provider}/test` | 测试 API 连接 | 管理员 |
| Provider 列表 | GET | `/api/api-config/providers/list` | 获取支持的 Provider 列表 | 管理员 |

**预置 Provider**：
- `tmdb` — TMDb API
- `douban` — 豆瓣
- `bangumi` — Bangumi
- `thetvdb` — TheTVDB
- `fanart` — Fanart.tv
- `openai` — OpenAI 兼容 API
- `siliconflow` — 硅基流动
- `deepseek` — DeepSeek
- `adult` — Adult Provider (JavBus/JavDB)

#### 2.6.6 敏感数据加密

> 源文件：`backend/app/system/crypto.py`

- 使用 Fernet (AES-128-CBC) 加密存储 API Key、Passkey 等敏感字段
- 基于 APP_SECRET_KEY 派生加密密钥
- 加密数据前缀标识 `enc:v1:`，兼容旧版明文迁移

---

### 2.7 管理后台模块

> 源文件：`backend/app/admin/` (router.py, service.py, schemas.py, backup_service.py)

#### 2.7.1 定时任务管理

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| 定时任务列表 | GET | `/api/admin/scheduler/tasks` | 获取可管理的定时任务 |
| 创建定时任务 | POST | `/api/admin/scheduler/tasks` | 创建自定义定时任务 |
| 更新定时任务 | PUT | `/api/admin/scheduler/tasks/{id}` | 更新定时任务 |
| 删除定时任务 | DELETE | `/api/admin/scheduler/tasks/{id}` | 删除定时任务 |

#### 2.7.2 批量操作

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| 批量扫描 | POST | `/api/admin/media/batch/scan` | 批量扫描媒体库 |
| 批量刮削 | POST | `/api/admin/media/batch/scrape` | 批量刮削媒体 |
| 批量删除 | POST | `/api/admin/media/batch/delete` | 批量删除媒体 |
| 批量移动 | POST | `/api/admin/media/batch/move` | 批量移动媒体到其他库 |
| 批量收藏 | POST | `/api/admin/media/batch/favorite` | 批量收藏 |
| 批量标记已看 | POST | `/api/admin/media/batch/watched` | 批量标记为已看 |
| 批量重命名 | POST | `/api/admin/media/batch/rename` | 批量重命名文件 |
| AI 重命名 | POST | `/api/admin/media/batch/ai-rename` | AI 智能重命名 |

#### 2.7.3 内容分级

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| 获取分级 | GET | `/api/admin/content-rating` | 获取内容分级配置 |
| 更新分级 | PUT | `/api/admin/content-rating` | 更新内容分级 |

#### 2.7.4 文件管理

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| 浏览文件 | GET | `/api/admin/files/browse` | 浏览文件目录 |
| 文件操作 | POST | `/api/admin/files/operation` | 文件操作（移动/复制/删除） |
| 重命名预览 | GET | `/api/admin/files/rename/preview` | 重命名预览 |
| 批量重命名预览 | POST | `/api/admin/files/rename/batch-preview` | 批量重命名预览 |
| 执行重命名 | POST | `/api/admin/files/rename/execute` | 执行重命名 |
| 创建文件夹 | POST | `/api/admin/files/folder` | 创建文件夹 |
| 重命名文件夹 | PUT | `/api/admin/files/folder/{path}` | 重命名文件夹 |
| 删除文件夹 | DELETE | `/api/admin/files/folder/{path}` | 删除文件夹 |

#### 2.7.5 系统管理

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| 系统设置 | GET/PUT | `/api/admin/settings` | 获取/更新系统设置 |
| 系统统计 | GET | `/api/admin/stats` | 获取系统统计信息 |
| 系统备份 | POST | `/api/admin/backup` | 触发系统备份 |
| 备份列表 | GET | `/api/admin/backup/list` | 获取备份列表 |
| 恢复备份 | POST | `/api/admin/backup/restore` | 从备份恢复 |

---

### 2.8 统计模块

> 源文件：`backend/app/stats/` (router.py, service.py, schemas.py)

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 概览统计 | GET | `/api/stats/overview` | 媒体总数/电影/剧集/大小/用户/播放次数 | 公开 |
| 播放趋势 | GET | `/api/stats/trend` | 按小时/天/周的播放趋势 | 公开 |
| 热门内容 | GET | `/api/stats/top-content` | 播放次数最多的媒体 | 公开 |
| 活跃用户 | GET | `/api/stats/top-users` | 播放次数最多的用户 | 公开 |
| 媒体库统计 | GET | `/api/stats/libraries` | 各媒体库统计 | 管理员 |
| 系统监控 | GET | `/api/stats/monitor` | CPU/内存/磁盘/网络监控 | 管理员 |
| 用户统计 | GET | `/api/stats/user/{id}` | 用户播放统计 | 管理员 |
| 记录播放 | POST | `/api/stats/play` | 记录播放事件 | 登录 |

---

### 2.9 播放列表模块

> 源文件：`backend/app/playlist/` (router.py, service.py, models.py, schemas.py)

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 播放列表列表 | GET | `/api/playlists` | 获取用户播放列表 | 登录 |
| 播放列表详情 | GET | `/api/playlists/{id}` | 获取列表详情（含媒体项） | 登录 |
| 创建播放列表 | POST | `/api/playlists` | 创建播放列表 | 登录 |
| 更新播放列表 | PUT | `/api/playlists/{id}` | 更新播放列表 | 登录 |
| 删除播放列表 | DELETE | `/api/playlists/{id}` | 删除播放列表 | 登录 |
| 添加项目 | POST | `/api/playlists/{id}/items` | 添加媒体到播放列表 | 登录 |
| 移除项目 | DELETE | `/api/playlists/{id}/items/{item_id}` | 从播放列表移除 | 登录 |
| 重新排序 | PUT | `/api/playlists/{id}/reorder` | 重新排序播放列表 | 登录 |

---

### 2.10 STRM 文件支持模块

> 源文件：`backend/app/strm/` (router.py, schemas.py, __init__.py)

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| STRM 配置 | GET | `/api/admin/strm/config` | 获取 STRM 配置 | 管理员 |
| 更新 STRM 配置 | PUT | `/api/admin/strm/config` | 更新 STRM 配置 | 管理员 |
| 获取 STRM URL | GET | `/api/admin/strm/media/{id}` | 获取媒体 STRM URL | 管理员 |
| 设置 STRM URL | PUT | `/api/admin/strm/media/{id}` | 设置媒体 STRM URL（协议白名单校验） | 管理员 |
| 清除 STRM URL | DELETE | `/api/admin/strm/media/{id}` | 清除 STRM URL | 管理员 |
| Emby STRM 播放信息 | GET | `/api/admin/strm/emby/Items/{id}/PlaybackInfo` | Emby 兼容 STRM 播放 | 公开 |

**STRM 功能**：将外部存储（WebDAV/Alist/S3/HTTP 直链）以"文件"形式加入媒体库，播放时直接访问远程 URL。

---

### 2.11 DLNA/投屏模块

> 源文件：`backend/app/dlna/__init__.py`（当前为 stub 实现）

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 发现设备 | GET | `/api/dlna/devices` | 发现 DLNA 设备 | 登录 |
| 获取设备 | GET | `/api/dlna/devices/{id}` | 获取设备信息 | 登录 |
| 投屏 | POST | `/api/dlna/cast` | 投屏媒体到设备 | 登录 |

> **注意**：当前 DLNA 为 stub 实现，返回空列表。Go 版可考虑完整实现。

---

### 2.12 授权管理模块

> 源文件：`backend/app/license/` (router.py, schemas.py, __init__.py)

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 授权信息 | GET | `/api/license/info` | 获取基本授权信息 | 登录 |
| 授权状态 | GET | `/api/license/status` | 获取详细授权状态 | 登录 |
| 激活授权 | POST | `/api/license/activate` | 通过授权码激活 Plus | 登录 |
| 解绑授权 | POST | `/api/license/unbind` | 解绑当前设备 | 登录 |
| 授权配置 | GET/POST | `/api/license/config` | 获取/更新授权配置 | 管理员 |
| 测试连接 | POST | `/api/license/config/test` | 测试授权服务器连接 | 管理员 |
| 心跳状态 | GET | `/api/license/heartbeat-status` | 获取心跳状态 | 登录 |
| 刷新授权 | POST | `/api/license/refresh` | 刷新授权状态 | 登录 |
| 生成授权码 | POST | `/api/license/generate` | 生成授权码 | 管理员 |
| 授权码列表 | GET | `/api/license/list` | 获取授权码列表 | 管理员 |
| 激活记录 | GET | `/api/license/{id}/activations` | 获取激活记录 | 管理员 |
| 吊销授权码 | POST | `/api/license/{id}/revoke` | 吊销授权码 | 管理员 |
| 解绑设备 | POST | `/api/license/activation/{id}/unbind` | 解绑指定设备 | 管理员 |

**Plus 版特性**：
- 无用户数量限制（免费版限30人）
- Plus 用户自动获得所有功能权限
- 授权码格式：`MS-XXXX-XXXX-XXXX-XXXX`
- 验证模式：本地验证 / 在线服务器验证

---

### 2.13 Emby API 兼容层

> 源文件：`backend/app/emby_api.py`（~1800 行，完整的 Emby Server API v3 兼容）

提供 Emby API 子集，让 **Infuse、Kodi、Fileball** 等客户端可以直接连接 MediaStationGo。

**核心 Emby 端点**（仅列出关键部分，实际约 50+ 端点）：

| 功能 | HTTP 方法 | 端点 | 说明 |
|------|----------|------|------|
| Emby 认证 | POST | `/api/emby/Users/AuthenticateByName` | Emby 客户端认证 |
| 系统信息 | GET | `/api/emby/System/Info` | Emby 系统信息 |
| 媒体库列表 | GET | `/api/emby/Library/VirtualFolders` | 虚拟文件夹（媒体库） |
| 媒体列表 | GET | `/api/emby/Items` | 媒体条目列表 |
| 媒体详情 | GET | `/api/emby/Users/{uid}/Items/{id}` | 媒体详情 |
| 搜索 | GET | `/api/emby/Items?searchTerm=` | 媒体搜索 |
| 播放信息 | GET | `/api/emby/Items/{id}/PlaybackInfo` | 获取播放流信息 |
| 视频流 | GET | `/api/emby/Videos/{id}/stream` | 视频流直链 |
| 字幕流 | GET | `/api/emby/Videos/{id}/Subtitles/{sid}/Stream` | 字幕流 |
| 播放进度上报 | POST | `/api/emby/Sessions/Playing` | 上报播放进度 |
| 播放停止 | POST | `/api/emby/Sessions/Playing/Stopped` | 播放停止上报 |
| 最新添加 | GET | `/api/emby/Users/{uid}/Items/Latest` | 最新添加的媒体 |
| 继续观看 | GET | `/api/emby/Users/{uid}/Items/Resume` | 继续观看列表 |

**Emby 认证方式**：
- X-Emby-Token Header
- Authorization: Bearer Token
- Emby 用户名/密码认证

---

### 2.14 发现/探索模块

> 源文件：`backend/app/media/discover_router.py`

| 功能 | HTTP 方法 | 端点 | 说明 | 权限 |
|------|----------|------|------|------|
| 可用区块列表 | GET | `/api/discover/sections` | 获取所有推荐区块（含可用状态） | 登录 |
| 聚合发现页 | GET | `/api/discover/feed` | 聚合各数据源推荐内容 | 登录 |
| 图片代理 | GET | `/api/discover/image-proxy` | 代理外部图片（绕过豆瓣防盗链） | 登录 |

**推荐区块（12 个）**：
| Key | 标签 | 数据源 |
|-----|------|--------|
| `recent_movies` | 最近添加电影 | 本地 |
| `recent_tv` | 最近添加剧集 | 本地 |
| `top_rated` | 评分最高 | 本地 |
| `anime` | 动漫推荐 | 本地 |
| `tmdb_trending` | 流行趋势 | TMDb |
| `tmdb_now_playing` | 正在热映 | TMDb |
| `tmdb_popular_movies` | TMDB 热门电影 | TMDb |
| `tmdb_popular_tv` | TMDB 热门电视剧 | TMDb |
| `douban_hot_movies` | 豆瓣热门电影 | 豆瓣 |
| `douban_hot_tv` | 豆瓣热门电视剧 | 豆瓣 |
| `douban_hot_anime` | 豆瓣热门动漫 | 豆瓣 |
| `douban_top250` | 豆瓣 TOP250 | 豆瓣 |
| `bangumi_daily` | Bangumi 每日放送 | Bangumi |

---

## 3. 数据模型清单

> 源文件：`backend/app/base_models.py`, 各模块 `models.py`

### 公共基类

| 模型 | 说明 |
|------|------|
| `Base` | SQLAlchemy 声明基类 |
| `TimestampMixin` | 时间戳混入（created_at, updated_at） |

### 用户模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `users` | User | id, username, password_hash, role, tier, avatar, nickname, is_active, last_login | 用户表 |
| `user_permissions` | UserPermission | id, user_id, can_* (19个权限字段) | 用户功能权限表 |
| `system_config` | SystemConfig | id, key, value, value_type | 系统配置表 |
| `watch_history` | WatchHistory | id, user_id, media_item_id, episode_id, progress, duration, completed, last_watched | 观看历史 |

### 媒体模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `media_libraries` | MediaLibrary | id, name, path, media_type, scan_interval, enabled, min_file_size, metadata_language, adult_content, prefer_nfo, enable_watch | 媒体库 |
| `media_items` | MediaItem | id, library_id, tmdb_id, douban_id, bangumi_id, title, original_title, year, overview, poster_url, backdrop_url, media_type, rating, genres, file_path, file_size, duration, video/audio_codec, resolution, strm_url, hdr_format, audio_channels, frame_rate, color_space, bit_depth, is_duplicate, duplicate_of, file_hash | 媒体条目 |
| `media_seasons` | MediaSeason | id, media_item_id, season_number, name, poster_url | 季 |
| `media_episodes` | MediaEpisode | id, season_id, episode_number, title, file_path, file_size, duration, air_date, video/audio_codec | 集 |
| `subtitles` | Subtitle | id, media_item_id, episode_id, language, language_name, path, source | 字幕 |
| `favorites` | Favorite | id, user_id, media_item_id (unique) | 收藏 |

### 下载模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `download_clients` | DownloadClient | id, name, client_type, host, port, username, password, enabled, category | 下载客户端 |
| `download_tasks` | DownloadTask | id, client_id, subscription_id, media_id, torrent_name, torrent_url, info_hash, save_path, status, progress, total_size, downloaded, speed, seeders, eta, message | 下载任务 |

### 订阅模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `sites` | Site | id, name, base_url, site_type, auth_type, cookie, api_key, auth_header, user_agent, rss_url, timeout, priority, use_proxy, rate_limit, browser_emulation, enabled, login_status, upload/download_bytes, downloader | 站点配置 |
| `subscriptions` | Subscription | id, name, original_name, tmdb_id, media_type, year, quality_filter, min/max_size, exclude/include_keywords, status, last_search, total_downloaded | 订阅 |
| `subscription_logs` | SubscriptionLog | id, subscription_id, action, resource_title, message | 订阅日志 |
| `notify_channels` | NotifyChannel | id, name, channel_type, config, enabled, events | 通知渠道 |

### 播放模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `play_history` | PlayHistory | id, user_id, media_item_id, played_at, duration, device_type, ip_address | 播放历史 |
| `playlists` | Playlist | id, user_id, name, description, cover_url, is_public | 播放列表 |
| `playlist_items` | PlaylistItem | id, playlist_id, media_item_id, position, added_at | 播放列表项 |

### 系统模块

| 表名 | 模型 | 关键字段 | 说明 |
|------|------|---------|------|
| `settings` | SettingsKV | id, key, value | KV 设置表 |
| `api_configs` | ApiConfig | id, provider, api_key, base_url, extra, enabled, description | API 配置表 |

---

## 4. 前端功能清单

> 源文件：`frontend/src/`

### 4.1 页面/视图

| 路由 | 视图文件 | 功能 | 权限 |
|------|---------|------|------|
| `/login` | LoginView.vue | 登录页 | 公开 |
| `/` | DashboardView.vue | 仪表盘（继续观看/最近添加/统计数据） | can_view_dashboard |
| `/media` | MediaLibraryView.vue | 媒体库浏览（列表/海报墙切换） | can_play_media |
| `/poster-wall` | PosterWallView.vue | 海报墙视图 | can_play_media |
| `/favorites` | FavoritesView.vue | 收藏列表 | can_favorite |
| `/tv/:id` | TvSeasonView.vue | 剧集季详情 | can_play_media |
| `/media/:id` | MediaDetailView.vue | 媒体详情页 | can_play_media |
| `/player/:id` | PlayerView.vue | 视频播放器 | can_play_media |
| `/downloads` | DownloadView.vue | 下载管理 | can_manage_downloads |
| `/discover` | DiscoverView.vue | 发现/探索页（多源聚合） | can_view_discover |
| `/search` | SearchResultView.vue | 搜索结果页 | 登录 |
| `/subscriptions` | SubscribeView.vue | 订阅管理 | can_manage_subscriptions |
| `/sites` | SitesView.vue | 站点管理 | can_manage_sites |
| `/site-search` | SiteSearchView.vue | 跨站资源搜索 | can_manage_sites |
| `/settings` | SettingsView.vue | 系统设置（多 Tab） | can_access_settings |
| `/history` | WatchHistoryView.vue | 观看历史 | can_view_history |
| `/profile` | ProfileView.vue | 个人资料 | 登录 |
| `/files` | FileManagerView.vue | 文件管理器 | can_manage_files |
| `/playlists` | PlaylistView.vue | 播放列表 | 登录 |
| `/playlists/:id` | PlaylistDetailView.vue | 播放列表详情 | 登录 |
| `/ai-assistant` | AIAssistantView.vue | AI 助手 | can_use_ai_assistant |
| `/profiles-management` | ProfileManagementView.vue | 用户管理 | 管理员 |
| `/storage` | StorageView.vue | 存储管理 | 管理员 |
| `/strm` | StrmView.vue | STRM 文件管理 | can_manage_strm |
| `/dlna` | DlnaView.vue | DLNA 投屏 | can_cast |

### 4.2 组件

| 组件 | 说明 |
|------|------|
| AppEmpty.vue | 空状态占位组件 |
| AppModal.vue | 通用模态框 |
| AppToast.vue | 消息提示 |
| BackendStatus.vue | 后端状态检测 |
| FileTree.vue / FileTreeNode.vue | 文件树组件 |
| settings/GeneralTab.vue | 通用设置 Tab |
| settings/AccountTab.vue | 账户设置 Tab |
| settings/UsersTab.vue | 用户管理 Tab |
| settings/LibrariesTab.vue | 媒体库设置 Tab |
| settings/OrganizeScrapeTab.vue | 整理与刮削设置 Tab |
| settings/DownloadTab.vue | 下载设置 Tab |
| settings/NotifyTab.vue | 通知设置 Tab |
| settings/SchedulerTab.vue | 定时任务设置 Tab |
| settings/SystemTab.vue | 系统设置 Tab |
| settings/ApiConfigTab.vue | API 配置 Tab |
| settings/LicenseTab.vue | 授权管理 Tab |
| settings/AdultTab.vue | Adult Provider 设置 Tab |
| settings/ConfigGroup.vue / ConfigRow.vue | 配置表单通用组件 |

### 4.3 状态管理（Pinia Stores）

| Store | 文件 | 说明 |
|-------|------|------|
| auth | stores/auth.ts | 认证状态 + 用户权限 |
| player | stores/player.ts | 播放器状态 |

### 4.4 API 调用模块

| 模块 | 文件 | 说明 |
|------|------|------|
| auth | api/auth.ts | 认证相关 API |
| media | api/media.ts | 媒体库 API |
| playback | api/playback.ts | 播放 API |
| download | api/download.ts | 下载 API |
| subscribe | api/subscribe.ts | 订阅 API |
| system | api/system.ts | 系统 API |
| settings | api/settings.ts | 设置 API |
| config | api/config.ts | 配置 API |
| admin | api/admin.ts | 管理后台 API |
| license | api/license.ts | 授权 API |
| profiles | api/profiles.ts | 用户配置 API |
| playlist | api/playlist.ts | 播放列表 API |
| strm | api/strm.ts | STRM API |
| dlna | api/dlna.ts | DLNA API |
| client | api/client.ts | HTTP 客户端封装 |

### 4.5 Composables

| 模块 | 说明 |
|------|------|
| useFormat.ts | 格式化工具（文件大小、时长等） |
| useImageError.ts | 图片加载错误处理（默认占位图） |
| useSSE.ts | SSE 实时事件连接 |
| useToast.ts | 消息提示封装 |

### 4.6 前端路由守卫

- 认证检查（requiresAuth）
- 游客页面重定向（guest）
- 管理员权限检查（adminOnly）
- 功能权限检查（requiredPermission）— 与后端 19 项权限对齐

---

## 5. 部署配置清单

### 5.1 Docker

| 文件 | 说明 |
|------|------|
| `docker/Dockerfile` | 多阶段构建（前端构建 + Python 运行时） |
| `docker/docker-compose.yml` | Docker Compose 编排 |
| `docker/docker-compose.template.yml` | 模板版本 |
| `docker/.env.template` | 环境变量模板 |
| `docker/deploy-docker.sh` | Linux 部署脚本 |
| `docker/deploy-docker.ps1` | Windows 部署脚本 |
| `docker/check-image-security.sh` | 镜像安全检查 |
| `docker-compose.example.yml` | 根目录示例 |

### 5.2 Helm Chart

| 文件 | 说明 |
|------|------|
| `deploy/charts/helm/Chart.yaml` | Chart 定义 |
| `deploy/charts/helm/values.yaml` | 默认值 |
| `deploy/charts/helm/templates/deployment.yaml` | Deployment 模板 |
| `deploy/charts/helm/templates/service.yaml` | Service 模板 |
| `deploy/charts/helm/templates/ingress.yaml` | Ingress 模板 |
| `deploy/charts/helm/templates/pvc.yaml` | PVC 模板 |
| `deploy/charts/helm/templates/secret.yaml` | Secret 模板 |
| `deploy/charts/helm/templates/serviceaccount.yaml` | ServiceAccount 模板 |
| `deploy/charts/helm/templates/_helpers.tpl` | Helm 辅助模板 |

### 5.3 其他部署

| 文件/目录 | 说明 |
|-----------|------|
| `nginx.example.conf` | Nginx 反向代理配置（含 SSE/视频流/HTTPS） |
| `deploy/scripts/install-linux.sh` | Linux 安装脚本 |
| `deploy/scripts/install-windows.ps1` | Windows 安装脚本 |
| `deploy/templates/unraid/mediastation.xml` | Unraid 模板 |
| `deploy/charts/truenas/media-station.yaml` | TrueNAS Chart |
| `deploy/packages/synology/build-spk.sh` | 群晖 SPK 打包 |
| `deploy/DEPLOYMENT.md` | 部署文档 |

---

## 6. 中间件与基础设施

| 功能 | 源文件 | 说明 |
|------|--------|------|
| CORS 中间件 | main.py | 可配置 origins，支持凭证 |
| 全局异常处理 | main.py | AppError 层级 + 422/500 兜底 |
| SPA 路由回退 | main.py | 非API请求返回 index.html |
| 路径遍历防护 | main.py, image_proxy.py | resolve() 后校验 |
| SQLite WAL 模式 | database.py | 预设 WAL + NORMAL 同步 |
| SQLite busy_timeout | database.py | 5000ms 忙等待 |
| PostgreSQL 连接池 | database.py | pool_size=10, max_overflow=20 |
| JWT 认证 | deps.py, user/auth.py | HS256, access + refresh token |
| 权限检查 | deps.py | require_permission() 工厂函数 |
| 敏感数据加密 | system/crypto.py | Fernet (AES-128-CBC) |
| SSE 事件总线 | system/events.py | 僵尸队列检测 + 心跳 + 自动清理 |
| 文件监控 | media/watcher.py | 文件系统实时监控 |
| 后台任务调度 | system/scheduler.py | APScheduler (AsyncIO) |

### 异常层级

| 异常类 | HTTP 状态码 | 说明 |
|--------|-----------|------|
| AppError | 500 | 基础业务异常 |
| NotFoundError | 404 | 资源不存在 |
| ValidationError | 422 | 参数校验失败 |
| UnauthorizedError | 401 | 未认证 |
| ForbiddenError | 403 | 无权限 |
| ConflictError | 409 | 资源冲突 |
| ExternalServiceError | 502 | 外部服务错误 |
| ScraperError | 404 | 刮削失败 |
| TranscodeError | 500 | 转码失败 |
| DownloadClientError | 502 | 下载客户端错误 |
| SiteError | 502 | 站点错误 |

---

## 7. 配置系统

> 源文件：`backend/app/config.py`

### 环境变量配置

| 分类 | 变量 | 默认值 | 说明 |
|------|------|--------|------|
| **应用** | APP_NAME | MediaStationGo | 应用名 |
| | APP_PORT | 3001 | 端口 |
| | APP_DEBUG | false | 调试模式 |
| | APP_SECRET_KEY | AUTO_GENERATE | JWT 密钥（自动生成警告） |
| | DATA_DIR | ./data | 数据目录 |
| | SERVER_URL | "" | 服务器地址（外部播放器用） |
| **数据库** | DATABASE_URL | "" | 留空用 SQLite |
| **TMDb** | TMDB_API_KEY | "" | TMDb API Key |
| | TMDB_LANGUAGE | zh-CN | TMDb 语言 |
| | TMDB_BASE_URL | https://api.themoviedb.org/3 | TMDb API 地址 |
| **豆瓣** | DOUBAN_COOKIE | "" | 豆瓣 Cookie |
| **Bangumi** | BANGUMI_TOKEN | "" | Bangumi Token |
| **qBittorrent** | QB_HOST | "" | qBittorrent 地址 |
| | QB_USERNAME | admin | 用户名 |
| | QB_PASSWORD | adminadmin | 密码 |
| **Transmission** | TR_HOST | "" | Transmission 地址 |
| | TR_USERNAME | "" | 用户名 |
| | TR_PASSWORD | "" | 密码 |
| **Telegram** | TELEGRAM_BOT_TOKEN | "" | Bot Token |
| | TELEGRAM_CHAT_ID | "" | Chat ID |
| **微信** | WECHAT_SENDKEY | "" | Server酱 SendKey |
| **Bark** | BARK_SERVER | "" | Bark 服务器 |
| | BARK_KEY | "" | Bark Key |
| **AI** | OPENAI_API_KEY | "" | OpenAI API Key |
| | OPENAI_BASE_URL | https://api.openai.com/v1 | API 地址 |
| | OPENAI_MODEL | gpt-4o-mini | 模型 |
| **FFmpeg** | FFMPEG_PATH | ffmpeg | FFmpeg 路径 |
| | FFPROBE_PATH | ffprobe | FFprobe 路径 |
| | HW_ACCEL | auto | 硬件加速 (auto/qsv/vaapi/nvenc/videotoolbox/none) |
| | MAX_TRANSCODE_JOBS | 2 | 最大并发转码 |
| | TRANSCODE_ENABLED | false | 默认关闭转码 |
| **媒体目录** | MOVIES_DIR | "" | 电影目录 |
| | TV_DIR | "" | 剧集目录 |
| | ANIME_DIR | "" | 动漫目录 |
| **JWT** | JWT_ACCESS_EXPIRE_MINUTES | 60 | Access Token 有效期 |
| | JWT_REFRESH_EXPIRE_DAYS | 30 | Refresh Token 有效期 |
| **安全** | VERIFY_CLIENT_SSL | true | 下载客户端 SSL 校验 |
| **CORS** | CORS_ORIGINS | "" | 逗号分隔的允许源 |

### 数据库存储配置（settings 表）

整理/刮削相关配置通过 `SettingsKV` 表存储，通过 `/api/settings` 端点管理。

### API 配置（api_configs 表）

各数据源 API Key 通过 `ApiConfig` 表存储，支持加密，通过 `/api/api-config` 端点管理。

---

## 8. 技术栈对照表

| 层次 | 原版 (Python) | 目标 (Go) |
|------|--------------|-----------|
| **Web 框架** | FastAPI | Gin |
| **ORM** | SQLAlchemy (async) | GORM |
| **数据库** | SQLite / PostgreSQL | SQLite / PostgreSQL |
| **认证** | python-jose (JWT) + passlib | golang-jwt + bcrypt |
| **任务调度** | APScheduler | robfig/cron 或类似 |
| **SSE** | sse-starlette | 原生实现 |
| **HTTP 客户端** | httpx | net/http |
| **模板引擎** | 无（SPA） | 无（SPA） |
| **前端** | Vue 3 + Pinia + Vue Router | React + Zustand + React Router |
| **UI 框架** | 未明确（推测自定义/Vuetify） | MUI + Tailwind CSS |
| **构建工具** | Vite | Vite |
| **加密** | cryptography (Fernet) | crypto/aes |
| **视频处理** | FFmpeg (subprocess) | FFmpeg (exec) |
| **容器化** | Docker + Helm | Docker + Helm |
| **反向代理** | Nginx | Nginx |

---

## 附录：API 端点总数统计

| 模块 | 端点数量 |
|------|---------|
| 用户与认证 | 19 |
| 媒体库 | 31 |
| 播放 | 11 |
| 下载 | 12 |
| 订阅与站点 | 20 |
| 系统 | 15 |
| 管理后台 | ~20 |
| 统计 | 8 |
| 播放列表 | 8 |
| STRM | 6 |
| DLNA | 3 |
| 授权管理 | 13 |
| Emby 兼容层 | ~50 |
| 发现/探索 | 3 |
| **总计** | **~220** |

---

> **文档版本**: v1.0 | **分析范围**: 旧版 Python 全量源代码
