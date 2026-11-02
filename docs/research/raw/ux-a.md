# Earnest — UX research

Checked 2026-11-01. Vendor claims marked **(V)**.

## 1. What the best actually do (verified)

| Source | Fact | Where |
| --- | --- | --- |
| Reeder (2024 rebuild) | No read/unread at all; a synced *timeline position* replaces counts. No third-party sync — position state doesn't map to sync APIs | reeder.app/help, macstories.net/reviews/reeder-a-new-approach-to-following-feeds |
| NetNewsWire | MIT, ~10.5k stars, 7.3b2 Oct 2026; the reference three-pane reader | api.github.com/repos/Ranchero-Software/NetNewsWire |
| Unread 5.1 | Nine sync services; "Subscribe" + "Save" share extensions; 14 themes; offline caching; 100% VoiceOver; $29.99/yr **(V)** | apps.apple.com/us/app/unread-an-rss-reader/id1363637349 |
| Readwise Reader | Library (Inbox → Later → Archive) separate from Feed (Unseen/Seen); cited answers library-wide, agentic moves/tags, over MCP | docs.readwise.io/reader/guides/workflows/library-configuration, readwise.io/mcp |
| Superhuman | One palette, `Cmd+K`, everywhere, printing each action's shortcut so it teaches; `z` = undo; optimistic UI | blog.superhuman.com/how-to-build-a-remarkable-command-palette |
| Things 3.25 | Adaptive layout for Apple's foldable "iPhone Duo": sidebar unfolded, multi-window, desktop-class keyboard | culturedcode.com/things/blog |
| Capy Reader | Native HTML-flattening renderer replacing WebView; All/Unread/Starred in a floating bottom bar; e-ink settings; no AI plans | jocmp.com/2026/10/07/reader-refresh-e-ink-improvements |
| Read You | Material You/Monet theming, OPML in/out, full-content parse, TTS; list-detail on large screens | github.com/ReadYouApp/ReadYou |
| Material 3 Expressive | 8dp spacing; panes (fixed/flexible/floating/semi-permanent); show-hide / levitate / reflow; `ListDetailPaneScaffold`, `NavigationSuiteScaffold`; Navigation 3 | m3.material.io/blog/whats-new-at-io26 |
| Predictive back | Android 15+ enables back-to-home/cross-activity/cross-task by default; intercepting back at the launcher activity disables them; Google's AEP requires it | developer.android.com/distribute/aep/aep-req-predictive-background |
| macOS 27 "Golden Gate" | Edge-to-edge sidebars, uniform toolbars, one window corner radius app-wide, coloured sidebar icons back, Liquid Glass slider clear→tinted **(V)** | apple.com/os/macos, macrumors.com/roundup/macos-27 |
| Feedbin | $70/yr or $7/mo, one plan, no AI **(V)** | feedbin.com/pricing |

A secondary roundup reports App Store and Reddit complaints about Inoreader's and Feedly's AI ("AI slop they keep shoving in my face"). Weak evidence, consistent direction.

## 2. Information architecture

| Pattern | Solves | Costs |
| --- | --- | --- |
| Three-pane (NetNewsWire) | Orientation, bulk scanning, keyboard | Cramped under ~1100px |
| River + synced position (Reeder) | Kills counts and inbox-zero debt; cheap sync | No third-party sync; no counts anywhere |
| Feed/Library triage (Readwise) | Separates pushed-from-outside from chosen-by-you | Two models to learn |

**Recommendation:** three panes on Mac/web, river semantics underneath, triage on top. Reeder's *mechanical* argument beats its aesthetic one: position-based read state cannot desync, so Earnest's API should store a cursor, not a per-item boolean — which is also why Earnest can offer third-party sync later and Reeder cannot.

**Counts:** default off everywhere; never in the dock badge, app icon, or widget. Per-list opt-in only. A badge is a debt counter that grows while you sleep.

