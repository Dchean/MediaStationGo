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
 *   1. series_id     （后端某些场景下会预先聚合）
 *   2. 有季/集信息时用 library_id + 节目名（综艺每集常有不同 TMDB episode id）
 *   3. tmdb_id / bangumi_id（电影或无季集条目）
 *   4. library_id + title （fallback：同库同名视为同一剧）
 *
 * 同一组内取最早 created_at 的那条作为代表卡片，并带 count 表示集数。
 */
export type SeriesCard = { key: string; rep: Media; linkMedia: Media; count: number }

export function getSeriesKey(media: Media): string {
  if (media.series_id) return `series:${media.series_id}`
  const fromPath = seriesTitleFromPath(media.path)
  if (isEpisodeLike(media)) {
    if (media.tmdb_id && media.tmdb_id > 0) return `tmdb:${media.tmdb_id}`
    if (media.bangumi_id && media.bangumi_id > 0) return `bgm:${media.bangumi_id}`
    if (media.douban_id) return `douban:${media.douban_id}`
    if (media.thetvdb_id) return `thetvdb:${media.thetvdb_id}`
    return `lib:${targetLibraryID(media)}|show:${normalizeTitle(fromPath || seriesTitle(media))}`
  }
  if (media.tmdb_id && media.tmdb_id > 0) return `tmdb:${media.tmdb_id}`
  if (media.bangumi_id && media.bangumi_id > 0) return `bgm:${media.bangumi_id}`
  if (fromPath) return `lib:${media.library_id}|show:${normalizeTitle(fromPath)}`
  return `lib:${media.library_id}|${normalizeTitle(media.title)}`
}

export function isEpisodeLike(media: Media): boolean {
  return (media.season_num ?? 0) > 0 || (media.episode_num ?? 0) > 0
}

export function isSeriesCard(card: SeriesCard): boolean {
  return card.count > 1 || isEpisodeLike(card.rep) || isEpisodeLike(card.linkMedia)
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

function seriesTitleFromPath(path?: string): string {
  if (!path) return ''
  const parts = path.split(/[\\/]+/).filter(Boolean)
  if (parts.length < 2) return ''
  let dirIndex = parts.length - 2
  if (/^(?:s\d{1,2}|season\s*\d{1,2}|第\s*\d{1,2}\s*季)$/i.test(parts[dirIndex])) {
    dirIndex -= 1
  }
  if (dirIndex < 0) return ''
  return normalizeTitle(parts[dirIndex])
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
