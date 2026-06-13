import { api } from './client'

export interface JobStatus {
  name: string
  interval: string
  last_run?: string
  last_err?: string
  running?: boolean
  started_at?: string
}

export const schedulerAPI = {
  status: () => api.get<{ jobs: JobStatus[] }>('/admin/scheduler').then((r) => r.data.jobs),
  run: (name: string) => api.post(`/admin/scheduler/${name}/run`).then((r) => r.data),
}
