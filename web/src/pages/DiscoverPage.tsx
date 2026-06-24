import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, RefreshCw, Sparkles } from 'lucide-react'

import { discoverAPI, type DiscoverItem, type DiscoverSection } from '../api/discover'
import { ContentRow, DiscoverSkeleton } from './DiscoverContentRow'
import { DiscoverDetailModal } from './DiscoverDetailModal'
import {
  defaultSections,
  discoverStorageKey,
  readSavedSections,
} from './discoverPageModel'

export function DiscoverPage() {
  const [sections, setSections] = useState<DiscoverSection[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [rows, setRows] = useState<Record<string, DiscoverItem[]>>({})
  const [rowLoading, setRowLoading] = useState<Record<string, boolean>>({})
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})
  const [sectionsReady, setSectionsReady] = useState(false)
  const [loading, setLoading] = useState(false)
  const [activeItem, setActiveItem] = useState<DiscoverItem | null>(null)
  const [reloadSeq, setReloadSeq] = useState(0)
  const [imageVersion, setImageVersion] = useState(() => String(Date.now()))
  const [refreshImageVersion, setRefreshImageVersion] = useState<string>()

  useEffect(() => {
    let cancelled = false
    setSectionsReady(false)
    discoverAPI
      .sections()
      .then((items) => {
        if (cancelled) return
        setSections(items)
        const saved = readSavedSections(items)
        const available = new Set(items.map((item) => item.key))
        const fallback = defaultSections.filter((key) => available.has(key))
        setSelected(saved.length > 0 ? saved : fallback)
        setSectionsReady(true)
      })
      .catch(() => {
        if (cancelled) return
        setSections([])
        setSelected([])
        setSectionsReady(true)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!sectionsReady) return
    const available = new Set(sections.map((section) => section.key))
    const activeSelected = selected.filter((key) => available.has(key))
    if (activeSelected.length !== selected.length) {
      setSelected(activeSelected)
      return
    }
    if (selected.length === 0) {
      setRows({})
      setRowLoading({})
      setRowErrors({})
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    setRowErrors({})
    setRowLoading(Object.fromEntries(selected.map((key) => [key, true])))
    setRows((current) => {
      const next: Record<string, DiscoverItem[]> = {}
      for (const key of selected) {
        next[key] = current[key] ?? []
      }
      return next
    })
    window.localStorage.setItem(discoverStorageKey, JSON.stringify(selected))

    let pending = selected.length
    const markDone = () => {
      pending -= 1
      if (!cancelled && pending <= 0) setLoading(false)
    }
    for (const key of selected) {
      discoverAPI
        .feed([key])
        .then((feed) => {
          if (cancelled) return
          setRows((current) => ({ ...current, [key]: feed[key] ?? [] }))
        })
        .catch((err) => {
          if (cancelled) return
          const message = err instanceof Error ? err.message : String(err)
          setRows((current) => ({ ...current, [key]: [] }))
          setRowErrors((current) => ({ ...current, [key]: message }))
        })
        .finally(() => {
          if (!cancelled) {
            setRowLoading((current) => ({ ...current, [key]: false }))
          }
          markDone()
        })
    }
    return () => {
      cancelled = true
    }
  }, [sections, sectionsReady, selected, reloadSeq])

  const sectionMap = useMemo(
    () => new Map(sections.map((section) => [section.key, section])),
    [sections],
  )
  const hasContent = selected.some((key) => (rows[key] ?? []).length > 0)
  const hasRowErrors = Object.keys(rowErrors).length > 0

  const toggleSection = (key: string) => {
    setSelected((current) => {
      if (current.includes(key)) {
        return current.filter((item) => item !== key)
      }
      return [...current, key]
    })
  }

  const refreshDiscover = () => {
    const nextImageVersion = String(Date.now())
    setImageVersion(nextImageVersion)
    setRefreshImageVersion(nextImageVersion)
    setReloadSeq((current) => current + 1)
  }

  return (
    <div className="mx-auto max-w-7xl space-y-8 px-4 py-6">
      <header className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex items-center gap-4">
          <div className="rounded-2xl border border-primary-500/20 bg-gradient-to-br from-primary-500/20 to-primary-600/10 p-3">
            <Sparkles className="h-8 w-8 text-brand-500" />
          </div>
          <div>
            <h1 className="font-display text-4xl font-bold tracking-tight text-ink-600">
              发现
            </h1>
            <p className="mt-1 text-base text-ink-50">
              多源推荐：TMDb / 豆瓣 / Bangumi，可按需组合显示
            </p>
          </div>
        </div>

        <div className="flex flex-col gap-3 lg:items-end">
          <button
            type="button"
            onClick={refreshDiscover}
            disabled={!sectionsReady || selected.length === 0}
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs font-semibold text-ink-600 transition hover:border-primary-300 hover:text-brand-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
            刷新
          </button>
          <div className="flex flex-wrap justify-start gap-2 lg:justify-end">
            {sections.map((section) => {
              const active = selected.includes(section.key)
              return (
                <button
                  key={section.key}
                  type="button"
                  onClick={() => toggleSection(section.key)}
                  className={
                    'rounded-full border px-3 py-1.5 text-xs font-semibold transition ' +
                    (active
                      ? 'border-primary-400 bg-primary-400/15 text-brand-500'
                      : 'border-gray-200 bg-white text-gray-500 hover:border-primary-300 hover:text-ink-600')
                  }
                >
                  {section.label}
                </button>
              )
            })}
          </div>
        </div>
      </header>

      {!sectionsReady && <DiscoverSkeleton />}

      {sectionsReady && !loading && selected.length === 0 && (
        <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center text-sand-500">
          至少选择一个推荐源，小宇宙才会开始转动。
        </div>
      )}

      {sectionsReady && selected.length > 0 && (
        <div className="space-y-10">
          {selected.map((key) => {
            const items = rows[key] ?? []
            if (items.length === 0) {
              if (rowLoading[key]) {
                return <DiscoverRowSkeleton key={key} title={sectionMap.get(key)?.label ?? key} />
              }
              return null
            }
            return (
              <ContentRow
                key={key}
                title={sectionMap.get(key)?.label ?? key}
                items={items}
                imageVersion={imageVersion}
                refreshImageVersion={refreshImageVersion}
                onSelect={setActiveItem}
              />
            )
          })}

          {hasRowErrors && (
            <div className="flex items-start gap-3 rounded-2xl border border-amber-500/20 bg-amber-500/10 p-4">
              <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-amber-400" />
              <div className="space-y-1 text-sm text-amber-200">
                {Object.entries(rowErrors).map(([key, message]) => (
                  <p key={key}>{sectionMap.get(key)?.label ?? key}：{message}</p>
                ))}
              </div>
            </div>
          )}

          {!loading && !hasContent && !hasRowErrors && (
            <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center">
              <p className="text-sand-500">
                当前选择的推荐源暂未返回内容，可切换豆瓣 / Bangumi 或检查网络代理。
              </p>
            </div>
          )}
        </div>
      )}

      {activeItem && (
        <DiscoverDetailModal
          item={activeItem}
          onClose={() => setActiveItem(null)}
        />
      )}
    </div>
  )
}

function DiscoverRowSkeleton({ title }: { title: string }) {
  return (
    <section className="space-y-4">
      <h2 className="pl-1 font-display text-2xl font-semibold text-ink-600">{title}</h2>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
        {[1, 2, 3, 4, 5, 6, 7, 8].map((item) => (
          <div key={item} className="aspect-[2/3] animate-pulse rounded-xl bg-gray-100" />
        ))}
      </div>
    </section>
  )
}
