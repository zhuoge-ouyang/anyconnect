package com.msitools.anyconnectmobile.core

object SmartSelectionPolicy {
    const val DEADLINE_MS = 60_000L
    const val DECISION_COUNTDOWN_MS = 20_000L
    const val COOLDOWN_MS = 30 * 60_000L
    const val CURRENT_ROUNDS = 3
    const val CANDIDATE_ROUNDS = 4
    const val MAX_CANDIDATES = 2
    const val MEDIAN_LIMIT_MS = 2_500L
    const val SLOWEST_LIMIT_MS = 5_000L
    const val MIN_IMPROVEMENT_MS = 300L

    val mandatoryVpnDomains = listOf("chatgpt.com", "api.openai.com")

    data class Round(
        val chatGptHealthy: Boolean,
        val openAiHealthy: Boolean,
        val durationMs: Long,
        val blocked: Boolean,
        val exitIp: String,
        val exitRegion: String,
        val vpnActive: Boolean,
    )

    data class Metrics(
        val attempts: Int,
        val successes: Int,
        val medianMs: Long,
        val slowestMs: Long,
        val exitIp: String,
        val exitRegion: String,
    )

    fun evaluate(rounds: List<Round>): Metrics {
        val valid = rounds.filter {
            it.chatGptHealthy && it.openAiHealthy && !it.blocked &&
                it.vpnActive && it.exitIp.isNotBlank()
        }
        val durations = valid.map { it.durationMs }.sorted()
        return Metrics(
            attempts = rounds.size,
            successes = valid.size,
            medianMs = durations.getOrElse(durations.size / 2) { 0L },
            slowestMs = durations.lastOrNull() ?: 0L,
            exitIp = valid.lastOrNull()?.exitIp.orEmpty(),
            exitRegion = valid.lastOrNull()?.exitRegion.orEmpty(),
        )
    }

    fun currentHealthy(metrics: Metrics): Boolean = qualified(metrics, CURRENT_ROUNDS)

    fun candidateQualified(metrics: Metrics): Boolean = qualified(metrics, CANDIDATE_ROUNDS)

    fun materiallyBetter(current: Metrics, candidate: Metrics): Boolean {
        if (current.medianMs <= 0L || candidate.medianMs <= 0L || candidate.successes < current.successes) {
            return false
        }
        return current.medianMs - candidate.medianMs >= MIN_IMPROVEMENT_MS &&
            candidate.medianMs.toDouble() <= current.medianMs.toDouble() * 0.75
    }

    fun effectiveVpnRules(mode: ConnectionMode, userRules: String): String {
        if (mode != ConnectionMode.DOMESTIC_DIRECT) return userRules
        return (WhitelistRuleExpander.expand(userRules) + mandatoryVpnDomains)
            .distinct()
            .joinToString("\n")
    }

    private fun qualified(metrics: Metrics, rounds: Int): Boolean =
        metrics.attempts == rounds && metrics.successes == rounds &&
            metrics.medianMs in 1..MEDIAN_LIMIT_MS && metrics.slowestMs <= SLOWEST_LIMIT_MS &&
            metrics.exitIp.isNotBlank()
}
