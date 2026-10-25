import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type {
  CFRule,
  CustomFormat,
  CustomFormatScoreResult,
  CustomFormatsSettings,
} from '../../../api/types'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// CustomFormatsSection is the custom-formats section of the P5 settings
// shell: the scored rule groups (all-rules-match semantics, parity with
// Sonarr's CF engine) plus the score tester, which runs a candidate release
// against the saved formats through the /custom-formats/score endpoint.
const CF_FIELDS = ['title', 'quality', 'source', 'kind', 'tag', 'size_mb'] as const
const CF_OPS = ['contains', 'not_contains', 'equals', 'regex', 'exists', 'gt', 'lt'] as const

export function CustomFormatsSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<CustomFormatsSettings>(
    'custom-formats',
  )
  const [formats, setFormats] = useState<CustomFormat[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  // New-format draft: name/score plus a rule being typed (the "add rule"
  // button appends it to the draft before the format itself is added).
  const [newName, setNewName] = useState('')
  const [newScore, setNewScore] = useState('10')
  const [ruleField, setRuleField] = useState<(typeof CF_FIELDS)[number]>('title')
  const [ruleOp, setRuleOp] = useState<(typeof CF_OPS)[number]>('contains')
  const [ruleValue, setRuleValue] = useState('')
  const [draftRules, setDraftRules] = useState<CFRule[]>([])

  // Score tester state (the POST is user-initiated; the mock/dev 404 shows
  // as the tester's own error line, not the section gate).
  const [sTitle, setSTitle] = useState('')
  const [sQuality, setSQuality] = useState('')
  const [sSource, setSSource] = useState('')
  const [sKind, setSKind] = useState('')
  const [sSize, setSSize] = useState('')
  const [sTags, setSTags] = useState('')
  const [scoreResult, setScoreResult] = useState<CustomFormatScoreResult | null>(null)
  const [scoreErr, setScoreErr] = useState('')
  const [scoring, setScoring] = useState(false)

  useEffect(() => {
    if (doc) setFormats(doc.formats ?? [])
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="cf-error" />
  }

  function addDraftRule() {
    if (ruleOp !== 'exists' && ruleValue.trim() === '') return
    setDraftRules((rs) => [...rs, { field: ruleField, operator: ruleOp, value: ruleValue.trim() }])
    setRuleValue('')
  }

  function addFormat() {
    const name = newName.trim()
    if (!name) return
    const id = formats.reduce((m, f) => Math.max(m, f.id), 0) + 1
    setFormats((fs) => [
      ...fs,
      { id, name, enabled: true, score: Number(newScore) || 0, rules: draftRules },
    ])
    setNewName('')
    setNewScore('10')
    setDraftRules([])
    setRuleValue('')
  }

  function handleSave() {
    void submit(async () => {
      const next: CustomFormatsSettings = { formats }
      await client.putSettingsSection<CustomFormatsSettings>('custom-formats', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  async function runScore() {
    if (scoring) return
    setScoring(true)
    setScoreErr('')
    setScoreResult(null)
    try {
      const res = await client.scoreCustomFormats({
        title: sTitle,
        quality: sQuality,
        source: sSource,
        kind: sKind,
        size_mb: sSize === '' ? 0 : Number(sSize) || 0,
        tags: sTags
          .split(',')
          .map((t) => t.trim())
          .filter(Boolean),
      })
      setScoreResult(res)
    } catch (e) {
      setScoreErr(e instanceof Error ? e.message : 'scoring failed')
    } finally {
      setScoring(false)
    }
  }

  return (
    <>
      <SectionCard
        title="Custom formats"
        hint="Scored rule groups. A format applies (adds its score) only when every rule matches. Use negative scores to penalise."
        testId="cf-card"
      >
        {formats.map((f) => (
          <div className="subform" key={f.id} data-testid={`cf-item-${f.id}`}>
            <input
              value={f.name}
              onChange={(e) =>
                setFormats((fs) => fs.map((x) => (x.id === f.id ? { ...x, name: e.target.value } : x)))
              }
              aria-label={`cf-name-${f.id}`}
              data-testid={`cf-name-${f.id}`}
            />
            <input
              type="number"
              value={f.score}
              onChange={(e) =>
                setFormats((fs) =>
                  fs.map((x) => (x.id === f.id ? { ...x, score: Number(e.target.value) || 0 } : x)),
                )
              }
              aria-label={`cf-score-${f.id}`}
              data-testid={`cf-score-${f.id}`}
            />
            <label className="check">
              <input
                type="checkbox"
                checked={f.enabled}
                onChange={(e) =>
                  setFormats((fs) =>
                    fs.map((x) => (x.id === f.id ? { ...x, enabled: e.target.checked } : x)),
                  )
                }
                data-testid={`cf-enabled-${f.id}`}
              />
              enabled
            </label>
            <button
              className="ghost"
              onClick={() => setFormats((fs) => fs.filter((x) => x.id !== f.id))}
              data-testid={`cf-del-${f.id}`}
            >
              Delete
            </button>
            <div className="cf-rules" data-testid={`cf-rules-${f.id}`}>
              {f.rules.map((r, i) => (
                <span className="pill" key={i} data-testid={`cf-rule-${f.id}-${i}`}>
                  {r.field} {r.operator} {r.value}
                  <button
                    className="ghost"
                    onClick={() =>
                      setFormats((fs) =>
                        fs.map((x) =>
                          x.id === f.id ? { ...x, rules: x.rules.filter((_, j) => j !== i) } : x,
                        ),
                      )
                    }
                    data-testid={`cf-rule-del-${f.id}-${i}`}
                  >
                    ×
                  </button>
                </span>
              ))}
              {f.rules.length === 0 && <span className="dim small">no rules (a format needs at least one to save)</span>}
            </div>
          </div>
        ))}
        {formats.length === 0 && <p className="dim small">No custom formats yet.</p>}
        {saved && (
          <p className="pill pill-ok" data-testid="cf-saved">
            Saved
          </p>
        )}
        <SaveError error={saveError} testId="cf-error-save" />
        <button onClick={handleSave} disabled={busy || loading} data-testid="cf-save">
          {busy ? 'Saving…' : 'Save custom formats'}
        </button>
      </SectionCard>

      <SectionCard title="Add a custom format" testId="cf-add-card">
        <label htmlFor="cf-new-name">Name</label>
        <input id="cf-new-name" value={newName} onChange={(e) => setNewName(e.target.value)} data-testid="cf-new-name" />
        <label htmlFor="cf-new-score">Score</label>
        <input id="cf-new-score" type="number" value={newScore} onChange={(e) => setNewScore(e.target.value)} data-testid="cf-new-score" />
        <label htmlFor="cf-rule-field">Rule field</label>
        <select id="cf-rule-field" value={ruleField} onChange={(e) => setRuleField(e.target.value as never)} data-testid="cf-rule-field">
          {CF_FIELDS.map((f) => (
            <option key={f} value={f}>
              {f}
            </option>
          ))}
        </select>
        <label htmlFor="cf-rule-op">Operator</label>
        <select id="cf-rule-op" value={ruleOp} onChange={(e) => setRuleOp(e.target.value as never)} data-testid="cf-rule-op">
          {CF_OPS.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
        {ruleOp !== 'exists' && (
          <>
            <label htmlFor="cf-rule-value">Value</label>
            <input id="cf-rule-value" value={ruleValue} onChange={(e) => setRuleValue(e.target.value)} data-testid="cf-rule-value" />
          </>
        )}
        <div>
          <button className="ghost" onClick={addDraftRule} data-testid="cf-add-rule">
            Add rule
          </button>{' '}
          <button className="ghost" onClick={addFormat} data-testid="cf-add">
            Add format
          </button>
        </div>
      </SectionCard>

      <SectionCard title="Score tester" hint="Run a candidate release name against the saved formats." testId="cf-score-card">
        <label htmlFor="cf-sc-title">Title</label>
        <input id="cf-sc-title" value={sTitle} onChange={(e) => setSTitle(e.target.value)} data-testid="cf-sc-title" />
        <div className="cf-row">
          <div>
            <label htmlFor="cf-sc-quality">Quality</label>
            <input id="cf-sc-quality" value={sQuality} onChange={(e) => setSQuality(e.target.value)} data-testid="cf-sc-quality" />
          </div>
          <div>
            <label htmlFor="cf-sc-source">Source</label>
            <input id="cf-sc-source" value={sSource} onChange={(e) => setSSource(e.target.value)} data-testid="cf-sc-source" />
          </div>
          <div>
            <label htmlFor="cf-sc-size">Size (MB)</label>
            <input id="cf-sc-size" type="number" value={sSize} onChange={(e) => setSSize(e.target.value)} data-testid="cf-sc-size" />
          </div>
          <div>
            <label htmlFor="cf-sc-kind">Kind</label>
            <input id="cf-sc-kind" value={sKind} onChange={(e) => setSKind(e.target.value)} data-testid="cf-sc-kind" />
          </div>
          <div>
            <label htmlFor="cf-sc-tags">Tags (comma-sep)</label>
            <input id="cf-sc-tags" value={sTags} onChange={(e) => setSTags(e.target.value)} data-testid="cf-sc-tags" />
          </div>
        </div>
        <button onClick={runScore} disabled={scoring} data-testid="cf-score">
          {scoring ? 'Scoring…' : 'Score release'}
        </button>
        {scoreErr && (
          <p className="error" data-testid="cf-score-error">
            {scoreErr}
          </p>
        )}
        {scoreResult && (
          <div data-testid="cf-score-result">
            <span className="pill" data-testid="cf-score-total">
              total: {scoreResult.total}
            </span>
            {scoreResult.matches.map((m) => (
              <span className="pill" key={m.name} data-testid={`cf-match-${m.name}`}>
                {m.name}: {m.score}
              </span>
            ))}
            {scoreResult.matches.length === 0 && <span className="dim small"> no format matched</span>}
          </div>
        )}
      </SectionCard>
    </>
  )
}
