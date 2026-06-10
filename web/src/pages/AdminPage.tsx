import { FormEvent, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { KeyRound, Loader2, Pencil, Plus, ShieldCheck, Trash2, UserCheck, UserX, X } from 'lucide-react'

import { adminAPI } from '../api/admin'
import { libraryAPI } from '../api/library'
import { licenseAPI, type LicenseStatus } from '../api/license'
import type { Library, User } from '../types'
import { APIConfigsPanel } from '../components/APIConfigsPanel'
import { ManagementShortcuts } from '../components/ManagementShortcuts'
import { confirmAction } from '../components/ConfirmDialog'
import { requestPassword } from '../components/PasswordDialog'

type AdminTab = 'library' | 'users' | 'api'

function parseAdminTab(value: string | null): AdminTab {
  if (value === 'users' || value === 'api') return value
  return 'library'
}

export function AdminPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [tab, setTab] = useState<AdminTab>(() => parseAdminTab(searchParams.get('tab')))
  const tabs = [
    { key: 'library' as const, label: '媒体库' },
    { key: 'users' as const, label: '用户' },
    { key: 'api' as const, label: '外部API' },
  ]

  useEffect(() => {
    setTab(parseAdminTab(searchParams.get('tab')))
  }, [searchParams])

  const selectTab = (next: AdminTab) => {
    setTab(next)
    setSearchParams(next === 'library' ? {} : { tab: next })
  }

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-ink-600">管理后台</h1>
      <ManagementShortcuts
        title="统一管理入口"
        description="侧栏保持精简，完整管理能力统一从这里进入。"
        items={[
          { to: '/sites', title: '站点管理', description: '维护 PT 站点、认证方式和检索配置' },
          { to: '/download-clients', title: '下载器管理', description: '配置 qBittorrent 等下载器连接', badge: '下载' },
          { to: '/files', title: '手动整理', description: '从下载目录选择文件夹并整理入库' },
          { to: '/storage', title: '存储与文件', description: '查看占用、清理重复项和管理文件' },
        ]}
      />
      <div className="flex flex-wrap gap-2 border-b border-gray-200">
        {tabs.map((k) => (
          <button
            key={k.key}
            onClick={() => selectTab(k.key)}
            className={
              'border-b-2 px-4 py-2 text-sm transition ' +
              (tab === k.key
                ? 'border-primary-400 text-brand-500'
                : 'border-transparent text-ink-50 hover:text-white')
            }
          >
            {k.label}
          </button>
        ))}
      </div>

      {tab === 'library' && <LibraryPanel />}
      {tab === 'users' && <UsersPanel />}
      {tab === 'api' && <APIConfigsPanel />}
    </div>
  )
}

