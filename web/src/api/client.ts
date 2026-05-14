import axios, { AxiosError } from 'axios'

import { useAuthStore } from '../stores/auth'

// Single axios instance used by every API helper. Adds the JWT to outgoing
// requests and routes 401s back to the login page.
export const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (resp) => resp,
  (err: AxiosError) => {
    if (err.response?.status === 401) {
      useAuthStore.getState().logout()
      if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  },
)

const tokenQuery = () => {
  const t = useAuthStore.getState().token ?? ''
  return `token=${encodeURIComponent(t)}`
}

// streamURL returns a direct-play URL for <video src>. The JWT is added as
// a query parameter because <video> elements cannot send Authorization
// headers.
export function streamURL(mediaId: string): string {
  return `/api/stream/${encodeURIComponent(mediaId)}?${tokenQuery()}`
}

// hlsURL returns the m3u8 playlist URL fed into hls.js.
export function hlsURL(mediaId: string): string {
  return `/api/hls/${encodeURIComponent(mediaId)}/index.m3u8?${tokenQuery()}`
}

// imageURL converts a remote poster URL into a same-origin proxy URL so it
// can never be blocked by CORS / GFW. Empty strings pass through unchanged.
export function imageURL(remote?: string): string {
  if (!remote) return ''
  if (remote.startsWith('/api/img')) return remote
  return `/api/img?url=${encodeURIComponent(remote)}&${tokenQuery()}`
}
