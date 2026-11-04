package ch.lkmc.earnest.core

import ch.lkmc.earnest.core.vectors.VectorError
import ch.lkmc.earnest.core.vectors.VectorException
import ch.lkmc.earnest.core.vectors.VectorFixture
import ch.lkmc.earnest.core.vectors.VectorRunner
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import kotlinx.serialization.json.booleanOrNull
import java.io.File

/**
 * The drift gate: every shared fixture runs its client-side steps through the
 * real [SyncEngine], with server steps simulated by an in-memory model. Any
 * unregistered fixture file fails the build.
 */
class VectorGateTest {
    private val runner = VectorRunner()

    private val manifest = listOf(
        "01-range-exception.json", "02-queue-age.json", "03-retention-deleted.json", "04-source-ranks.json",
        "05-equal-time.json", "06-backdated-ingest.json", "07-publisher-clock.json", "08-expired-cursor.json",
        "09-restore-generation.json", "10-notification-response-lost.json", "11-shared-ingest.json",
        "12-offline-google-batch.json", "13-bootstrap-range-race.json", "14-ingest-commit-order.json",
        "15-repeated-operation.json", "16-google-unread-done.json", "17-independent-kept.json",
        "18-late-cluster-member.json", "19-notification-before-snapshot.json", "20-google-tradeoff.json",
        "21-bootstrap-newer-range-crash.json", "22-native-unread-done.json", "23-batch-atomicity.json",
        "24-label-tombstones.json", "25-position-live.json", "26-missing-ts.json", "27-client-coalescing.json",
        "28-clock-boundaries.json", "29-resource-tombstones.json", "30-principal-isolation.json",
        "31-queued-range-client.json", "32-coalesced-done-clear.json", "33-restore-release-safety.json",
        "34-integer-identity.json", "35-batch-causal-shelf.json", "36-ack-out-of-order-merge.json",
        "37-ack-stale-generation.json", "38-independent-register-merge.json", "39-range-ack-gone.json",
    )

    @Test
    fun subsetRejectsStringCoercionInBothDirections() {
        val json = kotlinx.serialization.json.Json
        for ((string, scalar) in listOf("\"true\"" to "true", "\"1\"" to "1", "\"null\"" to "null")) {
            for ((expected, actual) in listOf(string to scalar, scalar to string)) {
                val error = runCatching { subset(json.parseToJsonElement(expected), json.parseToJsonElement(actual), "value") }.exceptionOrNull()
                assertTrue("subset accepted $expected as $actual", error is AssertionError)
            }
        }
        subset(json.parseToJsonElement("1"), json.parseToJsonElement("1.0"), "number")
        subset(json.parseToJsonElement("9007199254740993"), json.parseToJsonElement("9007199254740993"), "large integer")
    }

    @Test
    fun everyFixtureRunsItsClientSteps() {
        val directory = File("../../../testdata/vectors")
        val files = directory.listFiles { file -> file.extension == "json" }.orEmpty().sortedBy { it.name }
        assertEquals("register every new vector file in the gate manifest", manifest.size, files.size)
        val notRun = mutableListOf<String>()
        val partial = mutableListOf<String>()
        var clientSteps = 0
        for (file in files) {
            if (file.name !in manifest) fail("unlisted vector file: ${file.name}")
            val runner = VectorRunner()
            val fixture = runner.decode(file.readText())
            assertTrue("${file.name}: version", fixture.version == 1)
            assertTrue("${file.name}: name", fixture.name.isNotEmpty())
            assertTrue("${file.name}: decision", fixture.decision.isNotEmpty())
            assertTrue("${file.name}: final", fixture.final.isNotEmpty())
            val steps = fixture.setup + fixture.operations + fixture.final
            try {
                steps.forEachIndexed { index, step ->
                    if (step.op.startsWith("client_") || step.op.startsWith("bootstrap_")) clientSteps++
                    val result = runner.execute(step)
                    assertEquals("${file.name} step $index (${step.op})", step.error, result.error?.wireName())
                    subset(step.expect, result.value ?: kotlinx.serialization.json.JsonNull, "${file.name} step $index (${step.op})")
                }
            } catch (error: AssertionError) {
                notRun += file.name
                throw error
            }
            if (clientSteps == 0) partial += file.name
        }
        println("vector gate: ${files.size} files, $clientSteps client steps total, " +
            "fully run=${files.size - notRun.size - partial.size}, partly=$partial, not run=$notRun")
    }

    @Test
    fun unknownVectorAndStepAreRejected() {
        val runner = VectorRunner()
        try {
            runner.decode("""{"version":99,"name":"unknown","decision":"x","final":[{}]}""")
            fail("unknown version accepted")
        } catch (_: Exception) {
        }
        try {
            runner.decode("""{"version":1,"unexpected":true}""")
            fail("unknown field accepted")
        } catch (_: Exception) {
        }
        try {
            runner.decode("""{"version":1,"final":[{}]}""")
            fail("missing name or decision accepted")
        } catch (_: Exception) {
        }
        val step = runner.decode("""{"version":1,"name":"x","decision":"y","final":[{"op":"unrecognized","expect":{}}]}""").final.single()
        if (runner.execute(step).error == null) fail("unknown step accepted")
    }

    private fun VectorError.wireName(): String = when (this) {
        VectorError.INVALID -> "invalid"
        VectorError.GENERATION -> "generation"
        VectorError.CURSOR_GONE -> "cursor_gone"
        VectorError.WITHHELD -> "withheld"
        VectorError.ATTEMPTED -> "attempted"
    }
}

/** Expected objects are subsets; arrays are exact and ordered. */
internal fun subset(expected: kotlinx.serialization.json.JsonElement, actual: kotlinx.serialization.json.JsonElement, path: String) {
    when (expected) {
        is kotlinx.serialization.json.JsonObject -> {
            val got = actual as? kotlinx.serialization.json.JsonObject
                ?: error("$path: expected object, got $actual")
            for ((key, value) in expected) {
                val next = got[key] ?: error("$path.$key: missing key (got $actual)")
                subset(value, next, "$path.$key")
            }
        }
        is kotlinx.serialization.json.JsonArray -> {
            val got = actual as? kotlinx.serialization.json.JsonArray
                ?: error("$path: expected array, got $actual")
            if (got.size != expected.size) fail("$path: array mismatch: want $expected, got $got")
            expected.indices.forEach { subset(expected[it], got[it], "$path[$it]") }
        }
        is kotlinx.serialization.json.JsonNull -> if (actual !is kotlinx.serialization.json.JsonNull) {
            fail("$path: want null, got $actual")
        }
        is kotlinx.serialization.json.JsonPrimitive -> {
            val got = actual as? kotlinx.serialization.json.JsonPrimitive
                ?: error("$path: expected primitive, got $actual")
            if (expected.isString || got.isString) {
                if (expected.isString != got.isString || expected.content != got.content) {
                    fail("$path: want $expected, got $got")
                }
                return
            }
            val expectedNumber = expected.content.toBigDecimalOrNull()
            val gotNumber = got.content.toBigDecimalOrNull()
            if (expectedNumber != null && gotNumber != null) {
                if (expectedNumber.compareTo(gotNumber) != 0) fail("$path: want $expected, got $got")
                return
            }
            if (expected.booleanOrNull != got.booleanOrNull) fail("$path: want $expected, got $got")
        }
    }
}
