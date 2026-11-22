# Android design

The Android client keeps Earnest's existing identity and capabilities, but uses
mobile composition rather than shrinking the web client's sidebar and toolbars.

## Direction

A quiet, editorial reading experience with Material 3 Expressive controls and
motion. Warm paper is the canvas, teal identifies actions, and small destination
accents help with orientation. Typography and spacing do most of the work; an
article list is not a stack of bordered cards.

The web capability review covered the front page, reading streams, shelves,
reader, feeds, search, settings and agent activity. Android keeps its four main
destinations, folder drawer, inline reading and dedicated reader. This redesign
does not add web-only capabilities or change the sync, account or content-origin
contracts.

## Shared system

`clients/android/app/src/main/java/ch/lkmc/earnest/DesignSystem.kt` owns:

- A 4dp spacing rhythm: 4, 8, 12, 16, 20, 24, 32 and 48dp. Article and feed rows
  share a 20dp gutter. Forms and longer reading surfaces have bounded widths.
- A complete sp-based type scale with distinct headings, article titles,
  metadata, body text and control labels. The front-page lead uses a serif face
  for editorial emphasis; navigation and controls use native sans-serif text.
- `SettingsGroup`, `ScreenIntro`, `NoticeBanner`, `QuietState` and `LoadingState`
  for consistent grouping and feedback. `ReaderFilterChip` sizes its surface to
  the scaled label line height, including Android's nonlinear font scaling. Status is conveyed in words and icons,
  not just color. Notices announce updates politely to accessibility services.

`Theme.kt` applies the expressive motion scheme, rounded component shapes and
existing accessible light, dark, true-black and monochrome e-ink palettes.
Material 3 is explicitly pinned to 1.5.0-rc01 because its expressive theme and
motion APIs are not public in the BOM's 1.4 release. No third-party UI toolkit,
font download or additional runtime network dependency is introduced.

## Composition and interaction

- **Front page:** dated masthead, grouped window selector, one emphasized lead,
  flatter secondary stories, clear coverage and read metadata, and expandable
  sources. Absent photographs do not leave an empty image slot. The masthead
  continues to scroll with the stories.
- **Articles and shelves:** aligned, full-width rows with quiet separators,
  consistent source/time metadata, stronger headlines and larger thumbnails.
  Article menus, collapsed-row swipes and undo retain their existing meaning.
  Swipe thresholds give haptic feedback; released and cancelled gestures settle
  back into place.
- **Feeds:** an obvious subscribe action, secondary import/export actions,
  scannable source rows and wrapping cadence choices. Unsubscribe sits in the
  feed menu and asks for confirmation.
- **Reader:** a quieter focused shell, distinct original-page action, compact
  find tools and a height-bounded, scrollable native header so large text does
  not consume the entire reading viewport. Existing WebView restrictions and
  click-to-load behavior remain unchanged.
- **Article swipes:** the article follows a sideways drag at a third of the
  finger's speed and a label at the uncovered edge names the move, or says
  there is no article that way. The label fills and the device ticks at the
  72dp threshold.
- **Search:** the query leads, filters use progressive disclosure, results are
  readable rows rather than text buttons, and publisher articles remain
  separate from agent notes.
- **Settings:** account reading preferences precede imports and server-level AI
  configuration. Logical groups, wrapping controls and a local typography
  preview replace the previous undifferentiated form. The labelled reading
  preferences Save bar stays reachable while the form scrolls. Reload asks
  before discarding local edits or a frozen save retry; Load preserves both.
- **Connect:** a focused, scrollable form with URL keyboard support, honest
  request progress, clear authorization-code instructions and retry feedback.

Focused search, settings and phone-reader screens hide primary navigation to
avoid competing tasks. Back returns to the previous reading destination;
search preserves the folder/feed context. System bars follow the selected
palette. Short landscape windows use a navigation rail to preserve reading
height. Insets, keyboard resizing and scrolling keep content clear of system
UI, and actions retain Android's standard touch targets. Find focuses its
input and temporarily hides the reader header to leave room for the keyboard.
Wide-screen search retains the open article; resizing search into a compact
window keeps the search task visible, and Back restores the reading context.
The adaptive transition stays bound beneath focused settings/search screens,
with inactive panes empty and hidden from accessibility, so incoming articles
cannot leave navigation history and the visible pane out of step.

