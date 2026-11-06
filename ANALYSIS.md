# Earnest analysis

Unimplemented findings from the longcat-1 review, ordered by severity. All
entries here are nice-to-have; every blocking and important finding from that
review has an open pull request against main.

## usability

### Feed health timestamps are absolute and timezone-dependent

usability — nice-to-have

`web/src/FeedSettings.tsx:245` renders feed health times with
`toLocaleString()` in the browser's local timezone. Users in different zones
see different absolute times for the same feed, and no relative age is shown,
so "Tried …" is hard to interpret.

Suggested fix: render a relative time ("2 h ago") plus the absolute time in
the user's zone, or send a preformatted relative string from the server.

### The notifications release button is always visible

usability — nice-to-have

`web/src/agents/Surfaces.tsx:104` shows "Release current notifications" even
when no restore has happened and nothing is withheld. A user cannot tell
whether pressing it does anything.

Suggested fix: show it only when a withheld notification exists, or label it
with the current generation state.

### Onboarding blocks Continue while measuring feed volume

usability — nice-to-have

`web/src/Onboarding.tsx:11` polls feed health for up to 20 seconds and
disables Continue while checking. A user with many feeds waits with no sense
of progress.

Suggested fix: show a live count of feeds still being measured and let the
user continue immediately; update the report when the poll finishes.

### Saving settings gives no success feedback

usability — nice-to-have

`web/src/Settings.tsx:21` clears errors on save but never confirms success.
Users reasonably wonder whether the save worked.

Suggested fix: show a brief "Saved" status, as the Feeds screen already does.

### The inline feed title input goes stale

usability — nice-to-have

`web/src/FeedSettings.tsx:250` initialises the title input from `sub.title`
once and never updates it when the feed's title changes elsewhere (another
tab, a metadata refresh, an OPML import). The form can save a stale title back
over the fresh one.

Suggested fix: sync the draft when the subscription's title changes and the
input is not focused.

### Typography changes do not refresh an open article

usability — nice-to-have

`web/src/ArticleReader.tsx:26` fetches the content URL once per mount. A
typography change requires closing and reopening the article before the
iframe reflects it.

Suggested fix: re-render the iframe when the typography values change by
keying it on them.

## performance

### The passive-seen observer is rebuilt on every scroll frame

performance — nice-to-have

`web/src/River.tsx:50` depends on the joined visible-ID string, so it tears
down and recreates an IntersectionObserver and a 600 ms timer on every scroll
frame that changes the visible set. On a long list this is measurable
main-thread churn and can drop frames.

Suggested fix: observe rows in an effect that depends on the rendered page
(not the visible set), and keep the timer outside the observer's lifecycle.

### The river re-checks freshness on every action

performance — nice-to-have

`web/src/River.tsx:84` rebuilds the `actions` object and the command list on
every render, and the river's keydown effect depends on them. The listener is
re-bound on every state change.

Suggested fix: memoise the actions object and read it from a ref in the
keydown effect, matching the palette fix.

## visual issues

### Edition group headings can clip the first article

visual issues — nice-to-have

`web/src/style.css:29` reserves a fixed 1.5 rem for the Edition group heading
by subtracting it from the row height. A long feed title wraps to two lines on
a narrow measure and clips the first article.

Suggested fix: reserve the heading height with a measured value, or put the
heading outside the virtual row and account for it in the virtualizer's size
estimate.

### The mobile sidebar can take a third of the screen

visual issues — nice-to-have

`web/src/style.css:54` turns the sidebar into a 30 dvh scroll box with every
feed rendered as a button at max-width 700 px. With many feeds the nav takes a
third of the small screen before the reader is visible.

Suggested fix: collapse the feed list behind a details/summary by default on
small screens and let the reader take the full height.

### Switching views scrolls the workspace to the heading

visual issues — nice-to-have

`web/src/River.tsx` focuses the `<h2>` on every view change, which scrolls
the workspace to the top even though the list itself is remounted.

Suggested fix: focus without scrolling (`preventScroll: true`) and only on
keyboard-driven view changes.

### The article iframe has a hard 75 dvh height

visual issues — nice-to-have

`web/src/style.css:34` fixes the reader iframe at 75 dvh. On a tall monitor
that leaves a large empty area under the article; on a short one it dominates.

Suggested fix: let the article body fill the available height with an
internal scroll, or offer a compact/comfortable density choice.

## aesthetics and theming

