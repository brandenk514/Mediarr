import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Badge, BadgeGroup } from './badge'

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

  it('merges extra classes onto the badge', () => {
    render(
      <Badge testId="b" className="px-3">
        custom
      </Badge>,
    )
    expect(screen.getByTestId('b')).toHaveClass('px-3', 'border')
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
