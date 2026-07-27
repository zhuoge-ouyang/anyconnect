package com.msitools.anyconnectmobile.core

import java.net.URI

data class SmartHealthRecord(
    val siteName: String,
    val successes: Int = 0,
    val failures: Int = 0,
    val consecutiveFailures: Int = 0,
    val lastSuccessAtMs: Long = 0L,
    val lastFailureAtMs: Long = 0L,
    val medianMs: Long = 0L,
    val slowestMs: Long = 0L,
    val lastSelectedAtMs: Long = 0L,
) {
    fun inCooldown(nowMs: Long): Boolean =
        consecutiveFailures > 0 && nowMs - lastFailureAtMs < SmartSelectionPolicy.COOLDOWN_MS
}

object SmartCandidatePlanner {
    fun rank(
        sites: List<VpnSite>,
        current: VpnSite,
        history: Map<String, SmartHealthRecord>,
        nowMs: Long,
    ): List<VpnSite> {
        val currentUpstream = upstream(current)
        val distinctUpstreams = linkedMapOf<String, VpnSite>()
        for (site in sites) {
            if (site.name == current.name) continue
            val upstream = upstream(site)
            if (upstream == currentUpstream) continue
            distinctUpstreams.putIfAbsent(upstream, site)
        }
        val eligible = distinctUpstreams.values
            .filterNot { history[it.name]?.inCooldown(nowMs) == true }
            .sortedWith(
                compareByDescending<VpnSite> { successRate(history[it.name]) }
                    .thenByDescending { history[it.name]?.lastSuccessAtMs ?: 0L }
                    .thenBy { history[it.name]?.medianMs ?: Long.MAX_VALUE },
            )
        if (eligible.size <= SmartSelectionPolicy.MAX_CANDIDATES) return eligible
        val first = eligible.first()
        val second = eligible.drop(1).firstOrNull { region(it) != region(first) } ?: eligible[1]
        return listOf(first, second)
    }

    private fun successRate(record: SmartHealthRecord?): Double {
        if (record == null || record.successes + record.failures == 0) return 0.0
        return record.successes.toDouble() / (record.successes + record.failures).toDouble()
    }

    private fun upstream(site: VpnSite): String = runCatching {
        URI(site.server).host.orEmpty().lowercase()
    }.getOrDefault(site.server.lowercase())

    private fun region(site: VpnSite): String = site.name.substringAfter('.', site.name)
}
