import { api } from './client'
import type { StatsSnapshot } from '../types'

export const statsAPI = {
  snapshot: () => api.get<StatsSnapshot>('/stats').then((r) => r.data),
}
