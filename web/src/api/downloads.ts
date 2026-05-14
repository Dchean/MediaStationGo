import { api } from './client'
import type { DownloadTask, QBitTorrent } from '../types'

export interface DownloadsState {
  tasks: DownloadTask[]
  torrents: QBitTorrent[] | null
}

export const downloadsAPI = {
  list: () => api.get<DownloadsState>('/downloads').then((r) => r.data),

  add: (url: string, savePath = '') =>
    api
      .post<DownloadTask>('/downloads', { url, save_path: savePath })
      .then((r) => r.data),

  remove: (hash: string, deleteFiles = false) =>
    api
      .delete(`/downloads/${hash}?delete_files=${deleteFiles ? 'true' : 'false'}`)
      .then((r) => r.data),

  reload: () => api.post('/downloads/reload').then((r) => r.data),
}
