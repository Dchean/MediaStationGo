import type { Media } from '../types'

/**
 * 把若干 Episode/Movie 行折叠成"剧集卡片"。
 *
 * 后端的 /api/media 等接口默认返回 episode 级行——同一部剧的每一集都
 * 是一行。在「最近添加」「海报墙」「收藏墙」这种以剧集为单位展示的页面
 * 我们要把它们合成一张代表卡片，避免同一海报刷屏。
 *
 * 折叠键优先级（命中第一个就分组）：
 *
 * 剧集（有季集号 / 路径像剧集）:
 *   1. 库标识 + 路径剧名       ← 最稳定:同一部剧的各集都在同一剧目录下
 *   2. tmdb_id / bangumi_id / douban / thetvdb （无路径剧名时兜底）
 *   3. series_id              （后端预聚合,目前仅 Emby 虚拟分组用,DB 一般为空）
 *   4. 库标识 + title         （最终兜底）
 * 电影:
 *   1. tmdb_id / bangumi_id
 *   2. 库标识 + 路径剧名
 *   3. 库标识 + title
 *
 * 为什么剧集要把「路径剧名」放在 tmdb_id 之前:
 * 本地/网盘按 MoviePilot 整理的剧集每集旁带 episode NFO, 其 <uniqueid type="tmdb">
 * 是【单集 episode id】(每集都不同)。若按 tmdb_id 分组, 同一部剧 N 集会被拆成 N
 * 张卡(实测「遮天」90 集 = 89 个不同 tmdb_id)。而整剧目录名对全剧一致, 是最可靠的
 * 分组依据, 且对「部分集已刮削、部分未刮削」也能稳定合并。
 *
 * key 必须是「单条 media 的纯函数」且与子集无关。最终暴露给 URL/API 的
 * key 是短 hash，避免把库标识和标题这类内部分类依据塞进地址栏。
 *
 * 同一组内取最早 created_at 的那条作为代表卡片，并带 count 表示集数。
 */
export type SeriesCard = { key: string; rep: Media; linkMedia: Media; count: number }

export function getSeriesKey(media: Media): string {
  return compactSeriesKey(getSeriesRawKey(media))
}

function getSeriesRawKey(media: Media): string {
  const fromPath = seriesTitleFromPath(media.path)
  if (isEpisodeLike(media) || pathLooksEpisodic(media)) {
    // 路径剧名优先:对全剧一致, 不受单集 tmdb 污染影响。
    if (fromPath) return seriesFingerprint('library-path', targetLibraryID(media), fromPath)
    // 无路径剧名(扁平目录)时才退而用外部 id;此时各集若共享整剧 id 仍能合并。
    if (media.tmdb_id && media.tmdb_id > 0) return `tmdb:${media.tmdb_id}`
    if (media.bangumi_id && media.bangumi_id > 0) return `bgm:${media.bangumi_id}`
    if (media.douban_id) return `douban:${media.douban_id}`
    if (media.thetvdb_id) return `thetvdb:${media.thetvdb_id}`
    if (media.series_id) return `series:${media.series_id}`
    return seriesFingerprint('library-title', targetLibraryID(media), normalizeTitle(seriesTitle(media)))
  }
  if (media.series_id) return `series:${media.series_id}`
  if (media.tmdb_id && media.tmdb_id > 0) return `tmdb:${media.tmdb_id}`
  if (media.bangumi_id && media.bangumi_id > 0) return `bgm:${media.bangumi_id}`
  if (fromPath) return seriesFingerprint('library-path', media.library_id, fromPath)
  return seriesFingerprint('library-title', media.library_id, normalizeTitle(media.title))
}

function seriesFingerprint(...parts: string[]): string {
  return parts.join('\x1f')
}

function compactSeriesKey(raw: string): string {
  const normalized = raw.trim()
  if (!normalized) return ''
  let hash = 0x811c9dc5
  for (const byte of new TextEncoder().encode(normalized)) {
    hash ^= byte
    hash = Math.imul(hash, 0x01000193) >>> 0
  }
  return `series:${hash.toString(16).padStart(8, '0')}`
}

export function isEpisodeLike(media: Media): boolean {
  return (media.season_num ?? 0) > 0 || (media.episode_num ?? 0) > 0
}

// 剧集类目录名(电视剧/动漫及其二级分类)。媒体路径落在这些目录下时, 即便
// 季集号未识别出来, 也应按剧集对待, 跳转到 /library 分类视图而非 /media 单页。
const EPISODIC_PATH_RE =
  /[\\/](?:电视剧|剧集|国产剧|欧美剧|日韩剧|日剧|韩剧|综艺|纪录片|动漫|番剧|国漫|日番|儿童|tv|series|shows?|season[\s._-]*\d|s\d{1,2}(?:[\s._-]|[\\/])|specials?|sp|ova|oad|extra|extras|特别篇|特別篇|番外|特典)[\\/]/i

const SEASON_FOLDER_RE =
  /^(?:s\d{1,2}|season[\s._-]*\d{1,2}|第\s*\d{1,2}\s*季|specials?|sp|ova|oad|extra|extras|特别篇|特別篇|番外|特典)$/i

