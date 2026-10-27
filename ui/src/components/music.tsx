// Shared helpers for the Lidarr L1 music pages (artist grid + artist detail).
// Pure functions over the generated wire types — no client access, so they
// are trivially unit-testable and shared between the two pages.
import type { Artist, Track } from '../api/types'

// humanBytes renders a byte count in the library status bar's units
// (KiB/MiB/GiB/TiB). 0 = '0 B' — callers hide the value when it is 0.
export function humanBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  // One decimal place, trimmed when whole ('1 GiB', '444.7 MiB').
  const s = v.toFixed(1).replace(/\.0$/, '')
  return `${s} ${units[i]}`
}

// fmtDuration renders seconds as 'm:ss' (Lidarr's track durations).
export function fmtDuration(s: number | undefined): string {
  if (!s || s <= 0) return '—'
  const m = Math.floor(s / 60)
  const sec = s % 60
  return `${m}:${String(sec).padStart(2, '0')}`
}

// fmtAudioInfo joins the track's audio facts the Lidarr way:
// 'CODEC - CH channels - Kbps kbps - SR kHz - BD bit' with unknown parts
// omitted (e.g. 'FLAC - 2 - 994 kbps - 44.1 kHz - 16 bit'). All-zero/empty
// input is '—' (the track has no audio metadata yet).
export function fmtAudioInfo(t: Pick<Track, 'Codec' | 'Channels' | 'Kbps' | 'SampleRate' | 'BitDepth'>): string {
  const parts: string[] = []
  if (t.Codec) parts.push(t.Codec)
  if (t.Channels) parts.push(String(t.Channels))
  if (t.Kbps) parts.push(`${t.Kbps} kbps`)
  if (t.SampleRate) parts.push(`${t.SampleRate} kHz`)
  if (t.BitDepth) parts.push(`${t.BitDepth} bit`)
  return parts.length > 0 ? parts.join(' - ') : '—'
}

// ---- grid status ribbon (Lidarr's five ribbon states) ----

// MusicRibbonState is the client-side ribbon derivation the Lidarr grid uses.
// The per-artist counts come from the L1 artist aggregates (TrackCount /
// FileCount / QueuedCount) — never stored, computed by the repository.
export type MusicRibbonState =
  | 'downloading'
  | 'continuing'
  | 'ended'
  | 'missing-monitored'
  | 'missing-unmonitored'

export type RibbonTone = 'ok' | 'warn' | 'err' | 'muted'

export interface MusicRibbon {
  state: MusicRibbonState
  text: string
  tone: RibbonTone
}

// deriveMusicRibbon maps the per-artist counts + fields onto the five Lidarr
// ribbon states:
//   Downloading                  QueuedCount > 0
//   Continuing (all tracks)      all tracks downloaded + status continuing
//   Ended (all tracks)           all tracks downloaded + status ended/inactive
//   Missing Tracks (monitored)   tracked artist still missing files
//   Missing Tracks (unmonitored) same, but the artist is not monitored
// "All tracks downloaded" = FileCount >= TrackCount with TrackCount > 0.
export function deriveMusicRibbon(
  a: Pick<Artist, 'Monitored' | 'Status' | 'TrackCount' | 'FileCount' | 'QueuedCount'>,
): MusicRibbon {
  const tracks = a.TrackCount ?? 0
  const files = a.FileCount ?? 0
  const allDownloaded = tracks > 0 && files >= tracks
  if ((a.QueuedCount ?? 0) > 0) {
    return { state: 'downloading', text: 'Downloading', tone: 'warn' }
  }
  if (allDownloaded) {
    if (a.Status === 'ended' || a.Status === 'inactive') {
      return { state: 'ended', text: 'Ended (all tracks downloaded)', tone: a.Status === 'ended' ? 'ok' : 'muted' }
    }
    return { state: 'continuing', text: 'Continuing (all tracks downloaded)', tone: 'ok' }
  }
  if (a.Monitored) {
    return { state: 'missing-monitored', text: 'Missing Tracks (artist monitored)', tone: 'err' }
  }
  return { state: 'missing-unmonitored', text: 'Missing Tracks (artist not monitored)', tone: 'muted' }
}

// MUSIC_RIBBON_LEGEND is the library footer legend — the five states in
// Lidarr's order, each with the tone used by both the ribbon and the legend
// swatch.
export const MUSIC_RIBBON_LEGEND: { state: MusicRibbonState; label: string; tone: RibbonTone }[] = [
  { state: 'downloading', label: 'Downloading', tone: 'warn' },
  { state: 'continuing', label: 'Continuing (all tracks downloaded)', tone: 'ok' },
  { state: 'ended', label: 'Ended (all tracks downloaded)', tone: 'ok' },
  { state: 'missing-monitored', label: 'Missing Tracks (artist monitored)', tone: 'err' },
  { state: 'missing-unmonitored', label: 'Missing Tracks (artist not monitored)', tone: 'muted' },
]

// ---- album type groups (the detail accordion groups) ----

// albumTypeLabel renders an album_type enum value as a Lidarr group header
// word ('album' -> 'Album', 'djmix' -> 'DJ Mix'). Empty/unknown = Album.
export function albumTypeLabel(t: string | undefined): string {
  switch (t) {
    case 'ep':
      return 'EP'
    case 'single':
      return 'Single'
    case 'compilation':
      return 'Compilation'
    case 'live':
      return 'Live'
    case 'remix':
      return 'Remix'
    case 'broadcast':
      return 'Broadcast'
    case 'demo':
      return 'Demo'
    case 'djmix':
      return 'DJ Mix'
    case 'other':
      return 'Other'
    default:
      return 'Album'
  }
}

// The group order for the detail accordion (Lidarr's default sort).
export const ALBUM_TYPE_ORDER = [
  'album',
  'single',
  'ep',
  'compilation',
  'live',
  'remix',
  'broadcast',
  'demo',
  'djmix',
  'other',
] as const

// relTime renders an ISO timestamp as a compact relative date for the
// history modal ('just now', '5 min ago', '3 days ago', else the date).
export function relTime(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const diff = Date.now() - t
  const min = Math.floor(diff / 60000)
  if (min < 1) return 'just now'
  if (min < 60) return `${min} min ago`
  const h = Math.floor(min / 60)
  if (h < 24) return `${h} h ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} days ago`
  return new Date(t).toLocaleDateString()
}

// FALLBACK_PROFILE_LABEL is the card footer's quality line when the artist
// has no quality profile set (the music kind's server default).
export const FALLBACK_PROFILE_LABEL = 'Lossless'
