package com.msitools.anyconnectmobile.service

import android.os.Handler
import android.os.Looper
import java.util.concurrent.CopyOnWriteArraySet

data class VpnStatusUpdate(
    val state: String,
    val message: String,
    val nodeName: String,
    val modeName: String,
    val rxBps: Long,
    val txBps: Long,
)

object VpnStatusHub {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val listeners = CopyOnWriteArraySet<(VpnStatusUpdate) -> Unit>()

    fun addListener(listener: (VpnStatusUpdate) -> Unit) {
        listeners += listener
    }

    fun removeListener(listener: (VpnStatusUpdate) -> Unit) {
        listeners -= listener
    }

    fun publish(update: VpnStatusUpdate) {
        mainHandler.post {
            listeners.forEach { it(update) }
        }
    }
}
