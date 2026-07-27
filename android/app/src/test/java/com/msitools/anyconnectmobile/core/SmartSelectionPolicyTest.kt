package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SmartSelectionPolicyTest {
    private fun healthy(count: Int, durationMs: Long = 900L, ip: String = "203.0.113.8") =
        List(count) {
            SmartSelectionPolicy.Round(true, true, durationMs, false, ip, "SIN", true)
        }

    @Test
    fun currentRequiresThreeStrictRoundsAndCandidateRequiresFour() {
        assertTrue(SmartSelectionPolicy.currentHealthy(SmartSelectionPolicy.evaluate(healthy(3))))
        assertFalse(SmartSelectionPolicy.candidateQualified(SmartSelectionPolicy.evaluate(healthy(3))))
        assertTrue(SmartSelectionPolicy.candidateQualified(SmartSelectionPolicy.evaluate(healthy(4))))
    }

    @Test
    fun oneEndpointFailureOrMissingVpnProofRejectsCandidate() {
        val endpointFailure = healthy(4).toMutableList().apply {
            this[2] = this[2].copy(openAiHealthy = false)
        }
        assertFalse(SmartSelectionPolicy.candidateQualified(SmartSelectionPolicy.evaluate(endpointFailure)))
        assertFalse(
            SmartSelectionPolicy.candidateQualified(
                SmartSelectionPolicy.evaluate(healthy(4).map { it.copy(vpnActive = false) }),
            ),
        )
    }

    @Test
    fun forcedDeepScanRequiresBothRelativeAndAbsoluteImprovement() {
        val current = SmartSelectionPolicy.evaluate(healthy(3, 2_000L))
        assertTrue(SmartSelectionPolicy.materiallyBetter(current, SmartSelectionPolicy.evaluate(healthy(4, 1_400L))))
        assertFalse(SmartSelectionPolicy.materiallyBetter(current, SmartSelectionPolicy.evaluate(healthy(4, 1_600L))))
    }

    @Test
    fun mandatoryProbeDomainsAreAlwaysVpnRulesInDomesticDirectMode() {
        val rules = SmartSelectionPolicy.effectiveVpnRules(ConnectionMode.DOMESTIC_DIRECT, "youtube.com")
        assertTrue(rules.lines().containsAll(listOf("chatgpt.com", "api.openai.com", "youtube.com")))
    }
}
