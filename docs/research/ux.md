# UX

Sources were checked 2026-11-01 by the research helpers. No user research has been done: this is pattern review plus judgement. Test it on yourself first.

## Principles

1. **Position by default, counts by choice.** Show how far you've read. Counts are a first-class option offered at onboarding, because [many readers want them](competitors.md#what-users-say). Never on a dock icon or app icon by default.
2. **Every feed declares its urgency.** Now, Today or This week.
3. **Sessions end.** A written end state, not an infinite scroll.
4. **The original is one key away from any generated text.** AI annotates and never replaces.
5. **Nothing happens behind your back.** Every automated change is visible and attributed, and says plainly whether it can be reverted.
6. **Notifications are a budget.** Each explains itself; digest is the default.
7. **One palette, one keymap** across Mac and web; gestures of equal power on Android.
8. **Typography is the product.** It comes before any AI feature.
9. **Honest, plain copy.** Facts in errors. Wit only in empty and end states.
10. **Native means native.** Stock window chrome on Mac, panes and predictive back on Android.

## Information model

```
 Feeds ─► River (Today)  ─► Done
    │        │     └──────► Later ──► Done or Keep
    │        └─ Now items may notify
    └─► Edition (This week, and anything you batch)
```

**Cadence.** The two research reports proposed Digest and Fresh, and Vital, Steady and Deep. Name them by when you want to see them:

| Cadence | Behaviour | Typical feed |
| --- | --- | --- |
| **Now** | In the river; may notify under a rule | A security advisory list, a friend's rare blog |
| **Today** | In the river | Most blogs |
| **This week** | Only in the edition | Hacker News, release firehoses |

During import, detect firehoses from recent volume and propose This week. Current's per-source half-life is the same idea with a decay model; start with three named buckets and add decay if the river still overflows.

