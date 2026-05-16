import { api } from './client'

export interface AssistantSession {
  id: string
  user_id: string
  title: string
  created_at: string
  updated_at: string
}

export interface AssistantMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  operation_id?: string
  created_at: string
}

export interface SessionView {
  session: AssistantSession
  messages: AssistantMessage[]
}

export const assistantAPI = {
  listSessions: () =>
    api.get<AssistantSession[]>('/admin/assistant/sessions').then((r) => r.data),

  createSession: (title?: string) =>
    api
      .post<AssistantSession>('/admin/assistant/sessions', { title })
      .then((r) => r.data),

  getSession: (id: string) =>
    api.get<SessionView>(`/admin/assistant/session/${id}`).then((r) => r.data),

  deleteSession: (id: string) =>
    api.delete(`/admin/assistant/session/${id}`).then((r) => r.data),

  chat: (sessionID: string, message: string) =>
    api
      .post<SessionView>('/admin/assistant/chat', {
        session_id: sessionID,
        message,
      })
      .then((r) => r.data),

  execute: (sessionID: string, action: Record<string, unknown>) =>
    api
      .post<{ op_id: string }>('/admin/assistant/execute', {
        session_id: sessionID,
        action,
      })
      .then((r) => r.data),

  undo: (opID: string) =>
    api.post(`/admin/assistant/undo/${opID}`).then((r) => r.data),

  history: () =>
    api
      .get<{ items: { op_id: string; session: string; created_at: string; content: string }[] }>(
        '/admin/assistant/history',
      )
      .then((r) => r.data.items),
}
