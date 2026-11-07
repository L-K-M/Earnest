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
