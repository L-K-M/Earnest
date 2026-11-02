# Earnest — UX research report

Sources checked 2026-11-01. Vendor marketing is flagged. Estimates and unconfirmed claims are marked [unverified].

## Landscape (verified facts)

| App | Pattern worth stealing | Source (checked 2026-11-01) |
|---|---|---|
| Reeder | No unread counts; synced timeline position | https://reederapp.com — vendor marketing |
| NetNewsWire | Three panes, free/open source | https://api.github.com/repos/Ranchero-Software/NetNewsWire — 10,494 stars, active 2026-10-25 |
| Unread | Gesture navigation, typography-first, color themes | https://www.goldenhillsoftware.com/unread — vendor marketing |
| Readwise Reader | Inbox / Later / Feed triage, Library archive, Ghostreader AI | https://blog.readwise.io/reader-winter-update, https://readwise.io/reader/update-july2024 — vendor marketing |
| Capy Reader | "Smallish Android RSS reader" | https://api.github.com/repos/jocmp/capyreader — 1,441 stars, active 2026-10-31 |
| Read You | Material You RSS on Android | https://api.github.com/repos/ReadYouApp/ReadYou — 7,633 stars, last push 2026-08 |
| Superhuman / Things / Linear | Keyboard flows, calm defaults, command palette | Design idioms only |

