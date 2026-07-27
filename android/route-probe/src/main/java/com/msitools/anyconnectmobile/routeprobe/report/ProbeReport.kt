package com.msitools.anyconnectmobile.routeprobe.report

import org.json.JSONArray
import org.json.JSONObject

enum class GateDecision { PASS, BLOCKED, INCONCLUSIVE }

data class ProbeDeviceMetadata(
    val model: String,
    val sdkInt: Int,
    val supportedAbis: List<String>,
    val pageSizeBytes: Long,
    val fingerprint: String,
)

data class ProbeIpDbMetadata(
    val sha256: String,
    val rawRecordCount: Int,
    val ipv4RawCount: Int,
    val ipv6RawCount: Int,
    val ipv4CollapsedCount: Int,
    val ipv6CollapsedCount: Int,
    val ipv4ComplementCount: Int,
    val ipv6ComplementCount: Int,
)

data class ProbeScenarioResult(
    val scenario: String,
    val attempt: Int,
    val strategy: String,
    val includeCount: Int,
    val excludeCount: Int,
    val builderEntryCount: Int,
    val planSha256: String,
    val establishMs: Long,
    val rssBeforeKb: Int,
    val rssAfterKb: Int,
    val rssHighWaterKb: Int,
    val fdCountBefore: Int,
    val fdCountAfter: Int,
    val expectedCaptured: List<String>,
    val capturedChecksPassed: Boolean,
    val expectedBypass: List<String>,
    val bypassChecksPassed: Boolean,
    val directResponseStatus: String,
    val establishStatus: String,
    val errorClass: String?,
    val errorMessage: String?,
)

data class ProbeReport(
    val generatedAtEpochMs: Long,
    val device: ProbeDeviceMetadata,
    val ipdb: ProbeIpDbMetadata,
    val scenarios: List<ProbeScenarioResult>,
    val gateDecision: GateDecision,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("schemaVersion", SCHEMA_VERSION)
        .put("generatedAtEpochMs", generatedAtEpochMs)
        .put("device", device.toJson())
        .put("ipdb", ipdb.toJson())
        .put(
            "scenarios",
            JSONArray().also { array ->
                scenarios.forEach { array.put(it.toJson()) }
            },
        )
        .put("gateDecision", gateDecision.name)

    companion object {
        const val SCHEMA_VERSION = 1
    }
}

private fun ProbeDeviceMetadata.toJson(): JSONObject = JSONObject()
    .put("model", model)
    .put("sdkInt", sdkInt)
    .put(
        "supportedAbis",
        JSONArray().also { array -> supportedAbis.forEach(array::put) },
    )
    .put("pageSizeBytes", pageSizeBytes)
    .put("fingerprint", fingerprint)

private fun ProbeIpDbMetadata.toJson(): JSONObject = JSONObject()
    .put("sha256", sha256)
    .put("rawRecordCount", rawRecordCount)
    .put("ipv4RawCount", ipv4RawCount)
    .put("ipv6RawCount", ipv6RawCount)
    .put("ipv4CollapsedCount", ipv4CollapsedCount)
    .put("ipv6CollapsedCount", ipv6CollapsedCount)
    .put("ipv4ComplementCount", ipv4ComplementCount)
    .put("ipv6ComplementCount", ipv6ComplementCount)

private fun ProbeScenarioResult.toJson(): JSONObject = JSONObject()
    .put("scenario", scenario)
    .put("attempt", attempt)
    .put("strategy", strategy)
    .put("includeCount", includeCount)
    .put("excludeCount", excludeCount)
    .put("builderEntryCount", builderEntryCount)
    .put("planSha256", planSha256)
    .put("establishMs", establishMs)
    .put("rssBeforeKb", rssBeforeKb)
    .put("rssAfterKb", rssAfterKb)
    .put("rssHighWaterKb", rssHighWaterKb)
    .put("fdCountBefore", fdCountBefore)
    .put("fdCountAfter", fdCountAfter)
    .put(
        "expectedCaptured",
        JSONArray().also { array -> expectedCaptured.forEach(array::put) },
    )
    .put("capturedChecksPassed", capturedChecksPassed)
    .put(
        "expectedBypass",
        JSONArray().also { array -> expectedBypass.forEach(array::put) },
    )
    .put("bypassChecksPassed", bypassChecksPassed)
    .put("directResponseStatus", directResponseStatus)
    .put("establishStatus", establishStatus)
    .put("errorClass", errorClass ?: JSONObject.NULL)
    .put("errorMessage", errorMessage ?: JSONObject.NULL)
