import { api } from './client'
import type { NotifyChannel } from '../types'

// Payload accepted by create / update. `events` and `enabled` are optional.
export interface NotifyChannelInput {
  name: string
  type: NotifyChannel['type']
  config: Record<string, string>
  events?: string[]
  enabled?: boolean
}

// notifyChannelsAPI wraps the admin /admin/notify/channels surface.
export const notifyChannelsAPI = {
  list: () =>
    api.get<NotifyChannel[]>('/admin/notify/channels').then((r) => r.data ?? []),

  create: (input: NotifyChannelInput) =>
    api.post<NotifyChannel>('/admin/notify/channels', input).then((r) => r.data),

  update: (id: string, input: NotifyChannelInput) =>
    api.put<NotifyChannel>(`/admin/notify/channels/${id}`, input).then((r) => r.data),

  remove: (id: string) =>
    api.delete(`/admin/notify/channels/${id}`).then((r) => r.data),

  test: (id: string) =>
    api.post<{ message: string }>(`/admin/notify/channels/${id}/test`).then((r) => r.data),

  startTelegramPolling: () =>
    api.post<{
      message: string
      started: number
      already_running: number
      skipped: number
      errors?: string[]
    }>('/admin/telegram/polling/start').then((r) => r.data),

  stopTelegramPolling: () =>
    api.post<{ message: string; stopped: number }>('/admin/telegram/polling/stop').then((r) => r.data),
}
