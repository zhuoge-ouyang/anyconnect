package com.msitools.anyconnectmobile.core

import java.net.URI

data class ParsedRouteRule(
    val value: String,
    val prefixLength: Int?,
    val isHost: Boolean,
)

object RouteRuleParser {
    fun parse(rawRules: String): List<ParsedRouteRule> =
        rawRules.lineSequence()
            .map { it.trim() }
            .filter { it.isNotEmpty() }
            .filterNot { it.startsWith("#") }
            .mapNotNull(::parseLine)
            .distinctBy { "${it.value}/${it.prefixLength}/${it.isHost}" }
            .toList()

    private fun parseLine(line: String): ParsedRouteRule? {
        val normalized = normalize(line) ?: return null
        val slashIndex = normalized.indexOf('/')
        if (slashIndex > 0 && normalized.substring(slashIndex + 1).all(Char::isDigit)) {
            val address = normalized.substring(0, slashIndex)
            val prefix = normalized.substring(slashIndex + 1).toIntOrNull() ?: return null
            return ParsedRouteRule(address, prefix, isHost = false)
        }
        return ParsedRouteRule(normalized, prefixLength = null, isHost = !looksLikeIp(normalized))
    }

    private fun normalize(line: String): String? {
        val withoutComment = line.substringBefore("#").trim().trimEnd(',')
        if (withoutComment.isBlank()) return null
        if (!withoutComment.contains("://")) return withoutComment.trim('/')

        return runCatching {
            URI(withoutComment).host
        }.getOrNull()?.takeIf { it.isNotBlank() }
    }

    private fun looksLikeIp(value: String): Boolean =
        value.contains(':') || value.all { it.isDigit() || it == '.' }
}