Platform baselines: macOS 27 "Golden Gate" (announced WWDC 2026-06-08, https://mashable.com/tech/macos-27-golden-gate-wwdc-2026) refines Liquid Glass (https://www.apple.com/os/macos — vendor marketing; https://developer.apple.com/documentation/technologyoverviews/liquid-glass). Android: Material 3 Expressive is stable in Compose Material 3 1.4.0 (released 2026-10-21, https://developer.android.com/jetpack/androidx/releases/compose-material3; https://android-developers.googleblog.com/2025/12/whats-new-in-jetpack-compose-december.html); Google positions "Material Android is Compose-first" (https://m3.material.io/blog/material-is-compose-first — vendor marketing). Predictive back: animations extended to 3-button nav in Android 16 (https://www.androidpolice.com/predictive-back-animation-button-navigation-android-16); Play enforcement for targetSdk 16+ is [unverified].

## Information architecture

Default to **timeline (river) with optional three-pane**. Reeder's bet is right: unread counts are debt. Earnest should show *position*, not *obligation*: "You're caught up through Tue 09:12", never "1,247 unread". Keep counts only as an opt-in power mode (per-feed, hidden by default) because some firehose-tracking users genuinely need them.

- **Firehose vs rare gems**: two-speed feeds. Users mark feeds "Digest" (batched into a daily edition) or "Fresh" (in the live river). High-volume feeds (HN, release repos) default to Digest; low-volume blogs default to Fresh. This is the single most important IA decision — it's what makes 500 feeds livable.
- **Caught-up end state**: a designed screen ("All done. 6 items kept." + what was skipped and why), plus a session timer ("15-min catch-up" mode that ends at time or at end-of-edition, whichever first).
- **Triage states**: Readwise's Inbox → Later → Archive is the right skeleton (feed items land in Inbox-ish "Today", swipe/keystroke to Later, reading completes to Archive). Add "Kept" (starred) separate from Later.

## Platform

**Mac** (SwiftUI, Liquid Glass on chrome only, never behind text): sidebar (smart folders, two-speed feeds, tags) + list + reader; toolbar: Mark read, Later, Share, Text size, Search; every command in menus with key equivalents (⌘1/2/3 panes, j/k next/prev, e archive, s save, ⌘K palette, ⌘R refresh, ⌘F find). Multi-window: one window per task; share extension + "Save to Earnest"; Services menu; optional menu-bar mini-digest. Match macOS 27 conventions: unified toolbar, inspector sidebar, stock window chrome.

**Android** (Compose, M3 Expressive): adaptive layouts — list/detail two-pane on tablets and unfolded foldables (WindowSizeClass + `ListDetailPaneScaffold`), single pane on phones [unverified which M3 API set is current]; predictive-back-friendly navigation (back closes reader → list → exits; peek animations); swipe actions on rows (start: read/unread toggle; end: Later) with undo snackbar; long-press multi-select; widget: "Today's edition" with count-free headline stack; notification channels: one per rule ("Watch: Rust security advisories"), quiet-hours respected, silent by default.

**Web** (React+TS): keyboard model identical to Mac; ⌘K palette (Linear-style: fuzzy actions + feed jump + "summarize this"); PWA with service-worker offline cache of current edition + read queue; responsive: 3-pane ≥1280px, 2-pane 768–1280, single column + bottom bar below; no hover-only affordances.

## Reading view

Typography first (Unread's lesson): serif body ~65–75ch measure, size/line-height/font presets (Serif/Sans/Dyslexic-friendly). Full-content extraction with "view original" one click away. Images: lazy, lightbox, `prefers-reduced-motion`; never stretched full-width. Code: monospace block, wrap toggle, copy button, contained horizontal scroll. Dark mode follows system + override; dim images after dark. RTL: logical CSS properties, mirrored lists, bidi-isolated titles. A11y: VoiceOver/TalkBack order (title → source → date → summary → body), Dynamic Type/font scaling, ≥4.5:1 contrast, keyboard-reachable everything, visible focus rings in both themes.

## Onboarding

First run: (1) "Bring your feeds" — OPML import (drag-drop on Mac, file picker on Android) with per-feed preview and dupe detection; (2) optional curated starter packs ("Developer", "Longform", "News"), labelled as suggestions; (3) subscribe to one feed by URL to teach the model; (4) empty state doubles as the *edition generator*: "Nothing yet — import or pick a pack" plus a sample edition of public demo content so the reading view is never cold. Never ask for account/AI/notifications up front.

## AI UX (the Earnest differentiator, and the risk zone)

- **Summaries are doors, not walls**: a 2–3 line summary shown *above* the original, never replacing it; original text is one tap away and the default tab for starred feeds.
- **Labelling**: every AI artifact wears a small badge ("AI summary · model X · 2 min ago"); deterministic content never wears it. No sparkles; plain text badges.
- **Trust signals & citations**: summaries cite sentence-level anchors into the article ("¶4") that scroll the original into view; if extraction was partial, say so ("Feed gave excerpt only — summary covers 40%").
- **Agent actions**: undoable by default (undo snackbar), destructive actions (unsubscribe, mark-all-read) require confirm; every agent action lands in an **Activity log** ("Agent marked 12 HN items read under your 'Noise' rule · undo").
- **Notifications must explain themselves**: "Fired because: rule 'Rust advisory', matched CVE keyword, 2nd today (budget 3/day left 1)."
- **Avoid slop feel**: no generated hero images, no "Great question!" voice, no AI rewriting of feed titles, deterministic formatting for AI text so it reads as annotation, not content.

## Notification UX

Digest-first: default single daily "Edition ready" push at a user-set time; realtime only for user-authored rules (keyword/author/filter matches). Per-rule budgets (max N/day, quiet hours, min-interval) and an escalation ladder: silent badge → digest line → realtime push only for "Alert" rules. Every push deep-links to the exact item and carries its "why".

## Personality: Earnest

Sincere, quiet, a little wry (Wilde's play: earnestness as performance; the app is honestly earnest and allows itself a smile). Copy rules: plain sentences, no exclamation marks, no gamification (no streaks), dry humor in empty states ("Nothing to read. Wilde would approve of doing nothing beautifully."), honesty about AI ("I guessed. The feed gave me only a summary."). Visually: warm paper tones, one accent, no confetti, no red badges.

## 10 UX principles

1. **Position over obligation** — synced reading position, never a wall of unread counts.
2. **Two-speed feeds** — Digest for firehoses, Fresh for gems; the user chooses per feed.
3. **Every session ends** — designed caught-up state + optional time-boxed edition.
4. **Originals are sacred** — AI is annotation layered over source text, always dismissable.
5. **Every agent action is visible and undoable** — Activity log + one-click undo.
6. **Every notification explains its trigger** — rule, match, budget remaining.
7. **Keyboard parity across platforms** — Mac and web share one keymap; Android gets gestures of equal power.
8. **Calm by default, power on demand** — counts, dense lists, rules are opt-in.
9. **Typography is the product** — measure, size, theme, RTL, a11y before any AI feature.
10. **Honest voice** — plain, dry, never hype; AI output is labelled and humble.

## 3 flows (screen-by-screen)

### Flow 1: First run

Mac — welcome → import → first edition:
```
+----------------+-------------------------+------------------+
| Welcome        |   Bring your feeds      |                  |
| ○ Import OPML  |  [Drop OPML here]       |  Preview         |
| ○ Starter packs|  or Start from a pack:  |  128 feeds found |
| ○ Add by URL   |  ( ) Developer (42)     |  3 duplicates    |
|                |  (•) Longform   (31)    |  [Import]        |
+----------------+-------------------------+------------------+
-> "Edition ready · 14 new items · caught up through 09:12"
```
Android: same steps as full-screen cards; import via Storage Access Framework; swipe between steps; final screen shows first edition in single-pane list.

### Flow 2: Daily catch-up

Mac:
```
+----------+---------------------+---------------------------+
| Today    | Edition · 14 items  |  Title of article         |
| ▸ Today  | ▸ Rust advisory     |  Source · 2h · [AI badge] |
| Later (3)|   CVE-2026-…  [k]   |  “Why you got this” ▾     |
| Kept (9) | ▸ The web's …  [k]  |  ─────────────────────    |
| Digests  | ▸ HN digest · 9 ↴   |  Summary (3 lines, ¶ refs)|
| Feeds    | [caught up: end card]|  Original text… (65ch)    |
+----------+---------------------+---------------------------+
```
j/k move, e archive, s Later, ⌘K palette. End card: "All done. 6 kept · 8 skipped. See you tomorrow." Android: list → reader via predictive-back peek; swipe end = Later, swipe start = archive+undo snackbar; end card as bottom sheet.

### Flow 3: Agent-sent alert

Mac: notification banner "Rust advisory: CVE-2026-… (rule: Rust security, 1/3 today)" → click opens window with **Why this fired** panel (rule text, matched excerpt, budget state), summary + original tabs, and Activity log entry with Undo. Android: heads-up notification → deep link to item → top bar "Why?" chip expands trigger sheet; back peek returns to list; snooze and "adjust rule" as inline actions.

## 10 bold ideas (with risk)

| Idea | Risk |
|---|---|
| 1. Kill unread counts entirely (Reeder-style position sync) | Power users revolt; mitigate with opt-in per-feed counts |
| 2. "Edition" mode: all feeds frozen into one morning paper | Missed breaking items; mitigate with Alert rules |
| 3. Two-speed feed classification as the onboarding default | Users don't understand Digest/Fresh at first |
| 4. Session timer that auto-ends reading | Feels paternalistic; make it opt-in |
| 5. Sentence-anchored citations that scroll the original | Extraction quality varies per feed |
| 6. Agent Activity log with one-click undo for everything | Undo stack engineering cost is real |
| 7. "Why this fired" panel on every notification | Rule authors must write legible rules; good friction |
| 8. One keymap shared Mac↔web | Platform-native expectations may conflict |
| 9. No AI-written titles, ever; AI only annotates | Limits flashy demo appeal |
| 10. Dry, Wildean copy with zero gamification | Taste risk; subjective, and hard to localize |

## Anti-patterns

- Red badge counts, streaks, "inbox zero" shaming, confetti.
- AI summary replacing the article or hiding the link out.
- Sparkle glyphs and "✨ AI-powered" marketing inside the UI.
- Infinite scroll with no end state (ironic for a reader about calm).
- Hover-only actions (breaks touch/keyboard/AT).
- Push notifications without rule/budget explanation.
- Destructive agent actions without confirm/undo.
- Feed-bombing onboarding: importing 500 feeds then dumping 5,000 unread.
- Custom window chrome on Mac; bottom-nav-only Android with no back integration.
- Cookie-banner-style "enable AI?" modals on launch.
- Motion everywhere; respect `prefers-reduced-motion`.

## Gaps

- Play enforcement timeline for predictive back on targetSdk 16+ apps [unverified]; current recommended Compose adaptive-layout APIs not confirmed against latest docs [unverified].
- Superhuman/Things/Linear exact keymaps/versions not re-verified; used as design idioms only. Readwise Reader's full current triage labels not enumerated from primary docs.
- Unread's specific gesture set (e.g. shake-to-mark-read) [unverified]; only its vendor "comfortable gesture-based navigation" claim confirmed.
- macOS 27 HIG changes beyond Liquid Glass refinements unread in Apple docs. No user interviews/telemetry — expert judgment plus pattern review.
