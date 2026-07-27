package com.msitools.anyconnectmobile.core

data class RuleExpansionResult(
    val rulesText: String,
    val addedCount: Int,
)

object WhitelistRuleExpander {
    private val aliases = mapOf(
        "google" to listOf(
            "google.com",
            "www.google.com",
            "accounts.google.com",
            "clients3.google.com",
            "connectivitycheck.gstatic.com",
            "gstatic.com",
            "www.gstatic.com",
            "googleapis.com",
            "www.googleapis.com",
            "googleusercontent.com",
            "lh3.googleusercontent.com",
            "googlevideo.com",
            "youtube.com",
            "www.youtube.com",
            "ytimg.com",
            "i.ytimg.com",
        ),
        "google.com" to listOf(
            "google.com",
            "www.google.com",
            "accounts.google.com",
            "clients3.google.com",
            "connectivitycheck.gstatic.com",
            "gstatic.com",
            "www.gstatic.com",
            "googleapis.com",
            "www.googleapis.com",
            "googleusercontent.com",
            "lh3.googleusercontent.com",
            "googlevideo.com",
            "youtube.com",
            "www.youtube.com",
            "ytimg.com",
            "i.ytimg.com",
        ),
        "谷歌" to listOf(
            "google.com",
            "www.google.com",
            "accounts.google.com",
            "clients3.google.com",
            "connectivitycheck.gstatic.com",
            "gstatic.com",
            "www.gstatic.com",
            "googleapis.com",
            "www.googleapis.com",
            "googleusercontent.com",
            "lh3.googleusercontent.com",
            "googlevideo.com",
            "youtube.com",
            "www.youtube.com",
            "ytimg.com",
            "i.ytimg.com",
        ),
        "goole" to listOf(
            "google.com",
            "www.google.com",
            "accounts.google.com",
            "clients3.google.com",
            "connectivitycheck.gstatic.com",
            "gstatic.com",
            "www.gstatic.com",
            "googleapis.com",
            "www.googleapis.com",
            "googleusercontent.com",
            "lh3.googleusercontent.com",
            "googlevideo.com",
            "youtube.com",
            "www.youtube.com",
            "ytimg.com",
            "i.ytimg.com",
        ),
        "youtube" to listOf(
            "youtube.com",
            "www.youtube.com",
            "m.youtube.com",
            "youtu.be",
            "ytimg.com",
            "i.ytimg.com",
            "googlevideo.com",
        ),
        "youtube.com" to listOf(
            "youtube.com",
            "www.youtube.com",
            "m.youtube.com",
            "youtu.be",
            "ytimg.com",
            "i.ytimg.com",
            "googlevideo.com",
        ),
    )

    fun mergeAndExpand(existingRules: String, newRules: String): RuleExpansionResult {
        val existing = expand(existingRules)
        val added = expand(newRules)
        val merged = (existing + added).distinct()
        return RuleExpansionResult(
            rulesText = merged.joinToString("\n"),
            addedCount = (merged - existing.toSet()).size,
        )
    }

    fun expand(rawRules: String): List<String> =
        splitRawRules(rawRules)
            .flatMap(::expandOne)
            .let { RouteRuleParser.parse(it.joinToString("\n")) }
            .map(::formatParsedRule)
            .distinct()

    private fun splitRawRules(rawRules: String): List<String> =
        rawRules.lineSequence()
            .flatMap { it.split(',', '，', ';', '；').asSequence() }
            .map { it.substringBefore("#").trim().trimEnd(',') }
            .filter { it.isNotEmpty() }
            .toList()

    private fun expandOne(input: String): List<String> {
        val key = input.lowercase()
            .removePrefix("https://")
            .removePrefix("http://")
            .substringBefore("/")
            .removePrefix("www.")
            .trim()
        return aliases[key] ?: aliases[input.lowercase().trim()] ?: listOf(input)
    }

    private fun formatParsedRule(rule: ParsedRouteRule): String =
        if (rule.prefixLength == null) rule.value else "${rule.value}/${rule.prefixLength}"
}
