import type { Library, Media } from '../types'
import { artworkScore, groupSeries, type SeriesCard } from '../utils/groupSeries'

export type LibraryPreview = {
  library: Library
  items: Media[]
  total: number
  cards: SeriesCard[]
}

export function latestLibraryCards(items: Media[]): SeriesCard[] {
  return groupSeries(items)
    .sort((a, b) => mediaTime(b.rep) - mediaTime(a.rep) || artworkScore(b.rep) - artworkScore(a.rep))
    .slice(0, 10)
}

export function mediaTime(media: Media): number {
  return Date.parse(media.updated_at || media.created_at || '') || 0
}
