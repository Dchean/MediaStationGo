import { api } from './client'
import type { DiscoverItem, DiscoverSection } from '../types'

// discoverExtraAPI wraps the Vue-style multi-section feed used by the
// React DiscoverPage rails. Use the existing /discover/trending and
// /discover/popular helpers for the simple cases.
export const discoverExtraAPI = {
  sections: () =>
    api.get<{ sections: DiscoverSection[] }>('/discover/sections').then((r) => r.data.sections),

  feed: (sectionKeys: string[]) =>
    api
      .get<Record<string, DiscoverItem[] | null>>('/discover/feed', {
        params: { sections: sectionKeys.join(',') },
      })
      .then((r) => r.data),
}
