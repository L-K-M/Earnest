import {useState} from 'react'
import type {Subscription} from './reader'

// Feed icons and article thumbnails are copies Earnest stored when the feed
// and the article arrived, so showing them never contacts a publisher. A copy
// that fails to load falls back to the feed's letter mark, or disappears.

// A letter mark for a feed. The swatch index comes from the title so a feed
// keeps the same mark everywhere; the letter carries the meaning, not the
// colour.
export function feedSwatch(title: string): number {
  let hash = 0
  for (const char of title) hash = (hash * 31 + (char.codePointAt(0) ?? 0)) % 7
  return hash
}

export function FeedMark({title}: {title: string}) {
  const letter = Array.from(title.trim())[0]?.toLocaleUpperCase() ?? '?'
  return <span className="feed-mark" aria-hidden="true" data-swatch={feedSwatch(title)}>{letter}</span>
}

export function FeedIcon({subscription, title}: {subscription?: Subscription; title: string}) {
  const [broken, setBroken] = useState<string>()
  const src = subscription?.icon_url
  if (!src || broken === src) return <FeedMark title={title} />
  return <img className="feed-icon" src={src} alt="" width={16} height={16} decoding="async" onError={() => setBroken(src)} />
}

export function EntryThumbnail({src}: {src: string}) {
  const [broken, setBroken] = useState<string>()
  if (broken === src) return null
  return <img className="entry-thumb" src={src} alt="" width={64} height={64} loading="lazy" decoding="async" onError={() => setBroken(src)} />
}
