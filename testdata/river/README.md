# River fixtures

Every JSON file is executed by the Postgres service integration test, EarnestKit
and the Android app tests. IDs and expected IDs are numeric, ordered by ascending
sequence. `now` is fixed UTC time. `today_decay_days` is optional and defaults to
two. Entries include their effective `seen` and `shelf` values. Subscription and
mute `deleted` fields are optional and default to false.

`eligibility.json` covers the initial import sequence and timestamp boundaries,
null and old publisher dates, first-seen decay boundaries, future first-seen
clocks, non-decaying Now feeds, week feeds, mute matching, tombstones and state.
`custom-decay.json` checks that the Today setting changes its window while Now
and week retain their own behavior.

The Go test inserts these projections into Postgres and calls `Service.Entries`
with `ViewRiver`. Native tests execute their production pure filter over the
same projections. Android also checks the Today/Edition split.
