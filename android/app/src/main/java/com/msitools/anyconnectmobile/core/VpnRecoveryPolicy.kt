package com.msitools.anyconnectmobile.core

enum class VpnExitAction {
    STOP,
    FAIL,
    REAUTHENTICATE,
}

object VpnRecoveryPolicy {
    fun decide(
        generationActive: Boolean,
        hasConnected: Boolean,
        reauthenticationAttempted: Boolean,
        recoverable: Boolean = true,
    ): VpnExitAction = when {
        !generationActive -> VpnExitAction.STOP
        !recoverable -> VpnExitAction.FAIL
        hasConnected && !reauthenticationAttempted -> VpnExitAction.REAUTHENTICATE
        else -> VpnExitAction.FAIL
    }
}
