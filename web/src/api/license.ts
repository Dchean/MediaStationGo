import { api } from './client'

// LicenseKey + LicenseActivation mirror the Go model structs.
export interface LicenseKey {
  id: string
  key: string
  customer?: string
  plan: string
  max_activations: number
  issued_at: string
  expires_at?: string | null
  revoked: boolean
  notes?: string
  created_at: string
  updated_at: string
}

export interface LicenseActivation {
  id: string
  key_id: string
  device_id: string
  device_name?: string
  ip?: string
  unbound_at?: string | null
  heartbeat_at?: string | null
  created_at: string
}

export interface GenerateKeyInput {
  customer?: string
  plan?: string
  max_activations?: number
  expires_at?: string // RFC3339; "" or omit for perpetual
  notes?: string
}

export const licenseAPI = {
  generate: (input: GenerateKeyInput) =>
    api.post<LicenseKey>('/admin/license/generate', input).then((r) => r.data),

  list: () => api.get<LicenseKey[]>('/admin/license/list').then((r) => r.data),

  listActivations: (keyID: string) =>
    api
      .get<LicenseActivation[]>(`/admin/license/${keyID}/activations`)
      .then((r) => r.data),

  revoke: (keyID: string) =>
    api.post(`/admin/license/${keyID}/revoke`).then((r) => r.data),

  unbind: (activationID: string) =>
    api.post(`/admin/license/activation/${activationID}/unbind`).then((r) => r.data),

  // Self-service
  activate: (key: string, deviceID: string, deviceName?: string) =>
    api
      .post<LicenseActivation>('/license/activate', {
        key,
        device_id: deviceID,
        device_name: deviceName,
      })
      .then((r) => r.data),

  status: (keyID: string) =>
    api
      .get<{ key: LicenseKey; active_activations: number; valid: boolean }>(
        '/license/status',
        { params: { key_id: keyID } },
      )
      .then((r) => r.data),
}
