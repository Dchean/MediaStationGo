import { api } from './client'

export type DownloadClientType = 'qbittorrent' | 'aria2' | 'transmission'

export interface DownloadClient {
  id: string
  name: string
  type: DownloadClientType
  url: string
  username?: string
  save_path?: string
  is_default: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface DownloadClientInput {
  name: string
  type: DownloadClientType
  url: string
  username?: string
  password?: string
  save_path?: string
  is_default: boolean
  enabled: boolean
}

export const downloadClientsAPI = {
  list: () =>
    api.get<DownloadClient[]>('/admin/download/clients').then((r) => r.data),

  create: (input: DownloadClientInput) =>
    api.post<DownloadClient>('/admin/download/clients', input).then((r) => r.data),

  update: (id: string, input: DownloadClientInput) =>
    api
      .put<DownloadClient>(`/admin/download/clients/${id}`, input)
      .then((r) => r.data),

  remove: (id: string) =>
    api.delete(`/admin/download/clients/${id}`).then((r) => r.data),

  test: (id: string) =>
    api
      .post<{ ok: boolean; error?: string }>(`/admin/download/clients/${id}/test`)
      .then((r) => r.data),

  aria2Stats: (clientID: string) =>
    api
      .get('/admin/download/aria2/stats', { params: { client_id: clientID } })
      .then((r) => r.data),
}
