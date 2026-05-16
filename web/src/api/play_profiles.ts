import { api } from './client'
import type { PlayProfile } from '../types'

// Payload accepted by create / update.
export interface PlayProfileInput {
  user_id?: string
  name: string
  is_default: boolean
  content_rating_limit?: string
  allow_adult: boolean
  require_pin: boolean
  pin?: string
  preferred_subtitle_lang?: string
  preferred_audio_lang?: string
  autoplay_next: boolean
  skip_intro: boolean
  allowed_library_ids: string[]
}

// playProfilesAPI wraps /play-profiles. The admin variant adds ?all=true.
export const playProfilesAPI = {
  list: (all = false) =>
    api
      .get<PlayProfile[]>('/play-profiles', { params: all ? { all: 'true' } : {} })
      .then((r) => r.data),

  create: (input: PlayProfileInput) =>
    api.post<PlayProfile>('/play-profiles', input).then((r) => r.data),

  update: (id: string, input: PlayProfileInput) =>
    api.put<PlayProfile>(`/play-profiles/${id}`, input).then((r) => r.data),

  remove: (id: string) =>
    api.delete(`/play-profiles/${id}`).then((r) => r.data),
}