function pathLooksEpisodic(media: Media): boolean {
  const path = (media.path || media.display_library_path || media.library_path || '')
  return EPISODIC_PATH_RE.test(path)
}

export function isSeriesCard(card: SeriesCard): boolean {
  return (
    card.count > 1 ||
    isEpisodeLike(card.rep) ||
    isEpisodeLike(card.linkMedia) ||
    pathLooksEpisodic(card.rep) ||
    pathLooksEpisodic(card.linkMedia)
  )
}

export function seriesTitle(media: Media): string {
  const fromPath = seriesTitleFromPath(media.path)
  return fromPath || media.title || media.original_name || '未命名节目'
}

function normalizeTitle(value?: string): string {
  return (value ?? '')
    .toLowerCase()
    .replace(/\s*\((?:19|20)\d{2}\)\s*/g, ' ')
    .replace(/\s*\[(?:tmdb|tmdbid)[=-]\d+\]\s*/g, ' ')
    .replace(/\s*\{(?:tmdb|tmdbid|douban|bangumi|bgm|thetvdb|tvdb)[\s:=#-]*[a-z0-9_-]+\}\s*/g, ' ')
    .replace(/[\s._-]+/g, ' ')
    .trim()
}

const SERIES_SPECIAL_SUFFIX_RE =
  /(?:\s+(?:specials?|sp|ova|oad|extra|extras)|\s*(?:特别篇|特別篇|番外|特典|外传|外傳))$/i

function normalizePathSeriesTitle(value?: string): string {
  const title = normalizeTitle(value)
  const stripped = title.replace(SERIES_SPECIAL_SUFFIX_RE, '').trim()
  return stripped || title
}

export function seriesTitleFromPath(path?: string): string {
  if (!path) return ''
  const parts = path.split(/[\\/]+/).filter(Boolean)
  if (parts.length < 2) return ''
  let dirIndex = parts.length - 2
  while (dirIndex >= 0 && SEASON_FOLDER_RE.test(parts[dirIndex])) {
    dirIndex -= 1
  }
  if (dirIndex < 0) return ''
  return normalizePathSeriesTitle(parts[dirIndex])
}

export function groupSeries(items: Media[] = []): SeriesCard[] {
  const safeItems = Array.isArray(items) ? items : []
  const groups = new Map<string, SeriesCard>()
  for (const m of safeItems) {
    if (!m) continue
    const key = getSeriesKey(m)

    const g = groups.get(key)
    if (!g) {
      groups.set(key, { key, rep: m, linkMedia: m, count: 1 })
    } else {
      g.count += 1
      if (betterSeriesLinkMedia(m, g.linkMedia)) {
        g.linkMedia = m
      }
      const currentArtwork = artworkScore(m)
      const representativeArtwork = artworkScore(g.rep)
      if (currentArtwork > representativeArtwork) {
        g.rep = m
      } else if (currentArtwork === representativeArtwork) {
        const cur = (m.season_num ?? 0) * 10000 + (m.episode_num ?? 0)
        const rep = (g.rep.season_num ?? 0) * 10000 + (g.rep.episode_num ?? 0)
        if (cur > 0 && (rep === 0 || cur < rep)) g.rep = m
      }
    }
  }
  return Array.from(groups.values())
}

export function seriesCardLink(card: SeriesCard): string {
  if (isSeriesCard(card)) {
    return `/library/${targetLibraryID(card.linkMedia)}?series=${encodeURIComponent(card.key)}`
  }
  return `/media/${card.rep.id}`
}

function betterSeriesLinkMedia(candidate: Media, current: Media): boolean {
  const candidateScore = librarySpecificityScore(candidate)
  const currentScore = librarySpecificityScore(current)
  if (candidateScore !== currentScore) return candidateScore > currentScore
  return artworkScore(candidate) > artworkScore(current)
}

function librarySpecificityScore(media: Media): number {
  const rawPath = (media.display_library_path || media.library_path || '').trim()
  if (!rawPath) return 0
  const normalized = rawPath.replace(/\\/g, '/').replace(/\/+$/, '')
  const lower = normalized.toLowerCase()
  if (lower.startsWith('cloud://')) {
    const rest = normalized.slice('cloud://'.length)
    const slash = rest.indexOf('/')
    if (slash < 0 || slash === rest.length - 1) return 0
    return 100 + rest.slice(slash + 1).split('/').filter(Boolean).length
  }
  return 200 + normalized.split('/').filter(Boolean).length
}

function targetLibraryID(media: Media): string {
  return media.display_library_id || media.library_id
}

export function artworkScore(media: Media): number {
  const poster = (media.poster_url ?? '').toLowerCase()
  const backdrop = (media.backdrop_url ?? '').toLowerCase()
  if (poster) {
    if (/(poster|folder|cover|movie|show|pl)(?:[._-]|\.[a-z0-9]+$|$)/.test(poster)) return 40
    if (/(actor|actress|cast|avatar|sample|screenshot|screen|still|scene|fanart|backdrop|background|landscape|banner|logo|disc)/.test(poster)) return 10
    if (/(thumb)/.test(poster)) return 20
    return 30
  }
  return backdrop ? 5 : 0
}
