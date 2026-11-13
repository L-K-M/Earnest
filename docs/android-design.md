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
  Inline reading, article menus, swipes and undo retain their existing meaning.
  Swipe thresholds give haptic feedback; released and cancelled gestures settle
  back into place.
- **Feeds:** an obvious subscribe action, secondary import/export actions,
  scannable source rows and wrapping cadence choices. Unsubscribe sits in the
  feed menu and asks for confirmation.
- **Reader:** a quieter focused shell, distinct original-page action, compact
  find tools and a height-bounded, scrollable native header so large text does
  not consume the entire reading viewport. Existing WebView restrictions and
  click-to-load behavior remain unchanged.
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

## Verification

Automated coverage includes the existing core and app suites, Compose layout
and navigation tests, a cancelled-swipe regression, narrow-phone cadence and
reader-tool checks at 200% text, and contrast checks for notice/control text in
every theme. Follow-up coverage checks feed-icon taps, wide-search context and
Back, result/button semantics, expanded filter state, full-row 48dp toggles,
load/save exclusion, explicit draft discard, incoming articles during focused
screens and theme selection in short windows. The cancelled-swipe,
large-label chip-height and interaction regressions were observed failing
before their fixes.

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
