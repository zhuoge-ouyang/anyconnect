package com.msitools.anyconnectmobile.service

import android.annotation.TargetApi
import android.net.ConnectivityManager
import android.net.IpPrefix
import android.net.Network
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import com.msitools.anyconnectmobile.core.ConnectionMode
import com.msitools.anyconnectmobile.core.IpCidr
import com.msitools.anyconnectmobile.core.IpFamily
import com.msitools.anyconnectmobile.core.RouteRuleParser
import com.msitools.anyconnectmobile.core.VpnRoutePlanner
import java.io.Closeable
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.infradead.libopenconnect.LibOpenConnect

data class RoutingConfig(
    val mode: ConnectionMode,
    val rawRules: String,
)

data class VpnTrafficStats(
    val rxBytes: Long,
    val txBytes: Long,
)

class MinimalVpnEngine(
    private val service: VpnService,
) {
    @Volatile
    private var activeClient: AndroidOpenConnectClient? = null

    fun connect(
        server: String,
        username: String,
        password: String,
        routingConfig: RoutingConfig,
        onConnected: () -> Unit,
        onStats: (VpnTrafficStats) -> Unit,
    ) {
        require(server.startsWith("https://")) { "server must be https" }
        require(username.isNotBlank()) { "username is blank" }
        require(password.isNotEmpty()) { "password is empty" }
        OpenConnectLibrary.load()
        val client = AndroidOpenConnectClient(
            service = service,
            username = username,
            password = password,
            routingConfig = routingConfig,
            onStats = onStats,
            onTunnelEstablished = onConnected,
        )
        activeClient = client
        var statsTicker: Thread? = null
        try {
            client.setProtocol("anyconnect")
            client.setReportedOS("win")
            client.setVersionString("2.2.0133")
            client.setSystemTrust(true)
            client.setLogLevel(LibOpenConnect.PRG_INFO)
            check(client.parseURL(server) == 0) { "OpenConnect rejected server URL" }
            val cookieResult = client.obtainCookie()
            check(cookieResult == 0) { "OpenConnect authentication failed: $cookieResult" }
            check(client.makeCSTPConnection() == 0) { "OpenConnect CSTP connection failed" }
            check(client.setupDTLS(60) == 0) { "OpenConnect DTLS setup failed" }
            statsTicker = kotlin.concurrent.thread(name = "anyconnect-mobile-stats") {
                if (!client.awaitFinalRoutes()) return@thread
                while (!Thread.currentThread().isInterrupted) {
                    client.requestStats()
                    try {
                        Thread.sleep(1000)
                    } catch (_: InterruptedException) {
                        Thread.currentThread().interrupt()
                    }
                }
            }
            var mainloopResult: Int
            while (true) {
                mainloopResult = client.mainloop(
                    NATIVE_RECONNECT_TIMEOUT_SECONDS,
                    LibOpenConnect.RECONNECT_INTERVAL_MIN,
                )
                val switchRequest = client.takePendingTunSwitch() ?: break
                if (mainloopResult != 0) {
                    val failure = DnsRuleRefreshException(
                        "OpenConnect exited with $mainloopResult before the VPN route table could be refreshed",
                    )
                    client.failTunSwitch(switchRequest, failure)
                    throw failure
                }
                client.applyTunSwitch(switchRequest)
            }
            client.tunSetupFailure()?.let { failure ->
                throw IllegalStateException("Android VPN interface setup failed: ${failure.message}", failure)
            }
            client.routeRefreshFailure()?.let { failure ->
                throw failure
            }
            if (!client.isCanceled()) {
                throw IllegalStateException("VPN tunnel ended unexpectedly: $mainloopResult")
            }
        } finally {
            statsTicker?.interrupt()
            if (activeClient === client) activeClient = null
            client.close()
        }
    }

    fun cancel() {
        activeClient?.stop()
    }

    fun protect(fd: Int): Boolean = service.protect(fd)

    companion object {
        private const val NATIVE_RECONNECT_TIMEOUT_SECONDS = 30
    }
}