function LibraryPanel() {
  const [libs, setLibs] = useState<Library[]>([])
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [type, setType] = useState('movie')

  const refresh = () => libraryAPI.list({ includeHidden: true }).then(setLibs)
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await libraryAPI.create(name, path, type)
      toast.success('媒体库已创建')
      setName('')
      setPath('')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '创建失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <form onSubmit={handleCreate} className="glass-panel grid gap-3 md:grid-cols-4">
        <input
          required
          className="input-base"
          placeholder="名称"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          required
          className="input-base md:col-span-2"
          placeholder="容器路径，如 /media/电视剧/国产剧"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        <p className="md:col-span-4 -mt-2 text-xs text-sand-500">
          Docker 部署时请优先填写容器内路径，例如 /media/电影、/media/电视剧/国产剧；如果误填 NAS
          宿主机路径，系统会尝试按 compose 挂载自动转换。
        </p>
        <select className="input-base" value={type} onChange={(e) => setType(e.target.value)}>
          <option value="movie">电影</option>
          <option value="tv">电视剧</option>
          <option value="variety">综艺</option>
          <option value="anime">动漫</option>
          <option value="music">音乐</option>
        </select>
        <button type="submit" className="neon-button md:col-span-4">
          新建媒体库
        </button>
      </form>

      <div className="glass-panel">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase tracking-wider text-sand-500">
            <tr>
              <th className="py-2">名称</th>
              <th>路径</th>
              <th>类型</th>
              <th className="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {libs.map((l) => (
              <tr key={l.id} className="border-t border-gray-200">
                <td className="py-2 text-ink-600">{l.name}</td>
                <td className="text-ink-100">{l.path}</td>
                <td className="text-ink-100">{l.type}</td>
                <td className="space-x-2 py-2 text-right">
                  <button
                    className="rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10"
                    onClick={async () => {
                      const r = await libraryAPI.scan(l.id)
                      if (r.queued) toast.success('云盘扫描已加入后台队列，会自动入库')
                      else toast.success(`扫描完成，新增 ${r.added}，更新 ${r.updated ?? 0}`)
                    }}
                  >
                    扫描
                  </button>
                  <button
                    className="rounded-lg border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10"
                    onClick={async () => {
                      if (!(await confirmAction({ title: '删除媒体库', message: `确定删除「${l.name}」?`, confirmText: '删除' }))) return
                      await libraryAPI.remove(l.id)
                      toast.success('已删除')
                      await refresh()
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function UsersPanel() {
  const [users, setUsers] = useState<User[]>([])
  const [licenseStatus, setLicenseStatus] = useState<LicenseStatus | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [editingID, setEditingID] = useState<string | null>(null)
  const [editingUsername, setEditingUsername] = useState('')
  const [resettingPasswordID, setResettingPasswordID] = useState<string | null>(null)
  const refresh = async () => {
    const [nextUsers, nextLicense] = await Promise.all([
      adminAPI.listUsers(),
      licenseAPI.status().catch(() => null),
    ])
    setUsers(nextUsers)
    setLicenseStatus(nextLicense)
  }
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const unlimitedUsers =
    licenseStatus?.active === true &&
    (licenseStatus.unlimited_users === true || licenseStatus.max_users == null)
  const maxUsers = unlimitedUsers ? null : (licenseStatus?.max_users ?? 20)
  const userLimitReached = maxUsers != null && users.length >= maxUsers
  const userLimitLabel = unlimitedUsers ? '不限制' : String(maxUsers)

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await adminAPI.createUser({ username, password })
      toast.success('用户已添加，默认仅允许浏览与播放媒体')
      setUsername('')
      setPassword('')
      await refresh()
    } catch (err: unknown) {
      const msg =
        userCreateErrorMessage(err) ??
        '添加用户失败'
      toast.error(msg)
    }
  }

  const startEdit = (u: User) => {
    setEditingID(u.id)
    setEditingUsername(u.username)
  }

  const saveEdit = async (id: string) => {
    try {
      await adminAPI.updateUser(id, { username: editingUsername })
      toast.success('用户名已更新')
      setEditingID(null)
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '更新失败'
      toast.error(msg)
    }
  }

  const resetPassword = async (u: User) => {
    if (resettingPasswordID) return
    const nextPassword = await requestPassword({
      title: `重置 ${u.username} 的密码`,
      message: '请输入新的临时密码，至少 6 位。保存后该用户可立即使用新密码登录 Web、Bot 与第三方客户端。',
      confirmText: '重置密码',
    })
    if (!nextPassword) return
    if (nextPassword.length < 6) {
      toast.error('新密码至少 6 位')
      return
    }
    setResettingPasswordID(u.id)
    try {
      await adminAPI.resetUserPassword(u.id, nextPassword)
      toast.success('密码已重置')
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '重置密码失败'
      toast.error(msg)
    } finally {
      setResettingPasswordID(null)
    }
  }

  const toggleStatus = async (u: User) => {
    const next = !u.is_active
    if (!next && u.is_protected) {
      toast.error('受保护管理员不可禁用')
      return
    }
    if (
      !next &&
      !(await confirmAction({
        title: '禁用用户',
        message: `禁用「${u.username}」后，Web 与第三方客户端已有登录也会失效。`,
        confirmText: '禁用',
      }))
    ) {
      return
    }
    try {
      await adminAPI.setUserStatus(u.id, next)
      toast.success(next ? '用户已解禁' : '用户已禁用')
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '操作失败'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <form onSubmit={handleCreate} className="glass-panel grid gap-3 md:grid-cols-[1fr_1fr_auto]">
        <div className="md:col-span-3 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="font-display text-lg font-semibold text-ink-600">用户管理</h2>
            <p className="text-xs text-sand-500">
              已创建 {users.length}/{userLimitLabel} 个用户；新增用户默认只有媒体库浏览、播放、外部播放器与第三方客户端观看权限。
            </p>
          </div>
          <span className="rounded-full border border-primary-400/30 px-3 py-1 text-xs text-brand-500">
            默认管理员不可删除 · 最高权限
          </span>
        </div>
        <input
          required
          className="input-base"
          placeholder="用户名"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          disabled={userLimitReached}
        />
        <input
          required
          minLength={6}
          className="input-base"
          placeholder="初始密码（至少 6 位）"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={userLimitReached}
        />
        <button type="submit" className="neon-button inline-flex items-center justify-center gap-2" disabled={userLimitReached}>
          <Plus size={16} />
          添加用户
        </button>
      </form>

      <div className="glass-panel overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase tracking-wider text-sand-500">
            <tr>
              <th className="py-2">用户名</th>
              <th>角色</th>
              <th>状态</th>
              <th>权限说明</th>
              <th>最近登录</th>
              <th className="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className="border-t border-gray-200">
                <td className="py-2 text-ink-600">
                  {editingID === u.id ? (
                    <input
                      className="input-base h-9 max-w-48"
                      value={editingUsername}
                      onChange={(e) => setEditingUsername(e.target.value)}
                    />
                  ) : (
                    <span className="inline-flex items-center gap-2">
                      {u.username}
                      {u.is_default_admin && <ShieldCheck size={15} className="text-brand-500" />}
                    </span>
                  )}
                </td>
                <td className="text-ink-100">{u.role === 'admin' ? '管理员' : '观看用户'}</td>
                <td className={u.is_active ? 'text-green-500' : 'text-red-400'}>
                  {u.is_active ? '正常' : '已禁用'}
                </td>
                <td className="text-ink-50">
                  {u.role === 'admin' ? '全部管理权限' : '仅浏览/播放/外部播放器，无下载与文件操作'}
                </td>
                <td className="text-ink-50">
                  {u.last_login_at ? new Date(u.last_login_at).toLocaleString() : '从未登录'}
                </td>
                <td className="space-x-2 py-2 text-right">
                  {editingID === u.id ? (
                    <>
                      <button
                        className="rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10"
                        onClick={() => saveEdit(u.id)}
                      >
                        保存
                      </button>
                      <button
                        className="rounded-lg border border-gray-300 px-2 py-1 text-xs text-ink-100 hover:bg-gray-100"
                        onClick={() => setEditingID(null)}
                      >
                        <X size={12} />
                      </button>
                    </>
                  ) : (
                    <button
                      className="rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10"
                      onClick={() => startEdit(u)}
                    >
                      <Pencil size={12} />
                    </button>
                  )}
                  <button
                    className="rounded-lg border border-amber-400/40 px-2 py-1 text-xs text-amber-500 hover:bg-amber-400/10"
                    title="重置密码"
                    disabled={resettingPasswordID === u.id}
                    onClick={() => resetPassword(u)}
                  >
                    {resettingPasswordID === u.id ? <Loader2 size={12} className="animate-spin" /> : <KeyRound size={12} />}
                  </button>
                  <button
                    className={
                      'rounded-lg border px-2 py-1 text-xs disabled:cursor-not-allowed disabled:opacity-40 ' +
                      (u.is_active
                        ? 'border-orange-400/40 text-orange-500 hover:bg-orange-400/10'
                        : 'border-green-400/40 text-green-500 hover:bg-green-400/10')
                    }
                    disabled={u.is_protected && u.is_active}
                    title={u.is_active ? '禁用用户' : '解禁用户'}
                    onClick={() => toggleStatus(u)}
                  >
                    {u.is_active ? <UserX size={12} /> : <UserCheck size={12} />}
                  </button>
                  <button
                    className="rounded-lg border border-red-400/40 px-2 py-1 text-xs text-red-400 hover:bg-red-400/10 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={u.is_protected}
                    title={u.is_protected ? '默认管理员禁止删除' : '删除用户'}
                    onClick={async () => {
                      if (u.is_protected) return
                      if (!(await confirmAction({ title: '删除用户', message: `确定删除「${u.username}」?`, confirmText: '删除' }))) return
                      await adminAPI.deleteUser(u.id)
                      toast.success('已删除')
                      await refresh()
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function userCreateErrorMessage(err: unknown): string | undefined {
  const data = (err as { response?: { data?: { error?: string; max_users?: number } } })?.response?.data
  if (!data?.error) return undefined
  if (data.error === 'user limit reached' && data.max_users != null) {
    return `用户数量已达到授权上限：${data.max_users} 人`
  }
  return data.error
}
