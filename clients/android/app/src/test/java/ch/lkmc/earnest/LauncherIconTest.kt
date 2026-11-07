package ch.lkmc.earnest

import android.content.Context
import android.graphics.drawable.AdaptiveIconDrawable
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class LauncherIconTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    // Without a monochrome layer, themed launchers show the full-colour icon among tinted ones.
    @Test fun bothLauncherIconsOfferAMonochromeLayer() {
        for (icon in listOf(R.mipmap.ic_launcher, R.mipmap.ic_launcher_round)) {
            val drawable = context.getDrawable(icon) as AdaptiveIconDrawable
            assertNotNull(drawable.monochrome)
        }
    }
}
