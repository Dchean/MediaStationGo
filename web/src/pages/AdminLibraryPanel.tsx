import { FormEvent, useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Plus, RefreshCw, Save, Trash2 } from 'lucide-react'

import { libraryAPI, type LibraryRootInput } from '../api/library'
import type { Library, LibraryRoot } from '../types'
import { confirmAction } from '../components/confirmAction'

type RootDraft = LibraryRootInput

const emptyRootDraft = (): RootDraft => ({ name: '', path: '', enabled: true })

export function AdminLibraryPanel() {
  const [libs, setLibs] = useState<Library[]>([])
  const [name, setName] = useState('')
  const [roots, setRoots] = useState<RootDraft[]>([emptyRootDraft()])
  const [type, setType] = useState('movie')
  const [newRootByLibrary, setNewRootByLibrary] = useState<Record<string, RootDraft>>({})
  const [rootDrafts, setRootDrafts] = useState<Record<string, RootDraft>>({})

  const refresh = () => libraryAPI.list({ includeHidden: true }).then(setLibs)
  useEffect(() => {
    refresh().catch(() => undefined)
  }, [])

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const payload = roots
        .map((root, index) => ({ ...root, path: root.path.trim(), name: root.name?.trim(), sort_order: index }))
        .filter((root) => root.path)
      if (payload.length === 0) {
        toast.error('请至少填写一个路径')
        return
      }
      await libraryAPI.createWithRoots(name, type, payload)
      toast.success('媒体库已创建')
      setName('')
      setRoots([emptyRootDraft()])
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '创建失败'
      toast.error(msg)
    }
  }

  const updateCreateRoot = (index: number, patch: Partial<RootDraft>) => {
    setRoots((prev) => prev.map((root, i) => (i === index ? { ...root, ...patch } : root)))
  }

  const addCreateRoot = () => setRoots((prev) => [...prev, emptyRootDraft()])

  const removeCreateRoot = (index: number) => {
    setRoots((prev) => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== index)))
  }

  const newRootDraft = (libraryID: string) => newRootByLibrary[libraryID] ?? emptyRootDraft()

  const setNewRootDraft = (libraryID: string, patch: Partial<RootDraft>) => {
    setNewRootByLibrary((prev) => ({ ...prev, [libraryID]: { ...newRootDraft(libraryID), ...patch } }))
  }

  const addLibraryRoot = async (libraryID: string) => {
    const draft = newRootDraft(libraryID)
    if (!draft.path?.trim()) {
      toast.error('请填写路径')
      return
    }
    await libraryAPI.addRoot(libraryID, { ...draft, path: draft.path.trim(), name: draft.name?.trim() })
    setNewRootByLibrary((prev) => ({ ...prev, [libraryID]: emptyRootDraft() }))
    toast.success('路径已添加')
    await refresh()
  }

  const rootDraftKey = (libraryID: string, rootID: string) => `${libraryID}:${rootID}`

  const editableRootDraft = (libraryID: string, root: LibraryRoot): RootDraft => {
    const key = rootDraftKey(libraryID, root.id)
    return rootDrafts[key] ?? {
      name: root.name ?? '',
      path: root.path,
      enabled: root.enabled,
      sort_order: root.sort_order,
    }
  }

  const setEditableRootDraft = (libraryID: string, root: LibraryRoot, patch: Partial<RootDraft>) => {
    const key = rootDraftKey(libraryID, root.id)
    setRootDrafts((prev) => ({ ...prev, [key]: { ...editableRootDraft(libraryID, root), ...patch } }))
  }

  const saveLibraryRoot = async (libraryID: string, root: LibraryRoot) => {
    const draft = editableRootDraft(libraryID, root)
    if (!draft.path?.trim()) {
      toast.error('请填写路径')
      return
    }
    await libraryAPI.updateRoot(libraryID, root.id, {
      name: draft.name?.trim(),
      path: draft.path.trim(),
      enabled: draft.enabled,
      sort_order: draft.sort_order,
    })
    setRootDrafts((prev) => {
      const next = { ...prev }
      delete next[rootDraftKey(libraryID, root.id)]
      return next
    })
    toast.success('路径已保存')
    await refresh()
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
        <select className="input-base" value={type} onChange={(e) => setType(e.target.value)}>
          <option value="movie">电影</option>
          <option value="tv">电视剧</option>
          <option value="variety">综艺</option>
          <option value="anime">动漫</option>
          <option value="music">音乐</option>
        </select>
        <div className="md:col-span-4 space-y-2">
          {roots.map((root, index) => (
            <div key={index} className="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]">
              <input
                className="input-base"
                placeholder="路径名称"
                value={root.name ?? ''}
                onChange={(e) => updateCreateRoot(index, { name: e.target.value })}
              />
              <input
                required={index === 0}
                className="input-base"
                placeholder="容器路径，如 /media/电视剧/国产剧"
                value={root.path}
                onChange={(e) => updateCreateRoot(index, { path: e.target.value })}
              />
              <button
                type="button"
                className="rounded-lg border border-red-400/40 px-3 text-red-400 hover:bg-red-400/10 disabled:opacity-40"
                disabled={roots.length <= 1}
                onClick={() => removeCreateRoot(index)}
                title="删除路径"
              >
                <Trash2 size={16} />
              </button>
            </div>
          ))}
          <button type="button" className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm" onClick={addCreateRoot}>
            <Plus size={16} /> 添加路径
          </button>
        </div>
        <p className="md:col-span-4 -mt-2 text-xs text-sand-500">
          Docker 部署时请优先填写容器内路径，例如 /media/电影、/media/电视剧/国产剧；如果误填 NAS
          宿主机路径，系统会尝试按 compose 挂载自动转换。
        </p>
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
                <td className="text-ink-100">
                  <div className="space-y-2">
                    {(l.roots?.length
                      ? l.roots
                      : [{
                          id: '',
                          library_id: l.id,
                          path: l.path,
                          enabled: l.enabled,
                          name: '',
                          sort_order: 0,
                          created_at: l.created_at,
                          updated_at: l.updated_at,
                        }]).map((root) => (
                      <div key={root.id || root.path} className="rounded border border-gray-200/70 p-2">
                        <div className="grid gap-2 xl:grid-cols-[minmax(120px,0.8fr)_minmax(220px,2fr)_auto]">
                          {root.id ? (
                            <>
                              <input
                                className="input-base"
                                placeholder="路径名称"
                                value={editableRootDraft(l.id, root).name ?? ''}
                                onChange={(e) => setEditableRootDraft(l.id, root, { name: e.target.value })}
                              />
                              <input
                                className="input-base"
                                placeholder="真实路径"
                                value={editableRootDraft(l.id, root).path}
                                onChange={(e) => setEditableRootDraft(l.id, root, { path: e.target.value })}
                              />
                            </>
                          ) : (
                            <span className="min-w-0 break-all xl:col-span-2">{root.name ? `${root.name}：${root.path}` : root.path}</span>
                          )}
                          <div className="flex flex-wrap items-center gap-2">
                            {root.id && (
                              <button
                                className="rounded border border-primary-400/40 p-1 text-brand-500 hover:bg-primary-400/10"
                                title="保存路径"
                                onClick={() => saveLibraryRoot(l.id, root)}
                              >
                                <Save size={14} />
                              </button>
                            )}
                          <button
                            className="rounded border border-primary-400/40 p-1 text-brand-500 hover:bg-primary-400/10"
                            title="扫描路径"
                            onClick={async () => {
                              if (!root.id) return
                              await libraryAPI.scanRoot(l.id, root.id)
                              toast.success('路径扫描已加入后台任务')
                            }}
                          >
                            <RefreshCw size={14} />
                          </button>
                          {root.id && (
                            <button
                              className="rounded border border-gray-300 px-2 py-1 text-xs"
                              onClick={async () => {
                                const enabled = !editableRootDraft(l.id, root).enabled
                                setEditableRootDraft(l.id, root, { enabled })
                                await libraryAPI.updateRoot(l.id, root.id, { enabled })
                                await refresh()
                              }}
                            >
                              {editableRootDraft(l.id, root).enabled ? '启用' : '禁用'}
                            </button>
                          )}
                          {root.id && (
                            <button
                              className="rounded border border-red-400/40 p-1 text-red-400 hover:bg-red-400/10"
                              title="删除路径"
                              onClick={async () => {
                                if (!(await confirmAction({ title: '删除媒体库路径', message: `确定删除「${root.path}」?`, confirmText: '删除' }))) return
                                await libraryAPI.removeRoot(l.id, root.id)
                                toast.success('路径已删除')
                                await refresh()
                              }}
                            >
                              <Trash2 size={14} />
                            </button>
                          )}
                          </div>
                        </div>
                      </div>
                    ))}
                    <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]">
                      <input
                        className="input-base"
                        placeholder="路径名称"
                        value={newRootDraft(l.id).name ?? ''}
                        onChange={(e) => setNewRootDraft(l.id, { name: e.target.value })}
                      />
                      <input
                        className="input-base"
                        placeholder="新增路径"
                        value={newRootDraft(l.id).path}
                        onChange={(e) => setNewRootDraft(l.id, { path: e.target.value })}
                      />
                      <button className="rounded-lg border px-3 py-2 text-sm" onClick={() => addLibraryRoot(l.id)}>
                        <Plus size={14} />
                      </button>
                    </div>
                  </div>
                </td>
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
