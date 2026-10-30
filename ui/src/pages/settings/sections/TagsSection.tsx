import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { TagInfo, TagUsage, TagsSettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// TagsSection is the tags section: tag CRUD plus the usage view (how many
// series/episodes carry each tag, fetched per tag from the tags usage
// endpoint). Prowlarr PR4 adds the indexer side of the usage view: which
// indexers' tag sets include the label (their traffic routes through any
// proxy carrying the tag).
export function TagsSection() {
  const { doc, error, authLost, refresh } = useSectionDoc<TagsSettings>('tags')

  const [tags, setTags] = useState<TagInfo[]>([])
  const [label, setLabel] = useState('')
  const [description, setDescription] = useState('')
  const [usage, setUsage] = useState<Record<number, TagUsage>>({})
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (doc) setTags(doc.tags)
  }, [doc])

  // Fetch the usage counts for every tag (the tags section's usage view).
  useEffect(() => {
    let cancelled = false
    setUsage({})
    tags.forEach((t) => {
      client
        .getTagUsage(t.id)
        .then((u) => {
          if (!cancelled) setUsage((prev) => ({ ...prev, [t.id]: u }))
        })
        .catch(() => {
          /* usage is a view; a failure just leaves it blank */
        })
    })
    return () => {
      cancelled = true
    }
  }, [tags])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="tags-error" />
  }

  function handleAdd() {
    const trimmed = label.trim()
    if (!trimmed) return
    void submit(async () => {
      const id = Date.now()
      const next: TagsSettings = { tags: [...tags, { id, label: trimmed, description }] }
      await client.putSettingsSection<TagsSettings>('tags', next)
      setLabel('')
      setDescription('')
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  function handleDelete(id: number) {
    void submit(async () => {
      const next: TagsSettings = { tags: tags.filter((t) => t.id !== id) }
      await client.putSettingsSection<TagsSettings>('tags', next)
    })
  }

  return (
    <SectionCard title="Tags" hint="Organize series by label; usage shows what each tag covers." testId="tags-card">
      <table className="table" data-testid="tags-table">
        <thead>
          <tr>
            <th>Label</th>
            <th>Description</th>
            <th>Usage</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {tags.map((t) => (
            <tr key={t.id}>
              <td>{t.label}</td>
              <td>{t.description}</td>
              <td data-testid={`tag-usage-${t.id}`}>
                {usage[t.id] ? (
                  <>
                    {usage[t.id].series} series, {usage[t.id].episodes} episodes
                    {/* Prowlarr PR4: the indexers carrying the tag. Guard the
                        sub-document — a stale usage response predating the
                        field arrives without it. */}
                    {usage[t.id].indexers && usage[t.id].indexers.count > 0 && (
                      <span data-testid={`tag-usage-indexers-${t.id}`}>
                        {' · '}
                        {usage[t.id].indexers.count} indexer
                        {usage[t.id].indexers.count === 1 ? '' : 's'} (
                        {usage[t.id].indexers.names.join(', ')})
                      </span>
                    )}
                  </>
                ) : (
                  '—'
                )}
              </td>
              <td>
                <button onClick={() => handleDelete(t.id)} data-testid={`tag-del-${t.id}`}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <label htmlFor="tag-label">New tag label</label>
      <input id="tag-label" value={label} onChange={(e) => setLabel(e.target.value)} data-testid="tag-new-label" />
      <label htmlFor="tag-desc">Description</label>
      <input id="tag-desc" value={description} onChange={(e) => setDescription(e.target.value)} data-testid="tag-new-desc" />
      <button onClick={handleAdd} disabled={busy || !label.trim()} data-testid="tags-add">
        Add tag
      </button>
      {saved && (
        <p className="pill pill-ok" data-testid="tags-saved">
          Saved
        </p>
      )}
    </SectionCard>
  )
}
