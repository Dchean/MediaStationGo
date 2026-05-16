import { api } from './client'
import type { NotifyChannel } from '../types'

// Payload accepted by create / update. `events` and `enabled` are optional.
export interface NotifyChannelInput {
  name: string
  channel_type: NotifyChannel['channel_type']
  config: Record<string, string>
  events?: string[]
  enabled?: boolean
}

// notifyChannelsAPI wraps the admin /admin/notify/channels surface.
export const notifyChannelsAPI = {
  list: () =>
    api.get<NotifyChannel[]>('/admin/notify/channels').then((r) => r.data),

  create: (input: NotifyChannelInput) =>
    api.post<NotifyChannel>('/admin/notify/channels', input).then((r) => r.data),

  update: (id: string, input: NotifyChannelInput) =>
    api.put<NotifyChannel>(`/admin/notify/channels/${id}`, input).then((r) => r.data),

  remove: (id: string) =>
    api.delete(`/admin/notify/channels/${id}`).then((r) => r.data),

  test: (id: string) =>
    api.post<{ message: string }>(`/admin/notify/channels/${id}/test`).then((r) => r.data),
}