### The palette has no accent colour

aesthetics and theming — nice-to-have

`web/src/style.css` defines a single warm paper and one ink colour per theme.
Selection, links and focus use the browser default outline, so the app feels
flatter than the reading experience it provides.

Suggested fix: add one restrained accent per theme and use it for
`aria-current`, focus rings and the selected row.

### The dyslexia-friendly choice has no preview

aesthetics and theming — nice-to-have

`web/src/Settings.tsx:37` changes the face and spacing for the dyslexia
option but shows no preview. A user cannot tell what they are choosing before
saving.

Suggested fix: show a short live preview paragraph in the selected theme,
measure and font.

## missing features

### Unread counts cover feeds but not folders or labels

missing features — nice-to-have

`web/src/main.tsx:181` shows counts only for feeds in unread-counts mode.
Folders and labels have no count, so a user cannot see which folder is backing
up without opening it.

Suggested fix: extend `readUnreadCounts` to return per-folder and per-label
totals from the same bounded scan.

### Manage feeds has no filter

missing features — nice-to-have

`web/src/FeedSettings.tsx:234` renders every subscription in a long scroll
with no filter once there are more than a few dozen.

Suggested fix: add a client-side filter box over title and URL.

### The reader has no font-size control

missing features — nice-to-have

`web/src/Settings.tsx` controls measure and font but not font size. Users who
need larger text have no in-app control.

Suggested fix: add a font-size stepper to the typography fieldset and the
content URL.

### The reader has no focus mode, text-to-speech or share action

missing features — nice-to-have

`web/src/ArticleReader.tsx` has a small toolbar row but no focus mode, no
text-to-speech and no share action. These are common reader expectations.

Suggested fix: pick one (focus mode that hides the sidebar is the cheapest)
and ship it behind a setting.

## bugs

### A literal `<!DOCTYPE` in article text rejects a feed

bugs — nice-to-have

`internal/feed/charset.go:91` rejects any document containing the literal
bytes `<!DOCTYPE` or `<!ENTITY`, even inside CDATA, comments or escaped
article text. A legitimate feed whose article body contains that string is
refused as a DTD attack.

Suggested fix: only reject when the declaration appears in the prolog before
the root element, or strip CDATA and comments before the check.

### Passive "seen" can mark articles the user only scrolled past

bugs — nice-to-have

`web/src/River.tsx:53` triggers passive seen at 60% visibility for 600 ms. A
reader who scrolls a long list quickly, or who leaves the window open while
doing something else, gets entries marked seen that they never read; the
saved position then moves past them.

Suggested fix: require the entry to stay intersecting across two consecutive
timer ticks, or tie passive-seen to an explicit dwell time and never advance
the saved position past unread entries.

### Single-key actions act on the first article when nothing is selected

bugs — nice-to-have

`web/src/River.tsx:84` makes `j`/`k`/`e`/`s`/`f`/`m` act on `entries[0]` when
no entry is selected. A user who presses `e` while the list is still loading,
or after opening a view with nothing focused, silently marks the first
article Done.

Suggested fix: require a selection for single-key actions, or make the first
action only move the selection.

## novel ideas

### The session end could offer a reset and a real reading count

novel ideas — nice-to-have

`web/src/River.tsx:127` already has a small session summary. It could carry a
"Start again from the top" action that resets the view position, and a count
of articles actually opened this session (not just marked seen).

Suggested fix: add a compact session summary with a reset action.

### Agent briefs deserve their own surface

novel ideas — nice-to-have

`web/src/agents/Surfaces.tsx` renders agent briefs as details/summary rows in
the Today river. Grouping today's notes by principal, with the entries they
refer to, would make the agent relationship legible.

Suggested fix: promote briefs to their own panel, reusing `TodayAgents` data.

### The command palette could expose reading-state commands

novel ideas — nice-to-have

`web/src/palette/Palette.tsx` is fast and the fuzzy matcher is good. Adding
"Mark everything above as read", "Jump to oldest unread" and "Open a random
unread article" would make it more useful without new chrome.

Suggested fix: extend the command list and the underlying reader actions.

### A focus mode would fit the calm aesthetic

novel ideas — nice-to-have

`web/src/reader.ts` builds the content URL and the shell already supports
hiding the sidebar. A focus mode that dims the backdrop and centers the
article would be cheap.

Suggested fix: a toggle in the reader header that adds a class to the layout
and hides the nav.