**Caught-up end state:** no confetti, no photography (Superhuman's Inbox Zero reward **(V)** is the wrong emotion for a reader). Say what happened — *"That's everything since Tuesday. 9 saved, 2 flagged."* — then stop.

**Time-boxing:** Things splits Today / This Evening **(V)**; scope the river to a slice — "Twenty minutes" — and end with a written summary, not an infinite scroll.

**Firehose next to rare gems:** Feedbin's per-feed Actions (auto star / mark read / push) **(V)** is the right primitive. Earnest: **Vital** (may notify), **Steady** (timeline), **Deep** (weekly digest only), plus a computed *tide* readout naming the feeds that outpace you — weekly, never live.

## 3. Per platform

**Mac.** Toolbar: uniform height, few items, no "Mark All Read" by default (Unread pulled it from the default toolbar on macOS 27.0–27.1 to work around a system bug and restored it on 27.2 — goldenhillsoftware.com/2026/09/unread-501). Sidebar edge-to-edge per macOS 27. Keys: `j/k` navigate, `o` open, `e` done, `s` save, `u` back, `Cmd+K` palette, `?` cheat sheet, `Cmd+1..3` panes. Menu-bar commands with correct enabled state. Multi-window with drag-and-drop (Things on Duo **(V)**). Two share extensions: Subscribe and Save page. Survive the Liquid Glass slider — semantic materials, no hard-coded blur, contrast checked at the tinted extreme.

**Android.** M3 Expressive, 8dp spacing, panes not breakpoints: `ListDetailPaneScaffold` phone→tablet→foldable, `NavigationSuiteScaffold` to swap bottom bar for rail. Never intercept predictive back at the launcher activity; observe with `PRIORITY_SYSTEM_NAVIGATION_OBSERVER` if you must. Swipe: short = save, long = done, label revealed mid-gesture, not after commit. Widget: headlines, Glance, e-ink-friendly. Channels per user-visible category, `IMPORTANCE_LOW` for mere new content — Google's guidance classes new subscribed content as LOW and warns that disguising unimportant notifications as urgent "can produce unnecessary alarm".

**Web.** `Cmd+K`/`Ctrl+K` palette with `preventDefault()` (Chrome and Firefox claim Ctrl+K for the address bar); no single-key bindings. Panes, not a mobile stack — a reader is a desktop tool that happens to be in a browser. Offline: cache bodies and images, queue actions, resync cursors on reconnect.

## 4. Reading view

Measure 62–72 characters, user-adjustable, per-feed override. Real leading, not `line-height: 1.5` by default. Images full-bleed within the measure, captions smaller, never cropped in the reading view (thumbnails may crop). Code: monospace block, horizontal scroll, preserved whitespace, no smart-quote mangling. Dark mode: separate light and dark themes (Unread ships 14 **(V)**), plus true black for OLED and e-ink. RTL: mirror the layout — reading pane, swipe direction, back gesture — and set `dir` per article from the feed's language; don't mirror Latin text inside RTL articles. Accessibility: VoiceOver/TalkBack everywhere, dynamic type to 200%, nothing carried by colour alone.

## 5. Onboarding

OPML import first, not account creation. Parse, preview with per-feed toggles and duplicate detection, then import. Starter packs are **editorial letters**, not directories: 8–15 feeds with a paragraph on why, signed. The first-run empty state explains the two surfaces in three sentences of written copy — never a skeleton list of fake articles. Offer a "20 minutes a day" preset during import; it sets the session default and notification budget in one step.

## 6. AI UX

- **Never replace the original.** Summary above the article, two lines, expandable, full text one keystroke away (`Tab` toggles). If the original is only reachable by dismissing the summary, the summary has won.
- **Label it** with one quiet, reused mark. Not a sparkle icon, not a gradient.
- **Citations as passages, not links.** Readwise links to supporting passages **(V)**; the strongest pattern is hover-to-highlight in place. Distinguish "direct evidence" from "partial support".
- **Say when you don't know.** "Could not verify this claim"; "mixed evidence: two sources agree, one newer conflicts". A system that admits uncertainty reads as honest; one that never does reads as slop.
- **Undoable agent actions.** Every mutation is a reversible event in an undo timeline, not a silent write.
- **An activity log** as a first-class surface: "What Earnest did while you were out", grouped by rule, each row with Undo.
- **Explain every notification**: the rule, the matched text, a link to edit it.
- **Opt-in, with a real off switch.** A "No AI" mode that removes the surfaces, not a toggle that greys them out.

## 7. Notifications

Digest by default; realtime only for rules the user wrote. Per-rule budgets: max N/day, quiet hours, a hard monthly cap shown in settings. Every notification carries the matched phrase, the rule name, and Undo. Separate Android channels per rule category so one can be silenced without silencing the app.

## 8. Personality

"Earnest" is sincere, serious, and a Wilde pun. Plain-spoken and slightly dry — never whimsical, never a mascot. Microcopy states facts; errors say what happened and what to do. Wit belongs only in the empty and caught-up states, where one understated line lands because everything around it is plain.

## 9. Ten UX principles

1. **Position, not counts.** Store a cursor; never a debt.
2. **Two surfaces.** A river you dip into and a shelf you choose for.
3. **The original is always one keystroke from any generated text.**
4. **One palette, one key.** It teaches the shortcut; the shortcut retires the palette.
5. **Every mutation is optimistic, local-first, and undoable — including the agent's.**
6. **Nothing is marked read behind the user's back.** If it is, the UI says so.
7. **Notifications are a budget, not a feature.** Default off, per-rule caps, always explainable.
8. **Adaptive means panes**, not a breakpoint table.
9. **Typography is the product.** Measure, leading, and dark mode are features, not polish.
10. **Empty and end states are written copy** that tells the truth about what just happened.

## 10. Three flows

### A — First run (Mac)

```
┌ Earnest ─────────────────────────┬─────────────────────────────┐
│ ◉ Earnest       Import OPML   ⌘K │  Welcome · step 2 of 3      │
│──────────────────────────────────│  Found 47 feeds. 3 look     │
│ TODAY  (empty)                   │  like firehoses.            │
│ FEEDS  (empty)                   │  How much time do you have? │
│ LIBRARY                          │  ( ) 20 minutes a day       │
│  Later · Archive                 │  ( ) Everything             │
└──────────────────────────────────┴─────────────────────────────┘
```

### A — First run (Android, expanded)

```
┌────────────────────┬──────────────────────────┐
│ ◉ Earnest      ⌕   │  Found 47 feeds          │
│────────────────────│  3 look like firehoses.  │
│ TODAY  (empty)     │  How much time?          │
│ FEEDS  (empty)     │  ( ) 20 min/day          │
│ LIBRARY            │  ( ) Everything          │
│  Later · Archive   │  [ Import 47 feeds ]     │
│  + Add feed        │                          │
└────────────────────┴──────────────────────────┘
```

### B — Daily catch-up (Mac)

```
┌ Earnest ────────────────────────────────────────────────────────┐
│ ◉ Earnest  Today ▾  ⏱ 20 min · 6 left      Import  ⌘K   ?      │
├──────────────┬───────────────────────────┬──────────────────────┤
│ TODAY  214   │ ▸ Stripe: new pricing page│ Stripe: new pricing  │
│  Vital  3    │   Summary · 2 lines       │ page                 │
│  Steady 198  │   ────────────────────    │ [text continues]     │
│  Deep   13   │   Full article text…      │ Tab: hide summary    │
│ FEEDS        │ ▸ Increment: v2           │ S: save · E: done    │
│  · Strate…   │ ▸ Babel: release notes    │                      │
│ LIBRARY      │                           │                      │
│  Later  9    │                           │                      │
└──────────────┴───────────────────────────┴──────────────────────┘
```

### B — Daily catch-up (Android, compact)

```
┌──────────────────────────────┐
│ ◉ Earnest              ⌕  ⋮  │
├──────────────────────────────┤
│ Today ▾    ⏱ 20 min · 6 left │
├──────────────────────────────┤
│ ▸ Stripe: new pricing page   │
│   Summary · 2 lines      ⭐  │
│ ▸ Increment: v2             ›│
│ ▸ Babel: release notes       │
│  ← swipe: save   swipe: done │
├──────────────────────────────┤
│  Today    Feeds   Later  ⋯   │
└──────────────────────────────┘
```

### C — Agent-sent alert (Mac)

```
┌─ Earnest ───────────────────────────────────────────────────────┐
│ ◉ Earnest                              Activity (3)        ⌘K   │
├──────────────┬───────────────────────────┬──────────────────────┤
│ TODAY  214   │ ▸ Stripe: new pricing page│ Why you got this     │
│ FEEDS        │                           │ Rule "Price changes" │
│  · Strate… ● │ [full article text]       │ matched "we are      │
│ LIBRARY      │                           │ changing pricing"    │
│  Later  9    │                           │ [ Edit rule ]        │
│              │                           │ What I did:          │
│              │                           │ 09:14 Summarised 3 ↺ │
│              │                           │ 09:12 Archived 41  ↺ │
└──────────────┴───────────────────────────┴──────────────────────┘
```

### C — Agent alert (Android)

```
┌──────────────────────────────┐
│ ← Stripe: new pricing page   │
├──────────────────────────────┤
│ WHY THIS ALERT               │
│ Rule "Price changes" matched │
│ "we are changing pricing".   │
│ [ Edit rule ]  [ Undo alert ]│
├──────────────────────────────┤
│ [ full article text ]        │
├──────────────────────────────┤
│ What Earnest did · 3         │
│ 09:14 Summarised 3   Undo    │
│ 09:12 Archived 41    Undo    │
└──────────────────────────────┘
```

## 11. Ten bold ideas

| Idea | Risk |
| --- | --- |
| Cursor-only read state, exposed in the public API | Clients built on unread counts can't use it; migration is one-way |
| Sessions with an end ("Twenty minutes", 6 left) | Feels like being cut off; must be dismissible without guilt |
| A weekly *Tide* report naming the feeds that outpace you | Reads as a scolding; must be factual and short |
| Notification budgets enforced in-app, not just by the OS | Users who want firehose alerts will fight it |
| Agent activity log with per-row Undo, on by default | Feels like an audit trail; may scare non-technical users |
| Hover-to-highlight citations into the source passage | Hard with arbitrary HTML; fails on badly marked-up feeds |
| A "No AI" mode that removes the surfaces entirely | Splits the story; loses "AI-native reader" positioning |
| Starter packs as signed editorial letters | Maintenance cost; ages badly |
| Two Mac windows side by side with drag-and-drop | Rarely used, but cheap if state is per-window |
| Typography as the headline feature — measure slider, four typefaces | Sounds trivial next to AI features; is what people feel |

## 12. Anti-patterns

Unread counts in badges, dock, or widgets. Inbox-zero guilt and celebration screens. Sparkle icons and gradient AI chrome. "Mark all read" in the default toolbar. Summaries that cover the original. Unlabelled generated text. Notifications per article. Streaks, goals, leaderboards. Infinite scroll with no anchor. Fake articles in empty states. Cards on desktop. Intercepting predictive back at the launcher activity. Hard-coded blur that breaks under macOS 27's Liquid Glass slider. Single-key web shortcuts. A palette that hides the shortcut instead of teaching it. Silent agent writes. AI summaries on by default.

## Gaps

Not checked: NetNewsWire's keyboard map and pane proportions; Reeder's Mac toolbar specifics; Read You's swipe set and widget design; Superhuman's and Linear's onboarding copy; Glance guidance in depth; foldable book/tabletop posture applied to a reader; whether any reader ships per-rule notification budgets today; Inoreader/Feedly AI pricing beyond the roundup above; research with anxious readers.
