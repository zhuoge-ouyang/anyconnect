package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Test

class VpnRecoveryPolicyTest {
    @Test
    fun staleConnectionStopsWithoutPublishingFailure() {
        assertEquals(
            VpnExitAction.STOP,
            VpnRecoveryPolicy.decide(
                generationActive = false,
                hasConnected = true,
                reauthenticationAttempted = false,
            ),
        )
    }

    @Test
    fun initialAuthenticationFailureIsReported() {
        assertEquals(
            VpnExitAction.FAIL,
            VpnRecoveryPolicy.decide(
                generationActive = true,
                hasConnected = false,
                reauthenticationAttempted = false,
            ),
        )
    }

    @Test
    fun establishedTunnelFailureReauthenticatesSameConnection() {
        assertEquals(
            VpnExitAction.REAUTHENTICATE,
            VpnRecoveryPolicy.decide(
                generationActive = true,
                hasConnected = true,
                reauthenticationAttempted = false,
            ),
        )
    }

    @Test
    fun failedReauthenticationIsReportedWithoutAnAutomaticLoop() {
        assertEquals(
            VpnExitAction.FAIL,
            VpnRecoveryPolicy.decide(
                generationActive = true,
                hasConnected = true,
                reauthenticationAttempted = true,
            ),
        )
    }

    @Test
    fun dnsRouteRefreshFailureIsTerminalWithoutReauthentication() {
        assertEquals(
            VpnExitAction.FAIL,
            VpnRecoveryPolicy.decide(
                generationActive = true,
                hasConnected = true,
                reauthenticationAttempted = false,
                recoverable = false,
            ),
        )
    }
}
