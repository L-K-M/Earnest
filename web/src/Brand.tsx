/** The app icon beside the wordmark's lettering. The lettering is a mask
 * painted in the theme's brand ink, because the wordmark's own dark teal is
 * unreadable on dark backgrounds. */
export function Brand() {
  return <h1 className="brand"><img src="/icon-192.png" alt="" width="192" height="192" /><span className="wordmark" role="img" aria-label="Earnest" /></h1>
}
