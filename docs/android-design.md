# Android design

The Android client keeps Earnest's existing identity and capabilities, but uses
mobile composition rather than shrinking the web client's sidebar and toolbars.

## Direction

**Morning paper, expressive controls.** The content reads like a paper: serif
titles and headlines, warm stock, hairline rules and one lead story. The
controls are Material 3 Expressive: shape-changing buttons, connected choices,
a loading indicator and spring motion. Typography and spacing do the work; an
article list is not a stack of bordered cards.

Color has three jobs and no others:

- **Teal is an action:** the primary button, links and the text cursor.
- **Peach is a choice:** selected chips, segments and tonal buttons. E-ink
  shows a choice as solid black.
- **One tone is a place:** orange Front page, blue Articles, plum Saved, green
  Feeds, amber Later, purple Activity and agents. A tone colors a place's
  icon, indicator and empty state, never body text.

## Shared system

| File | Owns |
|---|---|
| `Theme.kt` | The five chrome palettes, place tones, monochrome rules and system bar icons |
| `EarnestType.kt` | The type scale (serif for display, headlines and large titles, sans for everything else) and the story styles |
| `DesignSystem.kt` | Spacing, widths, grouped shapes, section headers, notices, loading and quiet states, pills, settings groups |
| `Controls.kt` | Buttons, connected choices, menus and option rows |
| `EarnestIcons.kt` | Original stroke icons, solid when selected |
| `FeedAvatar.kt` | Feed icons and letter marks, matching the web client's |
| `SettingsActionBar.kt` | The one save bar for every settings draft |

Use these before adding a screen-local style:

- Spacing is 4, 8, 12, 16, 20, 24, 32 and 48dp. Lists, titles and the menu
  glyph share a 20dp gutter. The reader's header uses the document's own 16dp.
  Reading columns stop at 720dp, forms at 560dp and the paper at 1100dp.
- A screen has at most one filled button. Other actions are tonal or text, and
  destructive ones use the error color.
- A single choice is a connected group, an option row or a menu, and its whole
  row is the target. Targets are at least 48dp.
- Loading, empty, error and success each have a component. State is in words
  and icons as well as color, and notices announce themselves politely.
- Material 3 is pinned to 1.5.0 for the expressive APIs. There is no icon
  pack, font download, third-party UI toolkit or new network dependency.

## Composition and interaction

- **Shell:** a collapsing title names the place. Short windows, two panes and
  text from 150% use a small bar, whose title shrinks before it truncates.
  Four destinations sit in a bottom bar, or in a rail in wide and short
  windows. Navigation labels stop growing at 130%.
- **Front page:** a dateline with the Today/This week choice, one lead with
  its picture above the headline, then ruled secondary stories in columns.
- **Articles and Saved:** source and time, a serif headline and a thumbnail.
  The unread dot lives in the gutter, so reading never moves the text. The
  layout toggle stays neutral, so it does not compete with the shelf choice.
- **Reader:** the tools take the article's canvas. Keep, Later, Done and Open
  original sit in thumb reach. A short window moves them into the reader's
  menu to keep its height for the text, and keeps the byline as one line
  that starts with Cached copy when it shows one.
- **Drawer:** brand mark, places in their tones, feeds with their own marks
  and Settings pinned below the list.
- **Feeds:** grouped rows by folder, each with one pace menu that explains
  the choices. Subscribe is the floating action and opens a sheet with its
  button above the keyboard.
- **Search:** the field takes focus when opened empty. Filters fold away,
  saved searches stay visible and agent notes are labelled apart.
- **Settings:** three categories of titled groups and one pinned save bar that
  states the draft. A short window scrolls the categories and shows the bar
  only when there is something to save.
- **Connect:** brand and ribbon, one field and one action. The code then
  appears above the form with Copy code and Open approval page.
- **Widget and launch:** the app's paper in day and night. The widget follows
  the device rather than the app's theme.

Focused search, settings and phone-reader screens hide primary navigation.
Search and the phone reader keep the app's options in their menus. Notices
sit above the reader's action bar and the subscribe button, never over them.
Back returns to the previous reading place. Front page and Feeds use one pane
on wide screens, as neither has anything to show in an article pane. A change
of place navigates only after the new place's pane directive has been
composed; navigating in the click computes the panes of the place being left.
The adaptive transition stays bound beneath focused settings and search
screens, with inactive panes empty and hidden from accessibility, so incoming
articles cannot leave navigation history and the visible pane out of step.

Article swipes follow the finger at a third of its speed, and a label at the
uncovered edge names the move or says there is no article that way. The label
fills and the device ticks at the 72dp threshold. Under a large title, pull
to refresh starts once the title is expanded, so it never competes with
expanding it. An empty list says it is fetching only during the account's
first sync; later refreshes leave its empty state in place.

## Decisions in this redesign

These are the redesign's defaults, not owner decisions, and are open to change:

- Later and Done live behind **Saved** with Kept. They were only in the drawer.
- A feed's pace is a menu. Three chips on each row showed under three feeds
  per phone screen.
- Excerpt attribution, every existing action and the article theme semantics
  stay as they were. An article's System theme follows the device, not the
  app's theme.
- Article zoom multiplies the system font size instead of replacing it.

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
article, reader, drawer, feeds, search and results, settings and Connect.
Repeat on a 320dp phone at 200% system text, in landscape, at tablet widths
and in dark, true-black and e-ink modes. Open the keyboard in URL, search,
find and settings fields. Check Back, menus and empty and error states.

The expressive redesign was reviewed on an API 36 arm64 emulator against a
disposable server with fixture feeds: every main screen on a light phone, the
main screens in dark, at 320dp with 200% text, in phone landscape and at
1280x800dp, and the reader, lists or Feeds in e-ink and true black. Device
sign-in was completed against that server. The widget was placed on the
launcher, rendered in day and night, and opened the headline that was tapped.
The cached-copy reader and the failed-refresh notice were seen offline.

Not covered: physical devices, Android before 16, TalkBack speech, push
delivery, 600-840dp windows, AI settings with a configured provider, file
pickers, the swipe gestures and OEM font or launcher differences. Do not
infer them from these checks.

## References

- [Android mobile design](https://developer.android.com/design/ui/mobile)
- [Material 3 Expressive](https://m3.material.io/blog/building-with-m3-expressive)
- [Android Expressive overview](https://blog.google/products-and-platforms/platforms/android/material-3-expressive-android-wearos-launch/)
- [Apple Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines/)
