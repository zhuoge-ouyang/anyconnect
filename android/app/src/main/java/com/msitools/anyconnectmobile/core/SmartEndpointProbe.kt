package com.msitools.anyconnectmobile.core

import java.net.HttpURLConnection
import java.net.URL

class SmartEndpointProbe(
    private val vpnActive: () -> Boolean,
) {
    fun runRound(deadlineAtMs: Long): SmartSelectionPolicy.Round {
        val startedAt = System.currentTimeMillis()
        val chatGpt = probeEndpoint(
            url = "https://chatgpt.com/backend-api/codex/responses",
            method = "POST",
            body = "{}",
            deadlineAtMs = deadlineAtMs,
        )
        val openAi = probeEndpoint(
            url = "https://api.openai.com/v1/models",
            method = "GET",
            body = null,
            deadlineAtMs = deadlineAtMs,
        )
        val trace = probeTrace(deadlineAtMs)
        return SmartSelectionPolicy.Round(
            chatGptHealthy = chatGpt.healthy,
            openAiHealthy = openAi.healthy,
            durationMs = System.currentTimeMillis() - startedAt,
            blocked = chatGpt.blocked || openAi.blocked,
            exitIp = trace.first,
            exitRegion = trace.second,
            vpnActive = vpnActive(),
        )
    }

    private fun probeEndpoint(
        url: String,
        method: String,
        body: String?,
        deadlineAtMs: Long,
    ): EndpointResult = runCatching {
        val connection = open(url, deadlineAtMs).apply {
            requestMethod = method
            setRequestProperty("Accept", "application/json")
            setRequestProperty("User-Agent", "anyconnect-mobile-smart-probe/1.0")
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                outputStream.use { it.write(body.toByteArray()) }
            }
        }
        try {
            val status = connection.responseCode
            val mitigated = connection.getHeaderField("cf-mitigated").orEmpty()
            EndpointResult(
                healthy = status == HttpURLConnection.HTTP_UNAUTHORIZED && !mitigated.equals("challenge", true),
                blocked = status == HttpURLConnection.HTTP_FORBIDDEN || mitigated.equals("challenge", true),
            )
        } finally {
            connection.disconnect()
        }
    }.getOrDefault(EndpointResult(false, false))

    private fun probeTrace(deadlineAtMs: Long): Pair<String, String> = runCatching {
        val connection = open("https://chatgpt.com/cdn-cgi/trace", deadlineAtMs)
        try {
            val values = connection.inputStream.bufferedReader().useLines { lines ->
                lines.mapNotNull { line ->
                    line.split('=', limit = 2).takeIf { it.size == 2 }?.let { it[0] to it[1].trim() }
                }.toMap()
            }
            values["ip"].orEmpty() to values["colo"].orEmpty()
        } finally {
            connection.disconnect()
        }
    }.getOrDefault("" to "")

    private fun open(url: String, deadlineAtMs: Long): HttpURLConnection {
        val remaining = (deadlineAtMs - System.currentTimeMillis()).coerceIn(1L, 6_000L).toInt()
        return (URL(url).openConnection() as HttpURLConnection).apply {
            connectTimeout = remaining
            readTimeout = remaining
            useCaches = false
            instanceFollowRedirects = false
        }
    }

    private data class EndpointResult(val healthy: Boolean, val blocked: Boolean)
}
