package ch.lkmc.earnest

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.xmlpull.v1.XmlPullParser

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class BackupRulesTest {
    @Test fun accountDataIsExcludedFromDeviceTransfer() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val domains = mutableSetOf<String>()
        context.resources.getXml(R.xml.data_extraction_rules).use { parser ->
            var section = ""
            while (parser.eventType != XmlPullParser.END_DOCUMENT) {
                if (parser.eventType == XmlPullParser.START_TAG) {
                    if (parser.name == "device-transfer") section = parser.name
                    if (section == "device-transfer" && parser.name == "exclude" &&
                        parser.getAttributeValue(null, "path").orEmpty() in setOf("", ".")) {
                        domains += parser.getAttributeValue(null, "domain")
                    }
                }
                if (parser.eventType == XmlPullParser.END_TAG && parser.name == "device-transfer") section = ""
                parser.next()
            }
        }
        for (domain in listOf("database", "sharedpref")) {
            assertTrue("device transfer still includes $domain", domain in domains)
        }
    }
}
