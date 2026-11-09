# Brand sources

`icon.png` (the app icon art), `wordmark.png` (the icon beside the
lettering) and `glyph.svg` (a single-colour donkey head drawn from the art)
are the masters for every icon and wordmark image the clients ship. After
changing one, regenerate the derived files on macOS and commit them with
the source:

    swift scripts/brand-assets.swift

| Output | Used for |
|---|---|
| `clients/mac/App/Sources/Assets.xcassets/AppIcon.appiconset` | Mac app icon: the art in Apple's 824-point icon shape on the 1024-point grid, with the template's shadow |
| `clients/mac/App/Sources/Assets.xcassets/{BrandIcon,Wordmark}.imageset` | Mac sign-in view |
| `clients/android/app/src/main/res/mipmap-*/ic_launcher_foreground.png` | Android adaptive launcher icon; the art spans 76 of the layer's 108 dp |
| `clients/android/app/src/main/res/drawable-nodpi/brand_{icon,wordmark}.png` | Android login screen |
| `clients/android/app/src/main/res/drawable/ic_launcher_monochrome.xml` | Android themed launcher icon, which the launcher tints; the glyph spans 54 of 108 dp |
| `clients/android/app/src/main/res/drawable/ic_notification.xml` | Android notification icon in the status bar |
| `web/public/{favicon-32,apple-touch-icon,icon-192,icon-512,icon-maskable-512}.png` | Browser tab, home screen, web app manifest and push notification icons |
| `web/public/wordmark.png` | Web header and sign-in page |

The lettering ships as a mask, because the wordmark's dark teal is
unreadable on dark backgrounds. Each client paints it with the brand ink:
`#07566E`, the wordmark's own teal, on light backgrounds, `#8BD0EF` on dark
ones, and black in the e-ink theme. The Mac accent colour is `#106682` in
light mode and `#367F9C` in dark mode. These values live in
`web/src/style.css`,
`clients/android/app/src/main/java/ch/lkmc/earnest/BrandLockup.kt` and the
Mac asset catalog's colour sets.

`glyph.svg` must stay one even-odd path in a `0 0 100 100` viewBox, with
cut-outs rather than a second colour: Android draws both outputs from
alpha alone.

## App colours

The clients take their accents from the icon. The teal above stays the
primary colour for links, focus, primary buttons and the current item.
Six tones mark places and states, each an icon colour tuned to reach at
least 3:1 on every light surface and 4.5:1 on every dark one. They are
for icons, dots, tints and outlines, not for running text. E-ink draws
all of them in black.

| Tone | From the icon | Light | Dark and true black | Marks |
|---|---|---|---|---|
| Orange | right background `#EA5F12` | `#DD5602` | `#FE712E` | Unread, Today, notifications |
| Amber | amber `#FFB428` | `#AE7705` | `#FFB428` | Later |
| Plum | plum `#8E3B68` | `#8E3B68` | `#DA7FAD` | Kept, Activity |
| Purple | ears `#916E9D` | `#916E9D` | `#B38FC0` | Labels, agents, Ask my feeds |
| Blue | face `#408CAD` | `#3E8AAB` | `#5AA5C7` | Edition, folders, feeds, Rules |
| Green | chest `#5DA8A1` | `#428E87` | `#5EA9A2` | Done, synced |

A destination's icon is always drawn in its tone, and the current one sits
on a light tint of it, kept weak enough that the icon still reaches 3:1. Colour never carries meaning alone: each tone
sits beside an icon or a word. The web client's feed letter marks use
seven fixed swatches with white letters, the six light tones plus a
deepened mane red `#B23A1A`, picked by `h = (h * 31 + code point) mod 7`
over the feed title. A thin ribbon of the icon's own
colours, `#FFA01C #EA5F12 #C9481E #8E3B68 #916E9D #408CAD #5DA8A1` in
equal segments, marks the sign-in screens and the top of the web sidebar.

The tones live in `web/src/style.css`,
`clients/android/app/src/main/java/ch/lkmc/earnest/Theme.kt` and the Mac
asset catalog's `Tone*` colour sets. The Mac sidebar uses them only under
the default multicolour accent; a chosen system accent colour wins.
