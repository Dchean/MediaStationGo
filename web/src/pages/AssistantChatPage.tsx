import { FormEvent, useEffect, useRef, useState } from 'react'
import { Loader2, MessageSquare, Plus, Send, Trash2 } from 'lucide-react'
import toast from 'react-hot-toast'

import {
  assistantAPI,
  type AssistantMessage,
  type AssistantSession,
  type SessionView,
} from '../api/assistant'

// AssistantChatPage is the multi-turn chat surface backed by the Go
// AssistantService. It complements the older AIAssistantPage which is
// limited to single-turn smart search + recommendations.
export function AssistantChatPage() {
  const [sessions, setSessions] = useState<AssistantSession[]>([])
  const [active, setActive] = useState<SessionView | null>(null)
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [loading, setLoading] = useState(true)
  const messagesEndRef = useRef<HTMLDivElement | null>(null)

  const refreshSessions = async () => {
    try {
      const list = await assistantAPI.listSessions()
      setSessions(list)
      // Auto-select the most recent if nothing is open.
      if (list.length > 0 && !active) {
        const view = await assistantAPI.getSession(list[0].id)
        setActive(view)
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refreshSessions().catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [active?.messages.length])

  const onNew = async () => {
    try {
      const sess = await assistantAPI.createSession('New chat')
      const view = await assistantAPI.getSession(sess.id)
      setActive(view)
      await refreshSessions()
    } catch {
      toast.error('创建会话失败')
    }
  }

  const onSelect = async (id: string) => {
    try {
      setActive(await assistantAPI.getSession(id))
    } catch {
      toast.error('加载会话失败')
    }
  }

  const onDelete = async (id: string) => {
    if (!confirm('删除此会话?')) return
    try {
      await assistantAPI.deleteSession(id)
      if (active?.session.id === id) setActive(null)
      await refreshSessions()
    } catch {
      toast.error('删除失败')
    }
  }

  const onSend = async (e: FormEvent) => {
    e.preventDefault()
    if (!draft.trim() || !active) return
    setSending(true)
    const text = draft.trim()
    setDraft('')
    // Optimistic append so the user's turn shows immediately.
    setActive((s) =>
      s
        ? {
            ...s,
            messages: [
              ...s.messages,
              {
                id: 'pending-' + Date.now(),
                session_id: s.session.id,
                role: 'user',
                content: text,
                created_at: new Date().toISOString(),
              } as AssistantMessage,
            ],
          }
        : s,
    )
    try {
      const view = await assistantAPI.chat(active.session.id, text)
      setActive(view)
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '发送失败'
      toast.error(msg)
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="grid h-[calc(100vh-100px)] grid-cols-[260px_1fr] gap-4">
      {/* Sessions sidebar */}
      <aside className="glass-panel flex flex-col overflow-hidden">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-display text-sm font-semibold text-white">会话</h2>
          <button onClick={onNew} className="neon-button !px-2 !py-1 text-xs">
            <Plus size={12} /> 新建
          </button>
        </div>
        {loading && (
          <div className="flex justify-center py-6 text-slate-400">
            <Loader2 className="animate-spin" />
          </div>
        )}
        <ul className="flex-1 space-y-1 overflow-y-auto pr-1">
          {sessions.map((s) => (
            <li
              key={s.id}
              className={
                'group flex items-center gap-2 rounded px-2 py-2 text-sm cursor-pointer ' +
                (active?.session.id === s.id
                  ? 'bg-primary-400/10 text-primary-400'
                  : 'text-slate-300 hover:bg-white/5 hover:text-white')
              }
              onClick={() => onSelect(s.id)}
            >
              <MessageSquare size={14} className="shrink-0" />
              <span className="flex-1 truncate">{s.title || '未命名'}</span>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  onDelete(s.id)
                }}
                className="opacity-0 group-hover:opacity-100"
              >
                <Trash2 size={12} className="text-red-400" />
              </button>
            </li>
          ))}
          {!loading && sessions.length === 0 && (
            <li className="px-2 py-2 text-xs text-slate-500">暂无会话</li>
          )}
        </ul>
      </aside>

      {/* Conversation pane */}
      <section className="glass-panel flex flex-col overflow-hidden">
        {!active && (
          <div className="m-auto text-center text-slate-400">
            <MessageSquare size={32} className="mx-auto mb-2 opacity-50" />
            <p>选择或创建一个会话开始对话</p>
          </div>
        )}
        {active && (
          <>
            <div className="mb-3 border-b border-white/5 pb-2">
              <h2 className="font-display text-base font-semibold text-white">
                {active.session.title || '未命名'}
              </h2>
            </div>
            <div className="flex-1 space-y-3 overflow-y-auto pr-2">
              {active.messages.length === 0 && (
                <p className="text-sm text-slate-500">说点什么开始对话…</p>
              )}
              {active.messages.map((m) => (
                <Bubble key={m.id} message={m} />
              ))}
              <div ref={messagesEndRef} />
            </div>
            <form onSubmit={onSend} className="mt-3 flex gap-2 border-t border-white/5 pt-3">
              <input
                className="input-base flex-1"
                placeholder="发送消息…"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                disabled={sending}
              />
              <button type="submit" disabled={sending || !draft.trim()} className="neon-button">
                {sending ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
                发送
              </button>
            </form>
          </>
        )}
      </section>
    </div>
  )
}

function Bubble({ message }: { message: AssistantMessage }) {
  const mine = message.role === 'user'
  return (
    <div className={'flex ' + (mine ? 'justify-end' : 'justify-start')}>
      <div
        className={
          'max-w-[80%] whitespace-pre-wrap rounded-2xl px-4 py-2 text-sm ' +
          (mine
            ? 'bg-primary-400/20 text-primary-100'
            : message.role === 'system'
              ? 'border border-amber-400/30 bg-amber-400/5 text-amber-200'
              : 'bg-white/5 text-slate-200')
        }
      >
        {message.content}
      </div>
    </div>
  )
}
