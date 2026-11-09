import {Link} from './router'
import {TODAY, viewHref} from './routes'

/** The app icon beside the wordmark's lettering. The lettering is a mask
 * painted in the theme's brand ink, because the wordmark's own dark teal is
 * unreadable on dark backgrounds. With `home`, the lockup links to Today, the
 * home screen, as an app's logo usually does. */
export function Brand({home = false, onNavigate}: {home?: boolean; onNavigate?: () => void}) {
  const lockup = <><img src="/icon-192.png" alt="" width="192" height="192" /><span className="wordmark" role="img" aria-label="Earnest" /></>
  return <h1 className="brand">{home ? <Link to={viewHref(TODAY)} className="brand-link" title="Go to Today" onClick={onNavigate}>{lockup}</Link> : lockup}</h1>
}
