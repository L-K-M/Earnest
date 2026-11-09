import {useState} from 'react'
import {FeedMark} from './Sidebar'
import type {Subscription} from './reader'

// Feed icons and article thumbnails are copies Earnest stored when the feed
// and the article arrived, so showing them never contacts a publisher. A copy
// that fails to load falls back to the feed's letter mark, or disappears.

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
