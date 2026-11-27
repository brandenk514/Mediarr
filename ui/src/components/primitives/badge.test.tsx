import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { colorTones, toneNames } from '../../lib/tone-map'
import { Badge, BadgeGroup, type BadgeTone } from './badge'

// hexToRgb normalizes the Seerr palette pairs (hex) into the rgb() form the
// computed style resolves to, so the duotone assertions check the actual
// rendered color rather than the style attribute text.
function hexToRgb(hex: string): string {
  const n = parseInt(hex.replace('#', ''), 16)
  return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`
}

describe('Badge', () => {
  it('renders its label', () => {
    render(<Badge testId="b">Monitored</Badge>)
    expect(screen.getByTestId('b')).toHaveTextContent('Monitored')
  })

  it('applies the neutral tone by default', () => {
    render(<Badge testId="b">genre</Badge>)
    expect(screen.getByTestId('b')).toHaveClass('border-border', 'bg-surface-raised', 'text-text-muted', 'rounded-full')
  })

  it('applies tone-specific classes for status, kind, and genre variants', () => {
    render(
      <>
        <Badge tone="success" testId="ok">
          downloaded
        </Badge>
        <Badge tone="warning" testId="warn">
          queued
        </Badge>
        <Badge tone="error" testId="err">
          failed
        </Badge>
        <Badge tone="accent" testId="kind">
          movie
        </Badge>
        <Badge tone="info" testId="info">
          series
        </Badge>
      </>,
    )
    expect(screen.getByTestId('ok')).toHaveClass('text-success', 'bg-success/15')
    expect(screen.getByTestId('warn')).toHaveClass('text-warning')
    expect(screen.getByTestId('err')).toHaveClass('text-error')
    expect(screen.getByTestId('kind')).toHaveClass('text-accent')
    expect(screen.getByTestId('info')).toHaveClass('text-sky-400')
  })

  it('applies the semantic tone classes for all six semantic tones', () => {
    const semantic: Array<[BadgeTone, string[]]> = [
      ['neutral', ['bg-surface-raised', 'text-text-muted']],
      ['accent', ['text-accent', 'bg-accent/15']],
      ['info', ['text-sky-400', 'bg-sky-500/15']],
      ['success', ['text-success', 'bg-success/15']],
      ['warning', ['text-warning', 'bg-warning/15']],
      ['error', ['text-error', 'bg-error/15']],
    ]
    render(
      <BadgeGroup>
        {semantic.map((entry, i) => (
          <Badge key={entry[0]} tone={entry[0]} testId={`sem-${i}`}>
            {entry[0]}
          </Badge>
        ))}
      </BadgeGroup>,
    )
    semantic.forEach(([, classes], i) => {
      const el = screen.getByTestId(`sem-${i}`)
      for (const c of classes) expect(el).toHaveClass(c)
    })
  })

  it('merges extra classes onto the badge', () => {
    render(
      <Badge testId="b" className="px-3">
        custom
      </Badge>,
    )
    expect(screen.getByTestId('b')).toHaveClass('px-3', 'border')
  })
})

describe('Badge duotone tones (Seerr palette, issue #147)', () => {
  it('renders every duotone tone with its dark-hex background and light-hex text/border', () => {
    render(
      <BadgeGroup>
        {toneNames.map((name) => (
          <Badge key={name} tone={name} testId={`tone-${name}`}>
            {name}
          </Badge>
        ))}
      </BadgeGroup>,
    )
    expect(toneNames).toHaveLength(14)
    for (const name of toneNames) {
      const [dark, light] = colorTones[name]
      const el = screen.getByTestId(`tone-${name}`)
      expect(el).toHaveStyle({
        backgroundColor: hexToRgb(dark),
        color: hexToRgb(light),
        borderColor: hexToRgb(light),
      })
    }
  })

  it('renders the red duotone genre pill without semantic tone classes', () => {
    render(<Badge testId="g" tone="red">Action</Badge>)
    const el = screen.getByTestId('g')
    // Pill geometry unchanged; the duotone color comes from the tone pair,
    // not from a translucent semantic tint.
    expect(el).toHaveClass('rounded-full', 'border')
    expect(el).not.toHaveClass('text-success', 'text-accent', 'text-sky-400', 'bg-surface-raised')
  })

  it('keeps the neutral tone as the default for generic genre chips', () => {
    render(<Badge testId="g">Drama</Badge>)
    expect(screen.getByTestId('g')).toHaveClass('bg-surface-raised', 'text-text-muted')
  })
})

describe('BadgeGroup', () => {
  it('renders its badges in a wrapping row', () => {
    render(
      <BadgeGroup testId="group">
        <Badge testId="a">Drama</Badge>
        <Badge testId="b">Crime</Badge>
      </BadgeGroup>,
    )
    const group = screen.getByTestId('group')
    expect(group).toHaveClass('inline-flex', 'flex-wrap')
    expect(group).toContainElement(screen.getByTestId('a'))
    expect(group).toContainElement(screen.getByTestId('b'))
  })
})
