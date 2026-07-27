package com.msitools.anyconnectmobile.routeprobe.report

import android.content.Context
import android.util.AtomicFile
import java.io.File
import java.io.FileOutputStream
import java.io.InputStreamReader
import java.nio.charset.CodingErrorAction
import java.util.LinkedHashMap
import org.json.JSONObject

enum class CheckpointPhase { BEFORE_ESTABLISH, AFTER_ESTABLISH }

data class ProbeCheckpoint(
    val runId: String,
    val attempt: Int,
    val scenario: String,
    val strategy: String,
    val includeCount: Int,
    val excludeCount: Int,
    val builderEntryCount: Int,
    val planSha256: String,
    val phase: CheckpointPhase,
    val elapsedRealtime: Long,
) {
    init {
        require(runId.isNotBlank()) { "runId must not be blank" }
        require(attempt >= 1) { "attempt must be one-based" }
        require(scenario.isNotBlank()) { "scenario must not be blank" }
        require(strategy.isNotBlank()) { "strategy must not be blank" }
        require(includeCount >= 0 && excludeCount >= 0) { "route counts must not be negative" }
        require(builderEntryCount == includeCount + excludeCount) {
            "builderEntryCount must equal includeCount plus excludeCount"
        }
        require(planSha256.isNotBlank()) { "planSha256 must not be blank" }
        require(elapsedRealtime >= 0) { "elapsedRealtime must not be negative" }
    }
}

internal fun interface LatestReportWriter {
    fun write(target: File, bytes: ByteArray)
}

class ProbeReportStore internal constructor(
    private val reportsDirectory: File,
    private val latestReportWriter: LatestReportWriter,
) {
    constructor(context: Context) : this(
        reportsDirectory = File(context.filesDir, REPORTS_DIRECTORY),
        latestReportWriter = AtomicLatestReportWriter,
    )

    @Synchronized
    fun appendCheckpoint(checkpoint: ProbeCheckpoint) {
        ensureReportsDirectory()
        val bytes = (checkpoint.toJson().toString() + "\n").toByteArray(Charsets.UTF_8)
        FileOutputStream(checkpointsFile(), true).use { output ->
            output.write(bytes)
            output.fd.sync()
        }
    }

    @Synchronized
    fun unmatchedBeforeEstablish(): List<ProbeCheckpoint> {
        val file = checkpointsFile()
        if (!file.exists()) return emptyList()
        check(file.isFile) { "Checkpoint path is not a file: ${file.absolutePath}" }

        val pending = LinkedHashMap<CheckpointIdentity, ProbeCheckpoint>()
        val completed = mutableSetOf<CheckpointIdentity>()
        val decoder = Charsets.UTF_8.newDecoder()
            .onMalformedInput(CodingErrorAction.REPORT)
            .onUnmappableCharacter(CodingErrorAction.REPORT)
        try {
            file.inputStream().use { stream ->
                InputStreamReader(stream, decoder).buffered().use { reader ->
                    var lineNumber = 0
                    while (true) {
                        val line = reader.readLine() ?: break
                        lineNumber++
                        check(line.isNotEmpty()) {
                            "Checkpoint line $lineNumber is empty"
                        }
                        val checkpoint = try {
                            checkpointFromJson(JSONObject(line))
                        } catch (error: Throwable) {
                            throw IllegalStateException(
                                "Invalid checkpoint at line $lineNumber",
                                error,
                            )
                        }
                        val identity = CheckpointIdentity(checkpoint.runId, checkpoint.attempt)
                        when (checkpoint.phase) {
                            CheckpointPhase.BEFORE_ESTABLISH -> {
                                check(identity !in completed) {
                                    "BEFORE_ESTABLISH reuses completed ${identity.describe()}"
                                }
                                check(pending.putIfAbsent(identity, checkpoint) == null) {
                                    "Duplicate BEFORE_ESTABLISH for ${identity.describe()}"
                                }
                            }

                            CheckpointPhase.AFTER_ESTABLISH -> {
                                val before = pending[identity]
                                    ?: error(
                                        "AFTER_ESTABLISH has no matching BEFORE_ESTABLISH for " +
                                            identity.describe(),
                                    )
                                check(before.hasMatchingMetadata(checkpoint)) {
                                    "Checkpoint metadata mismatch for ${identity.describe()}"
                                }
                                check(checkpoint.elapsedRealtime >= before.elapsedRealtime) {
                                    "Checkpoint elapsedRealtime moved backwards for ${identity.describe()}"
                                }
                                pending.remove(identity)
                                completed += identity
                            }
                        }
                    }
                }
            }
        } catch (error: IllegalStateException) {
            throw error
        } catch (error: Throwable) {
            throw IllegalStateException("Unable to read checkpoint journal", error)
        }
        return pending.values.toList()
    }

    @Synchronized
    fun write(report: ProbeReport): File {
        ensureReportsDirectory()
        val bytes = report.toJson().toString().toByteArray(Charsets.UTF_8)
        val timestamped = File(
            reportsDirectory,
            "$TIMESTAMP_REPORT_PREFIX${report.generatedAtEpochMs}$JSON_SUFFIX",
        )
        check(!timestamped.exists()) {
            "Timestamped report already exists: ${timestamped.absolutePath}"
        }
        val temporary = File.createTempFile(timestamped.name + ".", TEMP_SUFFIX, reportsDirectory)
        FileOutputStream(temporary).use { output ->
            output.write(bytes)
            output.fd.sync()
        }
        check(temporary.renameTo(timestamped)) {
            "Unable to publish timestamped report: ${timestamped.absolutePath}"
        }

        latestReportWriter.write(File(reportsDirectory, LATEST_REPORT), bytes)
        return timestamped
    }

    private fun ensureReportsDirectory() {
        if (reportsDirectory.exists()) {
            check(reportsDirectory.isDirectory) {
                "Reports path is not a directory: ${reportsDirectory.absolutePath}"
            }
            return
        }
        check(reportsDirectory.mkdirs()) {
            "Unable to create reports directory: ${reportsDirectory.absolutePath}"
        }
    }

    private fun checkpointsFile(): File = File(reportsDirectory, CHECKPOINTS_FILE)

    private data class CheckpointIdentity(val runId: String, val attempt: Int) {
        fun describe(): String = "runId=$runId attempt=$attempt"
    }

    private companion object {
        const val REPORTS_DIRECTORY = "reports"
        const val CHECKPOINTS_FILE = "checkpoints.jsonl"
        const val LATEST_REPORT = "latest.json"
        const val TIMESTAMP_REPORT_PREFIX = "route-probe-"
        const val JSON_SUFFIX = ".json"
        const val TEMP_SUFFIX = ".tmp"
    }
}

