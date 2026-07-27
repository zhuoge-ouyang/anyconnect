package com.msitools.anyconnectmobile.routeprobe.service

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.os.ResultReceiver
import android.util.Log
import com.msitools.anyconnectmobile.routeprobe.ProbeContract
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicReference

class RouteProbeVpnService : android.net.VpnService() {
    private val lifecycle = ProbeRunLifecycle()
    private val runnerOwner = HandoverOwner<ProbeRunner>()
    private val executor = AtomicReference<ExecutorService?>()

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val request = parseProbeStartRequest(
            action = intent?.action,
            expectedAction = ProbeContract.ACTION_RUN,
            receiverLoader = { intent.receiver() },
        )
        val receiver = when (request) {
            is ProbeStartRequest.Ready -> request.receiver
            ProbeStartRequest.UnsupportedAction -> {
                cleanupInactiveStart(startId)
                return START_NOT_STICKY
            }

            ProbeStartRequest.MissingReceiver -> {
                cleanupInactiveStart(startId)
                return START_NOT_STICKY
            }

            is ProbeStartRequest.InvalidReceiver -> {
                Log.e(LOG_TAG, "Unable to unpack route probe ResultReceiver", request.error)
                cleanupInactiveStart(startId)
                return START_NOT_STICKY
            }
        }

        when (lifecycle.tryStart()) {
            ProbeRunStartResult.ALREADY_RUNNING -> {
                receiver.sendFailure("A route probe run is already active")
                return START_NOT_STICKY
            }

            ProbeRunStartResult.TERMINAL -> {
                receiver.sendFailure("Route probe service is stopping and cannot accept a new run")
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelfResult(startId)
                return START_NOT_STICKY
            }

            ProbeRunStartResult.STARTED -> Unit
        }

        try {
            ProbeNotification.startForeground(this)
        } catch (error: Exception) {
            lifecycle.cancelOnce()
            receiver.sendFailure(error.describe())
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelfResult(startId)
            return START_NOT_STICKY
        }

        val worker = Executors.newSingleThreadExecutor { runnable ->
            Thread(runnable, "route-probe-runner")
        }
        if (!executor.compareAndSet(null, worker)) {
            worker.shutdownNow()
            lifecycle.cancelOnce()
            receiver.sendFailure("Route probe executor already exists")
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelfResult(startId)
            return START_NOT_STICKY
        }
        try {
            worker.execute {
                var activeRunner: ProbeRunner? = null
                try {
                    activeRunner = ProbeRunner.create(this) { message ->
                        receiver.sendMessage(ProbeContract.RESULT_PROGRESS, message)
                    }
                    if (!runnerOwner.trySeed(activeRunner)) {
                        receiver.sendFailure("Route probe run was cancelled before execution")
                        return@execute
                    }
                    val result = activeRunner.run()
                    receiver.send(
                        ProbeContract.RESULT_COMPLETE,
                        Bundle().apply {
                            putString(ProbeContract.KEY_MESSAGE, result.completionMessage())
                            putString(ProbeContract.KEY_REPORT_JSON, result.report.toJson().toString())
                            putString(
                                ProbeContract.KEY_REPORT_PATH,
                                result.reportPath.absolutePath,
                            )
                        },
                    )
                } catch (error: Exception) {
                    receiver.sendFailure(error.describe())
                } finally {
                    try {
                        activeRunner?.let(runnerOwner::release)
                    } catch (error: Exception) {
                        Log.e(LOG_TAG, "Unable to close route probe runner", error)
                    } finally {
                        executor.getAndSet(null)?.shutdown()
                        lifecycle.finish()
                        stopForeground(STOP_FOREGROUND_REMOVE)
                        stopSelf()
                    }
                }
            }
        } catch (error: Exception) {
            executor.compareAndSet(worker, null)
            worker.shutdownNow()
            lifecycle.cancelOnce()
            receiver.sendFailure(error.describe())
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelfResult(startId)
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        cancelAndRelease()
        super.onRevoke()
    }

    override fun onDestroy() {
        cancelAndRelease()
        super.onDestroy()
    }

    private fun cancelAndRelease() {
        if (!lifecycle.cancelOnce()) return
        try {
            runnerOwner.close()
        } catch (error: Exception) {
            Log.e(LOG_TAG, "Unable to close cancelled route probe runner", error)
        } finally {
            executor.getAndSet(null)?.shutdownNow()
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun cleanupInactiveStart(startId: Int) {
        if (lifecycle.isRunning()) return
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelfResult(startId)
    }

    private companion object {
        const val LOG_TAG = "RouteProbeVpnService"
    }
}

internal sealed interface ProbeStartRequest<out T> {
    data class Ready<T>(val receiver: T) : ProbeStartRequest<T>
    data object UnsupportedAction : ProbeStartRequest<Nothing>
    data object MissingReceiver : ProbeStartRequest<Nothing>
    data class InvalidReceiver(val error: RuntimeException) : ProbeStartRequest<Nothing>
}

internal fun <T> parseProbeStartRequest(
    action: String?,
    expectedAction: String,
    receiverLoader: () -> T?,
): ProbeStartRequest<T> {
    if (action != expectedAction) return ProbeStartRequest.UnsupportedAction
    return try {
        receiverLoader()?.let { receiver -> ProbeStartRequest.Ready(receiver) }
            ?: ProbeStartRequest.MissingReceiver
    } catch (error: RuntimeException) {
        ProbeStartRequest.InvalidReceiver(error)
    }
}

private fun Intent?.receiver(): ResultReceiver? {
    this ?: return null
    return if (Build.VERSION.SDK_INT >= 33) {
        getParcelableExtra(ProbeContract.EXTRA_RECEIVER, ResultReceiver::class.java)
    } else {
        @Suppress("DEPRECATION")
        getParcelableExtra(ProbeContract.EXTRA_RECEIVER)
    }
}

private fun ResultReceiver?.sendMessage(resultCode: Int, message: String) {
    this?.send(
        resultCode,
        Bundle().apply { putString(ProbeContract.KEY_MESSAGE, message) },
    )
}

private fun ResultReceiver?.sendFailure(message: String) {
    sendMessage(ProbeContract.RESULT_FAILED, message)
}

private fun Exception.describe(): String = javaClass.name + ": " + message
