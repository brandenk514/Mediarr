// Duotone palette + TMDB genre tone map — ported from Seerr (issue #147).
//
// Ported verbatim from:
//   seerr/src/components/Discover/constants.ts @ 9399fe5a
//   (Seerr fork; the values feed the fork's TMDB duotone backdrop filters
//   and Discover genre cards).
//
// The CSS twin of this file is the --tone-* custom properties in
// ui/src/styles/tokens.css; issue #151 (Discover) consumes both.
//
// ---------------------------------------------------------------------------
// MIT License
//
// Copyright (c) 2020 sct
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
// ---------------------------------------------------------------------------

export type AvailableColors =
  | 'black'
  | 'red'
  | 'darkred'
  | 'blue'
  | 'lightblue'
  | 'darkblue'
  | 'orange'
  | 'darkorange'
  | 'green'
  | 'lightgreen'
  | 'purple'
  | 'darkpurple'
  | 'yellow'
  | 'pink'

/** [dark, light] hex pairs (no leading '#'), verbatim from the Seerr source. */
export const colorTones: Record<AvailableColors, [string, string]> = {
  red: ['991B1B', 'FCA5A5'],
  darkred: ['1F2937', 'F87171'],
  blue: ['032541', '01b4e4'],
  lightblue: ['1F2937', '60A5FA'],
  darkblue: ['1F2937', '2864d2'],
  orange: ['92400E', 'FCD34D'],
  lightgreen: ['065F46', '6EE7B7'],
  green: ['087d29', '21cb51'],
  purple: ['5B21B6', 'C4B5FD'],
  yellow: ['777e0d', 'e4ed55'],
  darkorange: ['552c01', 'd47c1d'],
  black: ['1F2937', 'D1D5DB'],
  pink: ['9D174D', 'F9A8D4'],
  darkpurple: ['480c8b', 'a96bef'],
}

/** All duotone tone names (14), in source order. */
export const toneNames: AvailableColors[] =
  Object.keys(colorTones) as AvailableColors[]

/** TMDB genre id -> duotone tone, verbatim from the Seerr source. */
export const genreColorMap: Record<number, AvailableColors> = {
  0: 'black',
  28: 'red', // Action
  12: 'darkpurple', // Adventure
  16: 'blue', // Animation
  35: 'orange', // Comedy
  80: 'darkblue', // Crime
  99: 'lightgreen', // Documentary
  18: 'pink', // Drama
  10751: 'yellow', // Family
  14: 'lightblue', // Fantasy
  36: 'orange', // History
  27: 'black', // Horror
  10402: 'blue', // Music
  9648: 'purple', // Mystery
  10749: 'pink', // Romance
  878: 'lightblue', // Science Fiction
  10770: 'red', // TV Movie
  53: 'black', // Thriller
  10752: 'darkred', // War
  37: 'orange', // Western
  10759: 'darkpurple', // Action & Adventure
  10762: 'blue', // Kids
  10763: 'black', // News
  10764: 'darkorange', // Reality
  10765: 'lightblue', // Sci-Fi & Fantasy
  10766: 'pink', // Soap
  10767: 'lightgreen', // Talk
  10768: 'darkred', // War & Politics
}

/**
 * Tone pair for a TMDB genre id. Unmapped ids fall back to the black tone,
 * mirroring the Seerr call sites (`genreColorMap[genre.id] ?? genreColorMap[0]`).
 */
export function toneForGenre(genreId: number): [string, string] {
  const tone = genreColorMap[genreId] ?? genreColorMap[0]
  return colorTones[tone]
}
