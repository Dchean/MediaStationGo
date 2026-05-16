import { api } from './client'

export interface UserPermission {
  user_id: string
  can_play_media: boolean
  can_favorite: boolean
  can_view_history: boolean
  can_view_dashboard: boolean
  can_view_discover: boolean
  can_manage_downloads: boolean
  can_manage_subscriptions: boolean
  can_manage_sites: boolean
  can_manage_files: boolean
  can_manage_strm: boolean
  can_cast: boolean
  can_use_ai_assistant: boolean
  can_access_settings: boolean
  updated_at: string
}

export const permissionsAPI = {
  // Caller's effective permissions; admins always get the all-true set.
  mine: () => api.get<UserPermission>('/auth/permissions').then((r) => r.data),

  // Admin endpoints
  get: (userID: string) =>
    api.get<UserPermission>(`/admin/users/${userID}/permissions`).then((r) => r.data),

  save: (userID: string, p: UserPermission) =>
    api
      .put<UserPermission>(`/admin/users/${userID}/permissions`, p)
      .then((r) => r.data),

  reset: (userID: string) =>
    api
      .post<UserPermission>(`/admin/users/${userID}/permissions/reset`)
      .then((r) => r.data),
}
