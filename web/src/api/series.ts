import { api } from './client'
import type { Media } from '../types'

export interface SeasonGroup {
  season: number
  episodes: Media[]
}

export const seriesAPI = {
  seasons: (libraryID: string) =>
    api
      .get<{ seasons: SeasonGroup[] }>(`/libraries/${libraryID}/seasons`)
      .then((r) => r.data.seasons),
}