private object OpenConnectLibrary {
    @Volatile
    private var loaded = false

    fun load() {
        if (loaded) return
        synchronized(this) {
            if (!loaded) {
                System.loadLibrary("openconnect")
                loaded = true
            }
        }
    }
}

private data class TunAddress(
    val address: InetAddress,
    val prefixLength: Int,
)

private data class TunConfiguration(
    val addresses: List<TunAddress>,
    val dnsServers: List<InetAddress>,
    val mtu: Int,
) {
    val families: Set<IpFamily>
        get() = addresses.mapTo(linkedSetOf()) { IpFamily.from(it.address) }

    fun routeDnsServer(): InetAddress =
        dnsServers.firstOrNull { IpFamily.from(it) in families }
            ?: error("VPN did not provide a DNS server reachable through its address families")
}

private data class RouteRuleSet(
    val staticRoutes: List<IpCidr>,
    val hostNames: List<String>,
)

private class TunSwitchRequest(
    val routes: List<IpCidr>,
) {
    val completed = CountDownLatch(1)

    @Volatile
    var failure: Throwable? = null
}

private class AndroidOpenConnectClient(
    private val service: VpnService,
    private val username: String,
    private val password: String,
    private val routingConfig: RoutingConfig,
    private val onStats: (VpnTrafficStats) -> Unit,
    private val onTunnelEstablished: () -> Unit,
) : LibOpenConnect("AnyConnect Mobile/0.1"), Closeable {
    private var tunFd: android.os.ParcelFileDescriptor? = null
    private var nativeTunFd: Int? = null
    private var selectedAuthgroup: String? = null
    @Volatile
    private var setupFailure: Throwable? = null
    @Volatile
    private var refreshFailure: DnsRuleRefreshException? = null
    @Volatile
    private var routeRefreshThread: Thread? = null
    @Volatile
    private var activeResolver: VpnDnsRuleResolver? = null
    private val switchLock = Any()
    private var pendingTunSwitch: TunSwitchRequest? = null
    private val finalRoutesReady = CountDownLatch(1)
    @Volatile
    private var finalRoutesInstalled = false
    @Volatile
    private var currentResolvedRoutes: List<IpCidr> = emptyList()
    @Volatile
    private var stopping = false

    override fun onProcessAuthForm(authForm: AuthForm): Int {
        logAuthForm(authForm)
        authForm.authgroupOpt?.let { authgroup ->
            if (selectedAuthgroup == null) {
                val selection = chooseAuthgroup(authForm)
                if (selection == null) {
                    android.util.Log.e(TAG, "Auth group is required but no selectable auth group was provided")
                    return OC_FORM_RESULT_ERR
                }
                authgroup.value = selection
                selectedAuthgroup = selection
                android.util.Log.i(TAG, "Selected auth group from server form")
                return OC_FORM_RESULT_NEWGROUP
            }
        }

        var usernameSet = false
        var passwordSet = false
        for (option in authForm.opts) {
            if ((option.flags and OC_FORM_OPT_IGNORE.toLong()) != 0L ||
                option.type == OC_FORM_OPT_HIDDEN
            ) {
                continue
            }
            when (option.type) {
                OC_FORM_OPT_TEXT -> {
                    option.value = username
                    usernameSet = true
                }
                OC_FORM_OPT_PASSWORD -> {
                    option.value = password
                    passwordSet = true
                }
                OC_FORM_OPT_SELECT -> {
                    if (option == authForm.authgroupOpt && selectedAuthgroup != null) {
                        option.value = selectedAuthgroup
                    } else {
                        android.util.Log.e(TAG, "Unsupported auth option: ${option.name}/${option.type}")
                        return OC_FORM_RESULT_ERR
                    }
                }
                else -> {
                    android.util.Log.e(TAG, "Unsupported auth option: ${option.name}/${option.type}")
                    return OC_FORM_RESULT_ERR
                }
            }
        }
        android.util.Log.i(
            TAG,
            "Auth form populated: username=$usernameSet, password=$passwordSet",
        )
        return OC_FORM_RESULT_OK
    }

    override fun onProgress(level: Int, msg: String?) {
        val message = msg.orEmpty().trimEnd()
        if (message.isEmpty()) return
        when (level) {
            PRG_ERR -> android.util.Log.e(TAG, message)
            PRG_DEBUG,
            PRG_TRACE -> android.util.Log.d(TAG, message)
            else -> android.util.Log.i(TAG, message)
        }
    }

    override fun onProtectSocket(fd: Int) {
        if (!service.protect(fd)) {
            android.util.Log.e(TAG, "VpnService.protect($fd) returned false")
        }
    }

    override fun onStatsUpdate(stats: VPNStats) {
        onStats(VpnTrafficStats(rxBytes = stats.rxBytes, txBytes = stats.txBytes))
    }

    override fun onSetupTun() {
        try {
            val configuration = readTunConfiguration()
            val rules = parseRouteRules()
            if (rules.hostNames.isEmpty()) {
                installTun(configuration, rules.staticRoutes, "final static")
                markFinalRoutesInstalled(rules.staticRoutes)
            } else {
                installTun(configuration, emptyList(), "VPN DNS bootstrap")
                startRouteRefresh(configuration, rules)
            }
            setupFailure = null
        } catch (failure: Throwable) {
            setupFailure = failure
            finalRoutesReady.countDown()
            android.util.Log.e(TAG, "Android VPN interface setup failed", failure)
            cancel()
        }
    }

    fun tunSetupFailure(): Throwable? = setupFailure

    fun routeRefreshFailure(): DnsRuleRefreshException? = refreshFailure

    fun awaitFinalRoutes(): Boolean {
        return try {
            finalRoutesReady.await()
            finalRoutesInstalled && !Thread.currentThread().isInterrupted
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
    }

    fun takePendingTunSwitch(): TunSwitchRequest? = synchronized(switchLock) {
        pendingTunSwitch.also { pendingTunSwitch = null }
    }

    fun applyTunSwitch(request: TunSwitchRequest) {
        try {
            if (stopping || isCanceled()) throw InterruptedException("VPN connection was stopped")
            val configuration = activeTunConfiguration
                ?: throw DnsRuleRefreshException("VPN configuration disappeared during route refresh")
            installTun(configuration, request.routes, "refreshed final")
            currentResolvedRoutes = request.routes
            markFinalRoutesInstalled(request.routes)
        } catch (failure: Throwable) {
            val wrapped = failure.asRouteRefreshFailure()
            refreshFailure = wrapped
            request.failure = wrapped
            finalRoutesReady.countDown()
            cancel()
            throw wrapped
        } finally {
            request.completed.countDown()
        }
    }

    fun failTunSwitch(request: TunSwitchRequest, failure: DnsRuleRefreshException) {
        refreshFailure = failure
        request.failure = failure
        request.completed.countDown()
        finalRoutesReady.countDown()
        cancel()
    }

    fun stop() {
        stopping = true
        synchronized(switchLock) {
            pendingTunSwitch?.let { request ->
                request.failure = InterruptedException("VPN connection was stopped")
                request.completed.countDown()
            }
            pendingTunSwitch = null
        }
        finalRoutesReady.countDown()
        cancel()
        stopRouteRefresh()
    }

    @Volatile
    private var activeTunConfiguration: TunConfiguration? = null

    private fun readTunConfiguration(): TunConfiguration {
        val info = getIPInfo()
        val addresses = buildList {
            if (!info.addr.isNullOrBlank() && !info.netmask.isNullOrBlank()) {
                add(
                    TunAddress(
                        InetAddress.getByName(info.addr),
                        ipv4PrefixLength(info.netmask),
                    ),
                )
            }
            if (!info.addr6.isNullOrBlank()) {
                add(
                    TunAddress(
                        InetAddress.getByName(info.addr6),
                        ipv6PrefixLength(info.netmask6),
                    ),
                )
            }
        }
        require(addresses.isNotEmpty()) { "VPN did not provide an IP address" }
        val dnsServers = info.DNS.mapNotNull { value ->
            value?.takeIf(String::isNotBlank)?.let(InetAddress::getByName)
        }.distinct()
        require(dnsServers.isNotEmpty()) { "VPN did not provide a DNS server" }
        return TunConfiguration(
            addresses = addresses,
            dnsServers = dnsServers,
            mtu = info.MTU,
        ).also { activeTunConfiguration = it }
    }

    private fun parseRouteRules(): RouteRuleSet {
        val parsedRules = RouteRuleParser.parse(routingConfig.rawRules)
        val staticRoutes = parsedRules.filterNot { it.isHost }.map { rule ->
            val address = InetAddress.getByName(rule.value)
            IpCidr.from(address, rule.prefixLength ?: defaultPrefix(address))
        }.distinct()
        val hostNames = parsedRules.filter { it.isHost }
            .map { it.value }
            .distinct()
        return RouteRuleSet(staticRoutes, hostNames)
    }

    private fun installTun(
        configuration: TunConfiguration,
        resolvedRoutes: List<IpCidr>,
        stage: String,
    ) {
        val builder = service.Builder()
            .setSession("AnyConnect")
            .setBlocking(true)
        configuration.addresses.forEach { address ->
            builder.addAddress(address.address, address.prefixLength)
        }
        configuration.dnsServers.forEach(builder::addDnsServer)
        if (configuration.mtu > 0) builder.setMtu(configuration.mtu)

        val dnsRoutes = configuration.dnsServers.map(IpCidr::from)
        val plan = VpnRoutePlanner.create(
            mode = routingConfig.mode,
            families = configuration.families,
            resolvedRules = resolvedRoutes,
            dnsRoutes = dnsRoutes,
            sdkInt = Build.VERSION.SDK_INT,
        )
        check(plan.includes.isNotEmpty()) {
            "No usable ${routingConfig.mode.label} VPN routes were produced during $stage"
        }
        plan.includes.forEach { builder.addRoute(it.address(), it.prefixLength) }
        if (Build.VERSION.SDK_INT >= 33) {
            plan.excludes.forEach { addExcludedRoute(builder, it) }
        }

        val established = builder.establish() ?: error("VpnService.Builder.establish returned null")
        val nativeFd = ParcelFileDescriptor.dup(established.fileDescriptor).detachFd()
        try {
            check(setupTunFD(nativeFd) == 0) { "openconnect_setup_tun_fd failed during $stage" }
        } catch (failure: Throwable) {
            runCatching { ParcelFileDescriptor.adoptFd(nativeFd).close() }
            established.close()
            throw failure
        }
        val previous = tunFd
        val previousNative = nativeTunFd
        tunFd = established
        nativeTunFd = nativeFd
        previousNative?.let { fd ->
            runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
        }
        previous?.close()
        android.util.Log.i(
            TAG,
            "Installed $stage TUN with ${plan.includes.size} VPN routes and " +
                "${plan.excludes.size} direct exclusions for ${routingConfig.mode.label}; " +
                "resolved ${resolvedRoutes.size} rule routes",
        )
    }

    private fun startRouteRefresh(
        configuration: TunConfiguration,
        rules: RouteRuleSet,
    ) {
        check(routeRefreshThread == null) { "VPN route refresh thread already exists" }
        routeRefreshThread = kotlin.concurrent.thread(name = "anyconnect-mobile-route-refresh") {
            refreshRouteLoop(configuration, rules)
        }
    }

    private fun refreshRouteLoop(
        configuration: TunConfiguration,
        rules: RouteRuleSet,
    ) {
        val routeExpirations = mutableMapOf<IpCidr, Long>()
        try {
            while (!Thread.currentThread().isInterrupted && !isCanceled()) {
                val network = awaitVpnNetwork(configuration)
                android.util.Log.i(
                    TAG,
                    "VPN network $network is ready; resolving route hosts through Android DnsResolver",
                )
                check(Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    "VPN DNS route refresh requires Android 10 or newer"
                }
                val resolver = VpnDnsRuleResolver(network, configuration.routeDnsServer())
                activeResolver = resolver
                val snapshot = try {
                    resolver.resolve(rules.hostNames, configuration.families)
                } finally {
                    if (activeResolver === resolver) activeResolver = null
                    resolver.close()
                }
                check(snapshot.routes.isNotEmpty()) {
                    "VPN DNS returned no usable addresses for ${rules.hostNames.size} route hosts"
                }
                val nowMillis = SystemClock.elapsedRealtime()
                snapshot.routeTtlSeconds.forEach { (route, ttlSeconds) ->
                    routeExpirations[route] = DnsRefreshPolicy.expirationMillis(nowMillis, ttlSeconds)
                }
                routeExpirations.entries.removeAll { (_, expiresAtMillis) ->
                    expiresAtMillis <= nowMillis
                }
                val routes = (rules.staticRoutes + routeExpirations.keys).distinct()
                if (!finalRoutesInstalled || routes.toSet() != currentResolvedRoutes.toSet()) {
                    requestTunSwitch(routes)
                }
                sleepUntilNextRefresh(DnsRefreshPolicy.delayMillis(snapshot.minimumTtlSeconds))
            }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        } catch (failure: Throwable) {
            if (!stopping && !isCanceled()) reportRouteRefreshFailure(failure)
        }
    }

    private fun awaitVpnNetwork(configuration: TunConfiguration): Network {
        val connectivity = service.getSystemService(ConnectivityManager::class.java)
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(VPN_NETWORK_READY_TIMEOUT_SECONDS)
        while (!Thread.currentThread().isInterrupted && !isCanceled()) {
            connectivity.allNetworks.firstOrNull { network ->
                val capabilities = connectivity.getNetworkCapabilities(network)
                val properties = connectivity.getLinkProperties(network)
                capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true &&
                    properties?.linkAddresses?.any { link ->
                        configuration.addresses.any { it.address == link.address }
                    } == true
            }?.let { return it }
            if (System.nanoTime() >= deadline) {
                throw DnsRuleRefreshException(
                    "Android did not expose the bootstrap VPN network within " +
                        "$VPN_NETWORK_READY_TIMEOUT_SECONDS seconds",
                )
            }
            Thread.sleep(VPN_NETWORK_POLL_MS)
        }
        throw InterruptedException("VPN route refresh was stopped")
    }

    private fun requestTunSwitch(routes: List<IpCidr>) {
        val request = TunSwitchRequest(routes)
        synchronized(switchLock) {
            check(pendingTunSwitch == null) { "A VPN route-table switch is already pending" }
            pendingTunSwitch = request
        }
        android.util.Log.i(TAG, "Pausing OpenConnect to apply ${routes.size} VPN DNS route mappings")
        pause()
        if (!request.completed.await(TUN_SWITCH_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
            synchronized(switchLock) {
                if (pendingTunSwitch === request) pendingTunSwitch = null
            }
            throw DnsRuleRefreshException(
                "Timed out while switching the Android VPN route table",
            )
        }
        request.failure?.let { throw it }
    }

    private fun sleepUntilNextRefresh(delayMillis: Long) {
        android.util.Log.i(TAG, "VPN DNS routes will refresh in ${delayMillis / 1000} seconds")
        Thread.sleep(delayMillis)
    }

    private fun markFinalRoutesInstalled(routes: List<IpCidr>) {
        currentResolvedRoutes = routes
        if (finalRoutesInstalled) return
        synchronized(finalRoutesReady) {
            if (finalRoutesInstalled) return
            finalRoutesInstalled = true
            finalRoutesReady.countDown()
            onTunnelEstablished()
        }
    }

    private fun reportRouteRefreshFailure(failure: Throwable) {
        val wrapped = failure.asRouteRefreshFailure()
        refreshFailure = wrapped
        finalRoutesReady.countDown()
        android.util.Log.e(TAG, "VPN DNS route refresh failed; stopping tunnel", wrapped)
        cancel()
    }

    private fun Throwable.asRouteRefreshFailure(): DnsRuleRefreshException =
        this as? DnsRuleRefreshException
            ?: DnsRuleRefreshException("VPN DNS route refresh failed", this)

    private fun stopRouteRefresh() {
        activeResolver?.close()
        activeResolver = null
        routeRefreshThread?.let { thread ->
            thread.interrupt()
            if (thread !== Thread.currentThread()) {
                runCatching { thread.join(ROUTE_THREAD_JOIN_TIMEOUT_MS) }
            }
        }
        routeRefreshThread = null
    }

    @TargetApi(33)
    private fun addExcludedRoute(builder: VpnService.Builder, route: IpCidr) {
        builder.excludeRoute(IpPrefix(route.address(), route.prefixLength))
    }

    private fun defaultPrefix(address: InetAddress): Int = IpFamily.from(address).bitCount

    private fun chooseAuthgroup(authForm: AuthForm): String? {
        val authgroup = authForm.authgroupOpt ?: return null
        authgroup.value?.takeIf { it.isNotBlank() }?.let { return it }

        val selectedIndex = authForm.authgroupSelection
        if (selectedIndex in authgroup.choices.indices) {
            return authgroup.choices[selectedIndex].name
        }

        if (authgroup.choices.size == 1) {
            return authgroup.choices.first().name
        }

        return null
    }

    private fun logAuthForm(authForm: AuthForm) {
        val fields = authForm.opts.joinToString { option ->
            buildString {
                append("name=")
                append(option.name)
                append(",type=")
                append(authOptionTypeName(option.type))
                append(",flags=")
                append(option.flags)
                if (option == authForm.authgroupOpt) {
                    append(",authgroup=true")
                    append(",choices=")
                    append(option.choices.size)
                    append(",selected=")
                    append(authForm.authgroupSelection)
                }
            }
        }
        android.util.Log.i(
            TAG,
            "Auth form: method=${authForm.method}, action=${authForm.action}, " +
                "authId=${authForm.authID}, error=${authForm.error}, fields=[$fields]",
        )
    }

    private fun authOptionTypeName(type: Int): String = when (type) {
        OC_FORM_OPT_TEXT -> "TEXT"
        OC_FORM_OPT_PASSWORD -> "PASSWORD"
        OC_FORM_OPT_SELECT -> "SELECT"
        OC_FORM_OPT_HIDDEN -> "HIDDEN"
        OC_FORM_OPT_TOKEN -> "TOKEN"
        else -> "UNKNOWN($type)"
    }

    override fun close() {
        stopping = true
        stopRouteRefresh()
        finalRoutesReady.countDown()
        nativeTunFd?.let { fd ->
            runCatching { ParcelFileDescriptor.adoptFd(fd).close() }
        }
        nativeTunFd = null
        tunFd?.close()
        tunFd = null
        destroy()
    }

    private fun ipv4PrefixLength(mask: String): Int {
        val address = InetAddress.getByName(mask)
        require(address is Inet4Address) { "invalid IPv4 netmask: $mask" }
        return address.address.sumOf { byte -> Integer.bitCount(byte.toInt() and 0xff) }
    }

    private fun ipv6PrefixLength(mask: String?): Int {
        if (mask.isNullOrBlank()) return 64
        mask.toIntOrNull()?.let { return it.coerceIn(0, 128) }
        val address = InetAddress.getByName(mask)
        return address.address.sumOf { byte -> Integer.bitCount(byte.toInt() and 0xff) }
    }

    companion object {
        private const val TAG = "AnyConnectOpenConnect"
        private const val VPN_NETWORK_READY_TIMEOUT_SECONDS = 15L
        private const val VPN_NETWORK_POLL_MS = 100L
        private const val TUN_SWITCH_TIMEOUT_SECONDS = 30L
        private const val ROUTE_THREAD_JOIN_TIMEOUT_MS = 2_000L
    }
}
