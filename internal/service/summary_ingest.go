package service

// ContentFromFeedExcerpt marks a summary-only body at the ingest boundary.
// RecordFetch stores it as source=feed with excerpt_only=true.
const ContentFromFeedExcerpt ContentSource = "feed_excerpt"