## Inline reading stability

The list owns the opened article's measured height and cached-copy status, so
recycling its lazy item does not shrink the body and expand it again. Width,
typography, zoom and density changes invalidate the height measurement. A reflow
inside a measured body returns to its headline while the new height is learned,
instead of applying the old deep offset to a temporary measuring viewport. Expansion
queues a one-shot headline anchor against the new composition: an offset inside
a tall, collapsed body must not be reinterpreted as many later article rows.
Collapsing from inside the body anchors its own headline instead.

The native WebView canvas is themed before loading, and its draw layer is clipped
to its bounds. Cached HTML themes the document canvas as well as the body. A
recreated reader clears its cached-copy indicator when live content commits
successfully. The content-origin security policy is unchanged.

An expanded article is as tall as its document. Its WebView uses `WRAP_CONTENT`
under an unbounded height, so Chromium supplies the height and rasters only
what is on screen. Never size it with `Modifier.height` or a constraint: Compose
constraints cannot represent a long article's height, while a layout result
can. Never measure it with the scroll range either, which can include the
viewport and feed back into the height. The row holds its last height until the
document has loaded and stayed the same size for 300ms. The reading place is
saved as a fraction of the body, because folding and rotation change its
height; until the body is measured again the list waits on the headline. The
place is approximate when a new width reflows text and images unevenly, as an
exact anchor would need script in the page.

Article pages run without JavaScript, so a swipe cannot ask what is under the
finger. The WebView's clamped horizontal overscroll answers instead: Chromium
reports it only when no element on the page took the movement. Without that
report a drag on the page never changes article. If a WebView stops reporting,
the swipe stops working and the page's own scrolling is unaffected.

## Verification

Automated coverage includes the existing core and app suites, Compose layout
and navigation tests, a cancelled-swipe regression, narrow-phone cadence and
reader-tool checks at 200% text, and contrast checks for notice/control text in
every theme. Follow-up coverage checks feed-icon taps, wide-search context and
Back, result/button semantics, expanded filter state, full-row 48dp toggles,
load/save exclusion, explicit draft discard, incoming articles during focused
screens and theme selection in short windows. The cancelled-swipe,
large-label chip-height and interaction regressions were observed failing
before their fixes. Inline-reading coverage adds switching below a tall body,
footer/Back collapse, off-screen Back without a viewport jump, anchoring near
the start of the list, lazy-item disposal, width-only measurement invalidation,
native canvas colors and clipping. The scroll and
canvas regressions were reproduced before the fixes. An API 34 emulator replay
confirmed an unchanged tapped-headline position, retained item height after
recreation, and no white flash during dark article switches. Light and narrow
320dp/200% text presentations were also inspected. This does not establish behavior
on the Huawei Android 12 device in the supplied recording.

For visual acceptance, inspect a populated front page, article list, inline
article, reader, feeds, search/results, settings and connect screen. Repeat on
a 320dp phone, at 200% system text, in landscape/tablet widths and in dark,
true-black and e-ink modes. Open the keyboard in URL, search, find and settings
dialogs. Check Back, drawer dismissal, action menus and empty/error states.
The redesign was visually reviewed on an API 36 arm64 emulator at phone,
320dp narrow-phone, 800dp and 1200dp tablet, and landscape sizes. The narrow
phone was also checked at 200% system text. Light, dark, true-black and e-ink
screens, loading/empty/error states, URL/search/find keyboards, and the sticky
settings action were inspected over multiple passes. The disposable fixture
activity and its sample data are not included in the app or patch.

Do not infer live device-authorization approval, server sync or push delivery
from fixture-based visual checks; those require a reachable deployment.
Physical-device behavior and widget rendering remain outside these checks.

## References

- [Android mobile design](https://developer.android.com/design/ui/mobile)
- [Material 3 Expressive](https://m3.material.io/blog/building-with-m3-expressive)
- [Android Expressive overview](https://blog.google/products-and-platforms/platforms/android/material-3-expressive-android-wearos-launch/)
- [Apple Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines/)