private val CHECKPOINT_KEYS = setOf(
    "runId",
    "attempt",
    "scenario",
    "strategy",
    "includeCount",
    "excludeCount",
    "builderEntryCount",
    "planSha256",
    "phase",
    "elapsedRealtime",
)

private object AtomicLatestReportWriter : LatestReportWriter {
    override fun write(target: File, bytes: ByteArray) {
        val atomicFile = AtomicFile(target)
        var output: FileOutputStream? = null
        try {
            output = atomicFile.startWrite()
            output.write(bytes)
            output.fd.sync()
            atomicFile.finishWrite(output)
        } catch (error: Throwable) {
            output?.let { stream ->
                try {
                    atomicFile.failWrite(stream)
                } catch (failError: Throwable) {
                    error.addSuppressed(failError)
                }
            }
            throw error
        }
    }
}

private fun ProbeCheckpoint.toJson(): JSONObject = JSONObject()
    .put("runId", runId)
    .put("attempt", attempt)
    .put("scenario", scenario)
    .put("strategy", strategy)
    .put("includeCount", includeCount)
    .put("excludeCount", excludeCount)
    .put("builderEntryCount", builderEntryCount)
    .put("planSha256", planSha256)
    .put("phase", phase.name)
    .put("elapsedRealtime", elapsedRealtime)

private fun ProbeReportStore.checkpointFromJson(json: JSONObject): ProbeCheckpoint {
    check(json.keys().asSequence().toSet() == CHECKPOINT_KEYS) {
        "Checkpoint keys do not match the stable schema"
    }
    return ProbeCheckpoint(
        runId = json.strictString("runId"),
        attempt = json.strictInt("attempt"),
        scenario = json.strictString("scenario"),
        strategy = json.strictString("strategy"),
        includeCount = json.strictInt("includeCount"),
        excludeCount = json.strictInt("excludeCount"),
        builderEntryCount = json.strictInt("builderEntryCount"),
        planSha256 = json.strictString("planSha256"),
        phase = CheckpointPhase.valueOf(json.strictString("phase")),
        elapsedRealtime = json.strictLong("elapsedRealtime"),
    )
}

private fun JSONObject.strictString(key: String): String = get(key).let { value ->
    check(value is String) { "Checkpoint field $key must be a string" }
    value
}

private fun JSONObject.strictInt(key: String): Int {
    val number = strictWholeNumber(key)
    check(number in Int.MIN_VALUE..Int.MAX_VALUE) { "Checkpoint field $key is outside Int range" }
    return number.toInt()
}

private fun JSONObject.strictLong(key: String): Long = strictWholeNumber(key)

private fun JSONObject.strictWholeNumber(key: String): Long {
    val value = get(key)
    check(value is Number) { "Checkpoint field $key must be a number" }
    val number = value.toLong()
    check(value.toDouble().isFinite() && value.toDouble() == number.toDouble()) {
        "Checkpoint field $key must be a whole number"
    }
    return number
}

private fun ProbeCheckpoint.hasMatchingMetadata(after: ProbeCheckpoint): Boolean =
    runId == after.runId &&
        attempt == after.attempt &&
        scenario == after.scenario &&
        strategy == after.strategy &&
        includeCount == after.includeCount &&
        excludeCount == after.excludeCount &&
        builderEntryCount == after.builderEntryCount &&
        planSha256 == after.planSha256
