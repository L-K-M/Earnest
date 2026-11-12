package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

/**
 * The front page is server-ranked: the client only decodes the contract and
 * keeps the order it was given. These tests pin the wire shape, the period
 * query and the larger image path in the media allowlist.
 */
class FrontPageApiTest {
    private val requests = CopyOnWriteArrayList<okhttp3.Request>()

    private fun api(body: String, code: Int = 200) = EarnestApi(
        "https://earnest.test",
        "secret",
        OkHttpClient.Builder().addInterceptor { chain ->
            requests += chain.request()
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                .code(code).message("test").body(body.toResponseBody()).build()
        }.build(),
    )

    @Test
    fun frontPageSendsThePeriodAndDecodesTheContract() = runBlocking {
        val page = api(frontPageJson()).frontPage(FrontPagePeriod.WEEKLY)
        assertEquals(1, requests.size)
        assertEquals("/v1/front-page", requests[0].url.encodedPath)
        assertEquals("weekly", requests[0].url.queryParameter("period"))
        assertEquals("Bearer secret", requests[0].header("Authorization"))

        assertEquals(FrontPagePeriod.WEEKLY, page.period)
        assertEquals("2026-10-27T10:00:00Z", page.windowStart)
        assertEquals("2026-11-03T10:00:00Z", page.windowEnd)
        assertEquals("2026-11-03T10:00:00Z", page.selectedAt)
        assertEquals("rev-1", page.revision)
        assertEquals(2, page.sections.size)

        val section = page.sections[0]
        assertEquals("Across your feeds", section.name)
        assertEquals(1, section.stories.size)
        val story = section.stories[0]
        assertEquals("cluster:1", story.id)
        assertEquals("Source A", story.sourceTitle)
        assertEquals("2026-11-02T12:00:00Z", story.publishedAt)
        assertEquals(2, story.feedCount)
        assertEquals("Excerpt text", story.excerpt)
        assertEquals("/v1/entries/2/image?v=img", story.imageUrl)
        assertEquals(2, story.coverage.size)
        assertEquals(2L, story.coverage[0].entry.id)
        assertEquals("Source A", story.coverage[0].sourceTitle)
        assertEquals(3L, story.coverage[1].entry.id)
        assertEquals("Source B", story.coverage[1].sourceTitle)

        val entry = story.entry
        assertEquals("Headline", entry.title)
        assertEquals("Blurb 2", entry.blurb)
        assertEquals("/v1/entries/2/thumbnail?v=thumb", entry.thumbnailUrl)
        assertEquals("unseen", entry.state?.seen?.value.toString().trim('"'))
        assertEquals("none", entry.state?.shelf?.value.toString().trim('"'))

        val summary = story.summary
        assertEquals("model-x", summary?.model)
        assertEquals("v1", summary?.promptVersion)
        assertEquals("2026-11-03T09:00:00Z", summary?.generatedAt)
        assertEquals("Excerpt only", summary?.excerptNotice)
        val claim = summary?.claims?.single()
        assertEquals("Claim one.", claim?.text)
        assertEquals(true, claim?.verified)
        val citation = claim?.citations?.single()
        assertEquals(3L, citation?.entryId)
        assertEquals("hash", citation?.contentHash)
        assertEquals(listOf("Passage"), citation?.passages)
        assertEquals(true, citation?.verified)
    }

    @Test
    fun frontPageDefaultsToDaily() = runBlocking {
        api(frontPageJson()).frontPage()
        assertEquals("daily", requests[0].url.queryParameter("period"))
    }

    @Test
    fun mediaAllowlistAcceptsTheEntryImageAndRejectsOtherImages() = runBlocking {
        for (path in listOf(
            "/v1/subscriptions/1/image",
            "/v1/subscriptions/1/image?v=abc",
            "/v1/entries/1/image/extra",
            "/v1/comments/1/image",
        )) {
            val api = api("[]")
            runCatching { api.media(path) }
                .onSuccess { error("media accepted $path") }
                .onFailure { assertTrue("media rejected $path with $it", it is IllegalArgumentException) }
        }
        requests.clear()
        // A 404 is the allowlist's success path: the request reached the server.
        assertNull(api("", code = 404).media("/v1/entries/9/image?v=abc"))
        assertEquals("/v1/entries/9/image", requests[0].url.encodedPath)
        assertEquals("abc", requests[0].url.queryParameter("v"))
        assertNull(requests[0].url.queryParameter("mode"))
    }

    private fun entry(id: Long, title: String, author: String = "Author") = """
        {
          "id": $id,
          "subscription_id": 3,
          "feed_id": 5,
          "seq": $id,
          "first_seen_at": "2026-11-03T00:00:00Z",
          "published_at": "2026-11-02T12:00:00Z",
          "title": "$title",
          "blurb": "Blurb $id",
          "url": "https://news.test/article/$id",
          "author": "$author",
          "rev": 2,
          "deleted": false,
          "state": {
            "principal": 1,
            "entry_id": $id,
            "seen": {"value": "unseen", "rev": 1, "occurred_at": "2026-11-03T00:00:00Z", "actor": 1, "source": "passive"},
            "shelf": {"value": "none", "rev": 1, "occurred_at": "2026-11-03T00:00:00Z", "actor": 1, "source": "passive"},
            "kept": {"value": false, "rev": 1, "occurred_at": "2026-11-03T00:00:00Z", "actor": 1, "source": "passive"},
            "state_rev": 1
          },
          "thumbnail_url": "/v1/entries/$id/thumbnail?v=thumb"
        }
    """.trimIndent()

    private fun frontPageJson(): String = """
        {
          "period": "weekly",
          "window_start": "2026-10-27T10:00:00Z",
          "window_end": "2026-11-03T10:00:00Z",
          "selected_at": "2026-11-03T10:00:00Z",
          "revision": "rev-1",
          "sections": [
            {
              "name": "Across your feeds",
              "stories": [
                {
                  "id": "cluster:1",
                  "entry": ${entry(2, "Headline")},
                  "source_title": "Source A",
                  "published_at": "2026-11-02T12:00:00Z",
                  "feed_count": 2,
                  "excerpt": "Excerpt text",
                  "image_url": "/v1/entries/2/image?v=img",
                  "coverage": [
                    {"entry": ${entry(2, "Headline")}, "source_title": "Source A"},
                    {"entry": ${entry(3, "Headline three")}, "source_title": "Source B"}
                  ],
                  "summary": {
                    "claims": [
                      {
                        "text": "Claim one.",
                        "citations": [
                          {"entry_id": 3, "content_hash": "hash", "passages": ["Passage"], "verified": true}
                        ],
                        "verified": true
                      }
                    ],
                    "model": "model-x",
                    "prompt_version": "v1",
                    "generated_at": "2026-11-03T09:00:00Z",
                    "excerpt_notice": "Excerpt only"
                  }
                }
              ]
            },
            {
              "name": "More from your feeds",
              "stories": [
                {
                  "id": "entry:4",
                  "entry": ${entry(4, "Solo story")},
                  "source_title": "Source C",
                  "published_at": "2026-11-01T12:00:00Z",
                  "feed_count": 1,
                  "excerpt": "Solo excerpt",
                  "coverage": [
                    {"entry": ${entry(4, "Solo story")}, "source_title": "Source C"}
                  ]
                }
              ]
            }
          ]
        }
    """.trimIndent()
}
