package com.msitools.anyconnectmobile.routeprobe.report

import java.io.File
import java.io.IOException
import java.nio.file.Files
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ProbeReportTest {
    @Test
    fun serializesStableCompleteSchemaWithoutSensitiveFields() {
        val report = sampleReport()

        val json = report.toJson()
        assertEquals(
            setOf(
                "schemaVersion",
                "generatedAtEpochMs",
                "device",
                "ipdb",
                "scenarios",
                "gateDecision",
            ),
            json.keys().asSequence().toSet(),
        )
        assertEquals(1, json.getInt("schemaVersion"))
        assertEquals("BLOCKED", json.getString("gateDecision"))
        assertEquals(
            setOf("model", "sdkInt", "supportedAbis", "pageSizeBytes", "fingerprint"),
            json.getJSONObject("device").keys().asSequence().toSet(),
        )
        assertEquals(
            setOf(
                "sha256",
                "rawRecordCount",
                "ipv4RawCount",
                "ipv6RawCount",
                "ipv4CollapsedCount",
                "ipv6CollapsedCount",
                "ipv4ComplementCount",
                "ipv6ComplementCount",
            ),
            json.getJSONObject("ipdb").keys().asSequence().toSet(),
        )

        val scenario = json.getJSONArray("scenarios").getJSONObject(0)
        assertEquals(
            setOf(
                "scenario",
                "attempt",
                "strategy",
                "includeCount",
                "excludeCount",
                "builderEntryCount",
                "planSha256",
                "establishMs",
                "rssBeforeKb",
                "rssAfterKb",
                "rssHighWaterKb",
                "fdCountBefore",
                "fdCountAfter",
                "expectedCaptured",
                "capturedChecksPassed",
                "expectedBypass",
                "bypassChecksPassed",
                "directResponseStatus",
                "establishStatus",
                "errorClass",
                "errorMessage",
            ),
            scenario.keys().asSequence().toSet(),
        )
        assertTrue(scenario.isNull("errorClass"))
        assertTrue(scenario.isNull("errorMessage"))

        val encoded = json.toString()
        listOf("username", "password", "nodeUrl", "interfaceAddress", "payload").forEach {
            assertFalse(encoded.contains(it, ignoreCase = true))
        }
    }

    @Test
    fun writesIdenticalTimestampAndLatestJson() {
        val reportsDirectory = Files.createTempDirectory("probe-report-write").toFile()
        val latestWriter = LatestReportWriter { target, bytes ->
            target.parentFile?.mkdirs()
            target.writeBytes(bytes)
        }
        val store = ProbeReportStore(reportsDirectory, latestWriter)
        val report = sampleReport()

        val timestamped = store.write(report)

        val latest = File(reportsDirectory, "latest.json")
        assertTrue(timestamped.name.matches(Regex("route-probe-123456789\\.json")))
        assertEquals(report.toJson().toString(), timestamped.readText())
        assertEquals(timestamped.readText(), latest.readText())
        assertFalse(File(reportsDirectory, timestamped.name + ".tmp").exists())
    }

    @Test
    fun findsOnlyDeterministicUnmatchedBeforeCheckpoints() {
        val reportsDirectory = Files.createTempDirectory("probe-checkpoints").toFile()
        val store = ProbeReportStore(
            reportsDirectory,
            LatestReportWriter { target, bytes -> target.writeBytes(bytes) },
        )
        val matched = sampleCheckpoint(runId = "run-1", attempt = 1)
        val unmatched = sampleCheckpoint(runId = "run-1", attempt = 2)

        store.appendCheckpoint(matched)
        store.appendCheckpoint(matched.copy(phase = CheckpointPhase.AFTER_ESTABLISH))
        store.appendCheckpoint(unmatched)

        assertEquals(listOf(unmatched), store.unmatchedBeforeEstablish())
    }

    @Test
    fun checkpointReaderHardFailsOnCorruptOrMismatchedRecords() {
        val corruptDirectory = Files.createTempDirectory("probe-checkpoint-corrupt").toFile()
        File(corruptDirectory, "checkpoints.jsonl").writeText("{not-json}\n")
        val corruptStore = ProbeReportStore(
            corruptDirectory,
            LatestReportWriter { target, bytes -> target.writeBytes(bytes) },
        )
        assertThrows(IllegalStateException::class.java) {
            corruptStore.unmatchedBeforeEstablish()
        }

        val mismatchDirectory = Files.createTempDirectory("probe-checkpoint-mismatch").toFile()
        val mismatchStore = ProbeReportStore(
            mismatchDirectory,
            LatestReportWriter { target, bytes -> target.writeBytes(bytes) },
        )
        val before = sampleCheckpoint(runId = "run-2", attempt = 1)
        mismatchStore.appendCheckpoint(before)
        mismatchStore.appendCheckpoint(
            before.copy(
                phase = CheckpointPhase.AFTER_ESTABLISH,
                planSha256 = "different-plan",
            ),
        )
        assertThrows(IllegalStateException::class.java) {
            mismatchStore.unmatchedBeforeEstablish()
        }
    }

    @Test
    fun checkpointReaderRejectsReusingAnAlreadyCompletedIdentity() {
        val reportsDirectory = Files.createTempDirectory("probe-checkpoint-reuse").toFile()
        val store = ProbeReportStore(
            reportsDirectory,
            LatestReportWriter { target, bytes -> target.writeBytes(bytes) },
        )
        val before = sampleCheckpoint(runId = "run-complete", attempt = 1)
        store.appendCheckpoint(before)
        store.appendCheckpoint(before.copy(phase = CheckpointPhase.AFTER_ESTABLISH))
        store.appendCheckpoint(before)

        assertThrows(IllegalStateException::class.java) {
            store.unmatchedBeforeEstablish()
        }
    }

    @Test
    fun latestWriterFailureLeavesPreviousLatestUntouched() {
        val reportsDirectory = Files.createTempDirectory("probe-latest-failure").toFile()
        val latest = File(reportsDirectory, "latest.json")
        latest.writeText("previous-latest")
        val store = ProbeReportStore(
            reportsDirectory,
            LatestReportWriter { _, _ -> throw IOException("latest write failed") },
        )

        assertThrows(IOException::class.java) {
            store.write(sampleReport())
        }

        assertEquals("previous-latest", latest.readText())
        assertTrue(File(reportsDirectory, "route-probe-123456789.json").isFile)
    }

    private fun sampleReport(): ProbeReport = ProbeReport(
        generatedAtEpochMs = 123_456_789L,
        device = ProbeDeviceMetadata(
            model = "test-model",
            sdkInt = 35,
            supportedAbis = listOf("arm64-v8a"),
            pageSizeBytes = 16_384L,
            fingerprint = "test-fingerprint",
        ),
        ipdb = ProbeIpDbMetadata(
            sha256 = "ipdb-sha256",
            rawRecordCount = 10_826,
            ipv4RawCount = 9_000,
            ipv6RawCount = 1_826,
            ipv4CollapsedCount = 4_000,
            ipv6CollapsedCount = 900,
            ipv4ComplementCount = 4_001,
            ipv6ComplementCount = 901,
        ),
        scenarios = listOf(
            ProbeScenarioResult(
                scenario = "DOMESTIC_DIRECT_IPV4",
                attempt = 1,
                strategy = "INCLUDES_ONLY",
                includeCount = 1_025,
                excludeCount = 0,
                builderEntryCount = 1_025,
                planSha256 = "plan-sha256",
                establishMs = 25,
                rssBeforeKb = 10_000,
                rssAfterKb = 10_100,
                rssHighWaterKb = 10_200,
                fdCountBefore = 20,
                fdCountAfter = 21,
                expectedCaptured = listOf("8.8.8.8"),
                capturedChecksPassed = true,
                expectedBypass = listOf("114.114.114.114"),
                bypassChecksPassed = false,
                directResponseStatus = "INCONCLUSIVE",
                establishStatus = "SUCCESS",
                errorClass = null,
                errorMessage = null,
            ),
        ),
        gateDecision = GateDecision.BLOCKED,
    )

    private fun sampleCheckpoint(runId: String, attempt: Int): ProbeCheckpoint = ProbeCheckpoint(
        runId = runId,
        attempt = attempt,
        scenario = "DOMESTIC_DIRECT_IPV4",
        strategy = "INCLUDES_ONLY",
        includeCount = 1_025,
        excludeCount = 0,
        builderEntryCount = 1_025,
        planSha256 = "plan-sha256",
        phase = CheckpointPhase.BEFORE_ESTABLISH,
        elapsedRealtime = 42L,
    )
}
