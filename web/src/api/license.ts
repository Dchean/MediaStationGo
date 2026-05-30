import { api } from './client'

// ── License Server types (generic, ready for MediaStationLicenseServer integration) ──

/** Response after binding/activating a license. */
export interface LicenseActivation {
  id: string
  key_id: string
  /** The license key string (e.g. MS-XXXX-XXXX-XXXX) */
  key?: string
  device_id: string
  device_name?: string
  plan?: string
  max_activations?: number
  max_users?: number | null
  unlimited_users?: boolean
  /** ISO8601 — null means perpetual */
  expires_at?: string | null
  valid: boolean
  ip?: string
  heartbeat_at?: string | null
  created_at: string
}

/** Status of the currently bound license on this device. */
export interface LicenseStatus {
  /** Whether a license is currently active */
  active: boolean
  activation?: LicenseActivation
  max_users?: number | null
  unlimited_users?: boolean
  /** Error or status message */
  message?: string
}

/** Generate a short device fingerprint from browser/OS info. */
function deviceFingerprint(): string {
  const nav = window.navigator
  const parts = [nav.hardwareConcurrency, nav.language, screen.width, screen.height]
  return btoa(parts.join('|')).slice(0, 32).replace(/[+/=]/g, '')
}

// ── API methods ──

export const licenseAPI = {
  /** Bind / activate a license key for this device. */
  bind: (key: string) =>
    api
      .post<LicenseActivation>('/license/activate', {
        key: key.trim(),
        device_id: deviceFingerprint(),
        device_name: navigator.platform || 'Web Client',
      })
      .then((r) => r.data),

  /** Get the status of the currently active license. */
  status: () =>
    api.get<LicenseStatus>('/license/status').then((r) => r.data),

  /** Refresh the heartbeat for the active license. */
  heartbeat: () =>
    api.post('/license/heartbeat').then((r) => r.data),
}