**Decay.** Today items older than a chosen age leave the river without a "you missed this" state, measured from when Earnest first saw them. They remain searchable. This is the default, not the universal answer: the [read-state backlash against river apps](competitors.md#what-users-say) suggests that a mandatory model is the mistake.

**Position versus state.** Reeder stores a timeline position, which is simple to sync but can't answer "have I seen this?" once you move between folders. Earnest keeps them separate, per [api.md](api.md): a position says where to resume and claims nothing, and per-entry state records what you saw and decided. Scrolling past marks only what was rendered. Done, Later and Keep are explicit.

**Panes.** Three panes on Mac and wide web. List-detail on tablets and unfolded foldables. Single pane on phones.

## The end of a session

```
That is everything since Tuesday.
9 kept. 2 for later. 41 moved to Done by your "Noise" rule (Undo).
```

No confetti, streaks or photography. An optional time box ("Twenty minutes") ends at time or at the end of the river, whichever comes first, and is dismissible without guilt.

## Flows

### First run

No account, AI or notification prompts first. Import comes first.

```
┌ Earnest ─────────────────┬──────────────────────────────┐
│ Today    (empty)         │ Found 128 feeds. 3 duplicates│
│ Feeds    (empty)         │                              │
│ Later                    │ 7 post more than 20 a day.   │
│                          │ Put them in This week?       │
│                          │         [Yes] [Review]       │
│                          │                              │
│                          │ How do you like to read?     │
│                          │ ( ) A river that ends        │
│                          │ ( ) Unread counts            │
└──────────────────────────┴──────────────────────────────┘
```

Do not fill the river with 5000 back items: import starts your position at now, and shows only the last day or so. Starter packs are short signed letters explaining why each feed earns its place, not directories. Empty states are written copy, never fake articles.

### Daily catch-up on Mac

```
┌──────────┬─────────────────────┬───────────────────────────┐
│ Today    │ Since 09:12         │ Title of the article      │
│ Edition  │ ▸ Rust advisory  ●  │ Source · 2 h ago          │
│ Later  3 │ ▸ A long essay      │ ─────────────────────     │
│ Keep     │ ▸ Release notes     │ Summary · model · ¶2 ¶4   │
│ Feeds    │   also in 3 others  │ Tab hides the summary     │
│ Activity │                     │                           │
│          │ [That is everything]│ Original text, 65 chars…  │
└──────────┴─────────────────────┴───────────────────────────┘
```

`j k` move, `e` done, `s` later, `f` keep, `⌘K` palette, `?` cheat sheet.

### Daily catch-up on Android

```
┌───────────────────────────┐
│ Today                 ⌕ ⋮ │
│ Since 09:12 · 20 min left │
├───────────────────────────┤
│ Rust advisory           ● │
│ A long essay              │
│ Release notes · +3 others │
│ ← later          done →   │
├───────────────────────────┤
│ Today  Edition  Later  ⋯  │
└───────────────────────────┘
```

Short swipe to later, long swipe to done, label revealed during the gesture, undo in a snackbar. `ListDetailPaneScaffold` carries it from phone to foldable. Never intercept predictive back at the launcher activity.

### An agent-sent alert

```
Lock screen:  Earnest
              Morning Agent: 1 alert

Opening it:
┌───────────────────────────┐
│ ← Advisory title          │
├───────────────────────────┤
│ WHY YOU GOT THIS          │
│ Sent by: Morning Agent    │
│ Budget: 1 of 3 today      │
│ [Mute agent]              │
├───────────────────────────┤
│ The agent says:           │
│ "Affects a crate you      │
│  follow."                 │
├───────────────────────────┤
│ Original article          │
└───────────────────────────┘
```

The lock screen shows only what the server generated, because push payloads pass through Apple, Google or ntfy, and an agent's text must not get a free ride out. Showing more is an opt-in. Inside, the "why" block is also server-generated. Anything the agent wrote appears separately, as plain text, in its own words. No links in the notification; the only tap target is the item. For a rule you wrote, the block also names the rule and the matched text.

## AI and agents in the UI

- **Off until asked.** "No AI" removes the surfaces rather than greying them out.
- **A quiet label**, in plain text: "Summary · model · 2 min ago". No sparkles or gradients.
- **Citations as passages.** Selecting one scrolls to and highlights the source passage. The server has checked that the passage exists, which isn't the same as supporting the claim. A claim whose citation failed stays visible and is flagged.
- **Admit limits.** "The feed gave an excerpt only. This summary covers the excerpt."
- **No rewriting.** Titles are the publisher's.
- **Activity is a first-class surface.** "While you were out", grouped by agent and rule. Each row says honestly what can happen next: Revert, Cancel, or nothing, since a delivered notification can't be recalled.
- **Annotations look different from content.** An agent's "skip: duplicate of X" never reads like the article.

## Per platform

**Mac.** Stock toolbar and sidebar per current macOS; semantic materials only, tested at both ends of the Liquid Glass slider. Every command in the menu bar with its key equivalent. Share extensions for Subscribe and Save. Multi-window through standard scene support.

**Android.** Material 3 Expressive, `ListDetailPaneScaffold`, `NavigationSuiteScaffold`. Channels per user-visible category. New content is `IMPORTANCE_LOW` per [Google's guidance](https://developer.android.com/develop/ui/views/notifications/channels); only Now alerts rise above that. A Glance widget with headlines and no count. Reuse Kararead's reader code.

**Web.** Single-key shortcuts when focus is outside text inputs, as in Feedbin and NewsBlur, with an off switch to satisfy [WCAG 2.1.4](https://www.w3.org/WAI/WCAG21/Understanding/character-key-shortcuts.html). `Ctrl+K` palette with `preventDefault`. Panes rather than a mobile stack on desktop. PWA for installability, Web Push and offline reading.

## Reading view

Measure 62 to 72 characters, adjustable, overridable per feed. A serif and a sans default plus a dyslexia-friendly option. Images within the measure, never cropped. Code in monospace with horizontal scroll and no smart quotes. Light, dark, true black and e-ink themes, following the system with an override. Per-article `dir` from content language, mirroring layout for RTL. VoiceOver and TalkBack order: title, source, date, summary, body. Dynamic type to 200%. Nothing conveyed by colour alone.

## Personality

Earnest is sincere and a little dry, in the manner of a good reference librarian. Wilde is a reference for the occasional line in an empty state, not a theme: no mascot, no pun in every string.

Good: "Nothing new. A rare and beautiful thing."
Bad: "You're all caught up! 🎉"

## Anti-patterns

Badge counts. Streaks. Confetti. Sparkles. AI summaries on by default. Summaries that hide the source. Notifications per article. Silent agent writes. "Mark all read" as a primary toolbar action. Fake articles in empty states. Importing thousands of back items into the river. Custom window chrome. Prompts for AI or notifications at launch. Cards on desktop.

## Risks to test

- Decay may make completionists anxious, and there's evidence some readers revolt. Offer the unread model at onboarding, not in a buried setting. You can test this for free: [hide counts selectively in FreshRSS or Miniflux with CSS](https://danq.me/2026/02/06/phantom-obligation-vs-freshrss/) for two weeks.
- Three cadences may be too few, or too much homework. Firehose detection must do the initial work.
- The activity log may feel like an audit trail. Keep it collapsed unless something happened.
- Time boxes may feel paternal. Keep them opt-in.
