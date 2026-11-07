package ch.lkmc.earnest

import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Test

class BrandInkTest {
    @Test fun lightSurfacesUseTheWordmarkTeal() {
        assertEquals(Color(0xFF07566E), brandInk(lightColorScheme()))
        assertEquals(Color(0xFF07566E), brandInk(lightColorScheme(background = Color(0xFFF8F6EF))))
    }

    @Test fun darkSurfacesUseTheLightenedTeal() {
        assertEquals(Color(0xFF8BD0EF), brandInk(darkColorScheme()))
        assertEquals(Color(0xFF8BD0EF), brandInk(darkColorScheme(background = Color.Black)))
    }

    @Test fun monochromeThemesKeepTheirPrimary() {
        assertEquals(Color.Black, brandInk(lightColorScheme(primary = Color.Black, background = Color.White)))
    }
}
