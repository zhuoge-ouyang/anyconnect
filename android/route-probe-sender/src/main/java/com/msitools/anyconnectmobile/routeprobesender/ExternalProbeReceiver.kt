package com.msitools.anyconnectmobile.routeprobesender

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.ResultReceiver
import kotlin.concurrent.thread

class ExternalProbeReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ExternalProbeContract.ACTION_SEND) return
        val resultReceiver = intent.resultReceiver()
        val pending = goAsync()
        thread(name = "route-probe-helper-send") {
            try {
                val result = SenderRequest.parse(intent.extras).fold(
                    onSuccess = { request ->
                        runCatching { sendUdpProbe(request) }
                            .fold(
                                onSuccess = { successBundle() },
                                onFailure = { errorBundle(it) },
                            )
                    },
                    onFailure = { errorBundle(it) },
                )
                resultReceiver?.send(RESULT_OK, result)
            } finally {
                pending.finish()
            }
        }
    }
}

private fun Intent.resultReceiver(): ResultReceiver? =
    if (Build.VERSION.SDK_INT >= 33) {
        getParcelableExtra(ExternalProbeContract.KEY_RESULT_RECEIVER, ResultReceiver::class.java)
    } else {
        @Suppress("DEPRECATION")
        getParcelableExtra(ExternalProbeContract.KEY_RESULT_RECEIVER)
    }

private const val RESULT_OK = 1
