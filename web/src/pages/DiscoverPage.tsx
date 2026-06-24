import { useEffect, useMemo, useState } from 'react'

import { discoverAPI, type DiscoverItem, type DiscoverSection } from '../api/discover'
import { DiscoverSkeleton } from './DiscoverContentRow'
import { DiscoverDetailModal } from './DiscoverDetailModal'
import { DiscoverEmptySelection, DiscoverHeader, DiscoverResults } from './DiscoverPageSections'
import {
  defaultSections,
  discoverStorageKey,
  readSavedSections,
  serializeSavedSections,
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
    window.localStorage.setItem(discoverStorageKey, serializeSavedSections(selected))

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
  const sectionLabel = (key: string) => sectionMap.get(key)?.label ?? key

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
      <DiscoverHeader
        sections={sections}
        selected={selected}
        sectionsReady={sectionsReady}
        loading={loading}
        onRefresh={refreshDiscover}
        onToggleSection={toggleSection}
      />

      {!sectionsReady && <DiscoverSkeleton />}

      {sectionsReady && !loading && selected.length === 0 && (
        <DiscoverEmptySelection />
      )}

      {sectionsReady && selected.length > 0 && (
        <DiscoverResults
          selected={selected}
          rows={rows}
          rowLoading={rowLoading}
          rowErrors={rowErrors}
          loading={loading}
          hasContent={hasContent}
          imageVersion={imageVersion}
          refreshImageVersion={refreshImageVersion}
          sectionLabel={sectionLabel}
          onSelect={setActiveItem}
        />
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
