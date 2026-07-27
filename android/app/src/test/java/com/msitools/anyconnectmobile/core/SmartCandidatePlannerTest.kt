package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Test

class SmartCandidatePlannerTest {
    @Test
    fun excludesCurrentUpstreamAndCooldownAndReturnsAtMostTwo() {
        val now = 10_000_000L
        val current = VpnSite("03.深圳", "https://shared.example:10000")
        val sites = listOf(
            current,
            VpnSite("05.贵州", "https://shared.example:10000"),
            VpnSite("22.日本", "https://jp.example:10000"),
            VpnSite("21.韩国", "https://kr.example:10000"),
            VpnSite("24.美国", "https://us.example:10000"),
        )
        val history = mapOf(
            "22.日本" to SmartHealthRecord("22.日本", successes = 4, lastSuccessAtMs = now - 1_000L),
            "21.韩国" to SmartHealthRecord(
                "21.韩国",
                consecutiveFailures = 1,
                lastFailureAtMs = now - 500L,
            ),
        )
        assertEquals(
            listOf("22.日本", "24.美国"),
            SmartCandidatePlanner.rank(sites, current, history, now).map { it.name },
        )
    }
}
