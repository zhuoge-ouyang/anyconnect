package com.msitools.anyconnectmobile.service

import android.app.NotificationManager
import android.content.Intent
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.IBinder
import com.msitools.anyconnectmobile.R
import com.msitools.anyconnectmobile.core.ClientPreferences
import com.msitools.anyconnectmobile.core.ConnectionMode
import com.msitools.anyconnectmobile.core.VpnExitAction
import com.msitools.anyconnectmobile.core.VpnRecoveryPolicy
import com.msitools.anyconnectmobile.core.VpnSite
import com.msitools.anyconnectmobile.core.VpnSiteCatalog
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread

class AnyConnectVpnService : VpnService() {
    @Volatile
    private var worker: Thread? = null
    @Volatile
    private var engine: MinimalVpnEngine? = null
    private var nodeName: String = ""
    private var mode: ConnectionMode = ConnectionMode.DOMESTIC_DIRECT
    private var lastRxBytes: Long = 0L
    private var lastTxBytes: Long = 0L
    private var lastStatsAtMs: Long = 0L
    private val connectionGeneration = AtomicInteger(0)

    override fun onBind(intent: Intent?): IBinder? {
        return super.onBind(intent)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_CONNECT -> startConnection(intent)
            ACTION_DISCONNECT -> {
                stopConnection()
                publishStatus(STATE_DISCONNECTED, getString(R.string.status_disconnected))
            }
            null -> restoreLastConnection()
        }
        return START_STICKY
    }

    override fun onDestroy() {
        stopConnection(stopService = false)
        super.onDestroy()
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        android.util.Log.i(TAG, "Task removed; keeping foreground VPN service alive")
        super.onTaskRemoved(rootIntent)
    }

    private fun startConnection(intent: Intent) {
        stopConnection(stopService = false)
        val generation = connectionGeneration.incrementAndGet()
        val server = intent.getStringExtra(EXTRA_SERVER).orEmpty()
        val username = intent.getStringExtra(EXTRA_USERNAME).orEmpty()
        val password = intent.getStringExtra(EXTRA_PASSWORD).orEmpty()
        nodeName = intent.getStringExtra(EXTRA_SITE_NAME).orEmpty()
        mode = ConnectionMode.fromStored(intent.getStringExtra(EXTRA_MODE))
        val rawRules = intent.getStringExtra(EXTRA_RULES).orEmpty()
        resetStats()
        startForeground(
            ConnectionNotifier.NOTIFICATION_ID,
            ConnectionNotifier.build(this, getString(R.string.notification_connecting)),
        )
        publishStatus(STATE_CONNECTING, "正在认证：${nodeName.ifBlank { server }}")
        worker = thread(name = "anyconnect-mobile-engine") {
            var hasConnected = false
            var reauthenticationAttempted = false
            while (generation == connectionGeneration.get() && !Thread.currentThread().isInterrupted) {
                val activeEngine = MinimalVpnEngine(this)
                engine = activeEngine
                val failure = runCatching {
                    awaitPreviousVpnRelease(generation)
                    activeEngine.connect(
                        server = server,
                        username = username,
                        password = password,
                        routingConfig = RoutingConfig(mode, rawRules),
                        onConnected = connected@{
                            if (generation != connectionGeneration.get()) return@connected
                            hasConnected = true
                            reauthenticationAttempted = false
                            val connectedText = "已连接：${nodeName.ifBlank { server }}"
                            publishStatus(STATE_CONNECTED, connectedText)
                            getSystemService(NotificationManager::class.java).notify(
                                ConnectionNotifier.NOTIFICATION_ID,
                                ConnectionNotifier.build(this, connectedText),
                            )
                        },
                        onStats = { stats ->
                            if (generation == connectionGeneration.get()) publishStats(stats)
                        },
                    )
                }.exceptionOrNull()
                if (engine === activeEngine) engine = null
                if (failure == null) break

                when (
                    VpnRecoveryPolicy.decide(
                        generationActive = generation == connectionGeneration.get(),
                        hasConnected = hasConnected,
                        reauthenticationAttempted = reauthenticationAttempted,
                        recoverable = failure !is DnsRuleRefreshException,
                    )
                ) {
                    VpnExitAction.STOP -> break
                    VpnExitAction.FAIL -> {
                        android.util.Log.e(TAG, "VPN connection failed", failure)
                        publishStatus(STATE_FAILED, "连接失败：${failure.message.orEmpty()}")
                        stopSelf()
                        break
                    }
                    VpnExitAction.REAUTHENTICATE -> {
                        reauthenticationAttempted = true
                        android.util.Log.w(TAG, "Established VPN tunnel ended; reauthenticating the same node", failure)
                        val reconnectingText = "网络中断，正在重新认证：${nodeName.ifBlank { server }}"
                        publishStatus(STATE_CONNECTING, reconnectingText)
                        getSystemService(NotificationManager::class.java).notify(
                            ConnectionNotifier.NOTIFICATION_ID,
                            ConnectionNotifier.build(this, reconnectingText),
                        )
                        awaitReconnectNetwork(generation)
                    }
                }
            }
        }
    }

    private fun awaitReconnectNetwork(generation: Int) {
        Thread.sleep(REAUTHENTICATE_DELAY_MS)
        val connectivityManager = getSystemService(ConnectivityManager::class.java)
        while (generation == connectionGeneration.get()) {
            val hasUnderlyingNetwork = connectivityManager.allNetworks.any { network ->
                connectivityManager.getNetworkCapabilities(network)?.let { capabilities ->
                    capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                        !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
                } == true
            }
            if (hasUnderlyingNetwork) return
            Thread.sleep(UNDERLYING_NETWORK_POLL_MS)
        }
    }

    private fun awaitPreviousVpnRelease(generation: Int) {
        val connectivityManager = getSystemService(ConnectivityManager::class.java)
        val deadline = System.currentTimeMillis() + VPN_RELEASE_TIMEOUT_MS
        while (connectivityManager.allNetworks.any { network ->
                connectivityManager.getNetworkCapabilities(network)
                    ?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
            }
        ) {
            check(generation == connectionGeneration.get()) { "连接已被新的操作取消" }
            check(System.currentTimeMillis() < deadline) { "旧 VPN 接口未能及时释放" }
            Thread.sleep(VPN_RELEASE_POLL_MS)
        }
    }

    private fun restoreLastConnection() {
        val settings = ClientPreferences(this).load()
        val site = VpnSiteCatalog.findPreferred(settings.siteName)
            ?: settings.siteServer.takeIf { it.isNotBlank() }?.let { VpnSite(settings.siteName, it) }
            ?: return
        if (settings.username.isBlank() || settings.password.isEmpty()) return
        startConnection(Intent(this, AnyConnectVpnService::class.java).apply {
            action = ACTION_CONNECT
            putExtra(EXTRA_SERVER, site.server)
            putExtra(EXTRA_SITE_NAME, site.name)
            putExtra(EXTRA_USERNAME, settings.username)
            putExtra(EXTRA_PASSWORD, settings.password)
            putExtra(EXTRA_MODE, settings.mode.name)
            putExtra(EXTRA_RULES, settings.rulesFor(settings.mode))
        })
    }

    private fun stopConnection(stopService: Boolean = true) {
        connectionGeneration.incrementAndGet()
        engine?.cancel()
        engine = null
        val previousWorker = worker
        previousWorker?.interrupt()
        if (previousWorker != null && previousWorker !== Thread.currentThread()) {
            runCatching { previousWorker.join(2_000L) }
        }
        worker = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        if (stopService) {
            stopSelf()
        }
    }

    private fun publishStatus(state: String, message: String) {
        android.util.Log.i(TAG, "Publishing VPN status: state=$state, node=$nodeName, mode=${mode.label}")
        ClientPreferences(this).saveConnectionStatus(
            state = state,
            message = message,
            nodeName = nodeName,
            mode = mode,
            rxBps = 0L,
            txBps = 0L,
        )
        sendBroadcast(Intent(ACTION_STATUS).apply {
            setPackage(packageName)
            putExtra(EXTRA_STATE, state)
            putExtra(EXTRA_MESSAGE, message)
            putExtra(EXTRA_SITE_NAME, nodeName)
            putExtra(EXTRA_MODE, mode.name)
            putExtra(EXTRA_RX_BPS, 0L)
            putExtra(EXTRA_TX_BPS, 0L)
        })
        VpnStatusHub.publish(
            VpnStatusUpdate(
                state = state,
                message = message,
                nodeName = nodeName,
                modeName = mode.name,
                rxBps = 0L,
                txBps = 0L,
            ),
        )
    }

    private fun publishStats(stats: VpnTrafficStats) {
        val now = System.currentTimeMillis()
        val elapsedMs = now - lastStatsAtMs
        val rxBps = if (elapsedMs > 0L) ((stats.rxBytes - lastRxBytes).coerceAtLeast(0L) * 1000L) / elapsedMs else 0L
        val txBps = if (elapsedMs > 0L) ((stats.txBytes - lastTxBytes).coerceAtLeast(0L) * 1000L) / elapsedMs else 0L
        lastRxBytes = stats.rxBytes
        lastTxBytes = stats.txBytes
        lastStatsAtMs = now
        ClientPreferences(this).saveConnectionStatus(
            state = STATE_STATS,
            message = "已连接：$nodeName",
            nodeName = nodeName,
            mode = mode,
            rxBps = rxBps,
            txBps = txBps,
        )
        sendBroadcast(Intent(ACTION_STATUS).apply {
            setPackage(packageName)
            putExtra(EXTRA_STATE, STATE_STATS)
            putExtra(EXTRA_MESSAGE, "已连接：$nodeName")
            putExtra(EXTRA_SITE_NAME, nodeName)
            putExtra(EXTRA_MODE, mode.name)
            putExtra(EXTRA_RX_BPS, rxBps)
            putExtra(EXTRA_TX_BPS, txBps)
        })
        VpnStatusHub.publish(
            VpnStatusUpdate(
                state = STATE_STATS,
                message = "已连接：$nodeName",
                nodeName = nodeName,
                modeName = mode.name,
                rxBps = rxBps,
                txBps = txBps,
            ),
        )
    }

    private fun resetStats() {
        lastRxBytes = 0L
        lastTxBytes = 0L
        lastStatsAtMs = 0L
    }

    companion object {
        const val ACTION_CONNECT = "com.msitools.anyconnectmobile.CONNECT"
        const val ACTION_DISCONNECT = "com.msitools.anyconnectmobile.DISCONNECT"
        const val ACTION_STATUS = "com.msitools.anyconnectmobile.STATUS"
        const val EXTRA_SERVER = "server"
        const val EXTRA_SITE_NAME = "siteName"
        const val EXTRA_USERNAME = "username"
        const val EXTRA_PASSWORD = "password"
        const val EXTRA_MODE = "mode"
        const val EXTRA_RULES = "rules"
        const val EXTRA_STATE = "state"
        const val EXTRA_MESSAGE = "message"
        const val EXTRA_RX_BPS = "rxBps"
        const val EXTRA_TX_BPS = "txBps"
        const val STATE_CONNECTING = "connecting"
        const val STATE_CONNECTED = "connected"
        const val STATE_STATS = "stats"
        const val STATE_DISCONNECTED = "disconnected"
        const val STATE_FAILED = "failed"
        private const val VPN_RELEASE_TIMEOUT_MS = 10_000L
        private const val VPN_RELEASE_POLL_MS = 50L
        private const val REAUTHENTICATE_DELAY_MS = 1_000L
        private const val UNDERLYING_NETWORK_POLL_MS = 250L
        private const val TAG = "AnyConnectVpnService"
    }
}
