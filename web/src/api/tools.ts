import { api } from './client'

// toolsAPI groups admin-only endpoints that don't fit the other domain
// modules: organizing media files into the canonical naming layout, and
// dispatching a test notification through the configured channels.
export const toolsAPI = {
  organizeMedia: (mediaID: string) =>
    api
      .post<{ path: string }>(`/admin/media/${mediaID}/organize`)
      .then((r) => r.data),

  organizeLibrary: (libraryID: string) =>
    api
      .post<Record<string, unknown>>(`/admin/libraries/${libraryID}/organize`)
      .then((r) => r.data),

  notifyTest: (title: string, body: string) =>
    api
      .post<{ message: string }>('/admin/notify/test', { title, body })
      .then((r) => r.data),
}
