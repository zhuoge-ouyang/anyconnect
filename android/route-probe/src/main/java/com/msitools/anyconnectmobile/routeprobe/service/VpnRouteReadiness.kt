package com.msitools.anyconnectmobile.routeprobe.service

import android.annotation.TargetApi
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.RouteInfo
import android.net.VpnService
import android.os.Build
import android.os.SystemClock
import com.msitools.anyconnectmobile.routeprobe.net.Cidr
import com.msitools.anyconnectmobile.routeprobe.net.IpFamily
import com.msitools.anyconnectmobile.routeprobe.net.RoutePlan
import java.math.BigInteger
import java.net.InetAddress

internal data class VpnRouteIdentity(
    val networkHandle: Long,
    val interfaceName: String,
) {
    init {
        require(interfaceName.isNotBlank()) { "interfaceName must not be blank" }
    }
}

internal data class VpnRouteSnapshot(
    val vpnNetworkCount: Int,
    val ownedVpnNetworkCount: Int,
    val ownedRouteCount: Int,
    val matchedTargets: Set<String>,
    val activeNetworkIsOwnedVpn: Boolean,
    val activeOwnedNetworkHandle: Long?,
    val activeOwnedInterfaceName: String?,
    val expectedPlanRouteCount: Int,
    val matchedPlanRouteCount: Int,
    val missingPlanRouteCount: Int,
    val missingPlanRouteSample: List<String> = emptyList(),
) {
    init {
        require(vpnNetworkCount >= 0) { "vpnNetworkCount must not be negative" }
        require(ownedVpnNetworkCount >= 0) { "ownedVpnNetworkCount must not be negative" }
        require(ownedRouteCount >= 0) { "ownedRouteCount must not be negative" }
        require(ownedVpnNetworkCount <= vpnNetworkCount) {
            "ownedVpnNetworkCount must not exceed vpnNetworkCount"
        }
        require(activeNetworkIsOwnedVpn == (activeOwnedNetworkHandle != null)) {
            "active owned VPN state and network handle must agree"
        }
        require(expectedPlanRouteCount > 0) { "expectedPlanRouteCount must be positive" }
        require(matchedPlanRouteCount in 0..expectedPlanRouteCount) {
            "matchedPlanRouteCount is outside the expected route count"
        }
        require(missingPlanRouteCount == expectedPlanRouteCount - matchedPlanRouteCount) {
            "missingPlanRouteCount must equal expected minus matched routes"
        }
        require(missingPlanRouteSample.size <= missingPlanRouteCount) {
            "missingPlanRouteSample cannot exceed missingPlanRouteCount"
        }
    }

    val activeOwnedVpnIdentity: VpnRouteIdentity?
        get() {
            val handle = activeOwnedNetworkHandle ?: return null
            val interfaceName = activeOwnedInterfaceName?.takeIf(String::isNotBlank) ?: return null
            return VpnRouteIdentity(handle, interfaceName)
        }
}

internal enum class ObservedRouteKind { UNICAST, THROW }

internal data class ObservedVpnRoute(
    val cidr: Cidr,
    val kind: ObservedRouteKind,
)

internal data class PlanRouteCoverage(
    val expectedCount: Int,
    val matchedCount: Int,
    val missingCount: Int,
    val missingSample: List<String>,
)

internal fun VpnRouteSnapshot.readinessBaseline(
    expectedTargets: Collection<InetAddress>,
): ProbeReadinessBaseline {
    val expected = expectedTargets.mapTo(linkedSetOf(), InetAddress::numericAddress)
    val identity = activeOwnedVpnIdentity
    val alreadyMatchesNewPlan = identity != null &&
        missingPlanRouteCount == 0 &&
        matchedPlanRouteCount == expectedPlanRouteCount &&
        matchedTargets.containsAll(expected)
    return ProbeReadinessBaseline(
        activeOwnedVpnIdentity = identity,
        requiresFreshVpnIdentity = alreadyMatchesNewPlan,
    )
}

internal class PlanRouteCoverageEvaluator(plan: RoutePlan) {
    private val includes = plan.includes.associateWith { cidr -> "include:$cidr" }
    private val excludes = plan.excludes.associateWith { cidr -> "exclude:$cidr" }
    val expectedCount: Int = includes.size + excludes.size

    init {
        check(expectedCount == plan.builderEntryCount) {
            "Route plan contains duplicate Builder entries"
        }
    }

    fun evaluate(observed: Collection<ObservedVpnRoute>): PlanRouteCoverage {
        val unicast = observed.asSequence()
            .filter { it.kind == ObservedRouteKind.UNICAST }
            .mapTo(hashSetOf(), ObservedVpnRoute::cidr)
        val throwRoutes = observed.asSequence()
            .filter { it.kind == ObservedRouteKind.THROW }
            .mapTo(hashSetOf(), ObservedVpnRoute::cidr)
        var matched = 0
        val missing = ArrayList<String>(MISSING_ROUTE_SAMPLE_LIMIT)
        includes.forEach { (cidr, label) ->
            if (cidr in unicast) {
                matched++
            } else if (missing.size < MISSING_ROUTE_SAMPLE_LIMIT) {
                missing += label
            }
        }
        excludes.forEach { (cidr, label) ->
            if (cidr in throwRoutes) {
                matched++
            } else if (missing.size < MISSING_ROUTE_SAMPLE_LIMIT) {
                missing += label
            }
        }
        return PlanRouteCoverage(
            expectedCount = expectedCount,
            matchedCount = matched,
            missingCount = expectedCount - matched,
            missingSample = missing,
        )
    }

    private companion object {
        const val MISSING_ROUTE_SAMPLE_LIMIT = 8
    }
}

internal class VpnRouteReadinessException(
    message: String,
    val lastSnapshot: VpnRouteSnapshot? = null,
) : Exception(message)

internal class VpnRouteReadinessWaiter(
    private val snapshot: () -> VpnRouteSnapshot,
    private val elapsedRealtime: () -> Long = SystemClock::elapsedRealtime,
    private val pause: (Long) -> Unit = { Thread.sleep(it) },
    private val timeoutMs: Long = DEFAULT_TIMEOUT_MS,
    private val pollIntervalMs: Long = DEFAULT_POLL_INTERVAL_MS,
) {
    init {
        require(timeoutMs > 0) { "timeoutMs must be positive" }
        require(pollIntervalMs > 0) { "pollIntervalMs must be positive" }
    }

    fun await(
        expectedTargets: Collection<InetAddress>,
        previousActiveOwnedVpnIdentity: VpnRouteIdentity? = null,
    ): VpnRouteSnapshot {
        val expected = expectedTargets.mapTo(linkedSetOf(), InetAddress::numericAddress)
        val startedAt = elapsedRealtime()
        while (true) {
            val observed = snapshot()
            if (observed.isReady(expected, previousActiveOwnedVpnIdentity)) return observed

            val elapsedMs = (elapsedRealtime() - startedAt).coerceAtLeast(0L)
            if (elapsedMs >= timeoutMs) {
                throw timeout(observed, expected, previousActiveOwnedVpnIdentity)
            }
            pause(minOf(pollIntervalMs, timeoutMs - elapsedMs))
        }
    }

    private fun VpnRouteSnapshot.isReady(
        expectedTargets: Set<String>,
        previousActiveOwnedVpnIdentity: VpnRouteIdentity?,
    ): Boolean =
        activeNetworkIsOwnedVpn &&
            ownedVpnNetworkCount > 0 &&
            activeOwnedVpnIdentity != null &&
            activeOwnedVpnIdentity != previousActiveOwnedVpnIdentity &&
            missingPlanRouteCount == 0 &&
            matchedPlanRouteCount == expectedPlanRouteCount &&
            matchedTargets.containsAll(expectedTargets)

    private fun timeout(
        observed: VpnRouteSnapshot,
        expectedTargets: Set<String>,
        previousActiveOwnedVpnIdentity: VpnRouteIdentity?,
    ): VpnRouteReadinessException {
        val missing = (expectedTargets - observed.matchedTargets).sorted()
        val message = buildString {
            append("VPN route readiness timed out after ").append(timeoutMs).append("ms: ")
            append("vpnNetworkCount=").append(observed.vpnNetworkCount).append(", ")
            append("ownedVpnNetworkCount=").append(observed.ownedVpnNetworkCount).append(", ")
            append("ownedRouteCount=").append(observed.ownedRouteCount).append(", ")
            append("activeNetworkIsOwnedVpn=")
                .append(observed.activeNetworkIsOwnedVpn)
                .append(", ")
            append("activeOwnedNetworkHandle=")
                .append(observed.activeOwnedNetworkHandle)
                .append(", ")
            append("activeOwnedInterfaceName=")
                .append(observed.activeOwnedInterfaceName)
                .append(", ")
            append("previousActiveOwnedVpnIdentity=")
                .append(previousActiveOwnedVpnIdentity)
                .append(", ")
            append("expectedPlanRouteCount=").append(observed.expectedPlanRouteCount).append(", ")
            append("matchedPlanRouteCount=").append(observed.matchedPlanRouteCount).append(", ")
            append("missingPlanRouteCount=").append(observed.missingPlanRouteCount).append(", ")
            append("missingPlanRouteSample=").append(observed.missingPlanRouteSample).append(", ")
            append("matchedTargets=").append(observed.matchedTargets.sorted()).append(", ")
            append("missingTargets=").append(missing)
        }
        return VpnRouteReadinessException(message, observed)
    }

    private companion object {
        const val DEFAULT_TIMEOUT_MS = 5_000L
        const val DEFAULT_POLL_INTERVAL_MS = 100L
    }
}

internal class AndroidVpnRouteSnapshotProvider(service: VpnService) {
    private val connectivityManager = requireNotNull(
        service.getSystemService(ConnectivityManager::class.java),
    ) { "ConnectivityManager is unavailable" }
    private var cachedPlan: RoutePlan? = null
    private var cachedCoverageEvaluator: PlanRouteCoverageEvaluator? = null

    @Suppress("DEPRECATION")
    fun snapshot(plan: RoutePlan): VpnRouteSnapshot {
        val requiredTunAddresses = plan.scenario.families.map(::probeTunAddress)
        val vpnNetworks = connectivityManager.allNetworks.mapNotNull { network ->
            val capabilities = connectivityManager.getNetworkCapabilities(network)
                ?: return@mapNotNull null
            if (!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) {
                return@mapNotNull null
            }
            ObservedVpnNetwork(
                network = network,
                linkProperties = connectivityManager.getLinkProperties(network),
            )
        }
        val ownedVpnNetworks = vpnNetworks.filter { observed ->
            observed.linkProperties?.hasEveryTunAddress(requiredTunAddresses) == true
        }
        val activeNetwork = connectivityManager.activeNetwork
        val activeOwnedVpn = ownedVpnNetworks.firstOrNull { it.network == activeNetwork }
        val activeRoutes = activeOwnedVpn?.linkProperties?.routes.orEmpty()
        val coverage = plan.coverageEvaluator().evaluate(
            activeRoutes.mapNotNull { it.observedVpnRoute() },
        )
        val matchedTargets = plan.expectedCaptured
            .filter { activeRoutes.effectivelyCaptures(it) }
            .mapTo(linkedSetOf(), InetAddress::numericAddress)

        return VpnRouteSnapshot(
            vpnNetworkCount = vpnNetworks.size,
            ownedVpnNetworkCount = ownedVpnNetworks.size,
            ownedRouteCount = activeRoutes.size,
            matchedTargets = matchedTargets,
            activeNetworkIsOwnedVpn = activeOwnedVpn != null,
            activeOwnedNetworkHandle = activeOwnedVpn?.network?.networkHandle,
            activeOwnedInterfaceName = activeOwnedVpn?.linkProperties?.interfaceName,
            expectedPlanRouteCount = coverage.expectedCount,
            matchedPlanRouteCount = coverage.matchedCount,
            missingPlanRouteCount = coverage.missingCount,
            missingPlanRouteSample = coverage.missingSample,
        )
    }

    private fun RoutePlan.coverageEvaluator(): PlanRouteCoverageEvaluator {
        if (cachedPlan === this) return checkNotNull(cachedCoverageEvaluator)
        val evaluator = PlanRouteCoverageEvaluator(this)
        cachedPlan = this
        cachedCoverageEvaluator = evaluator
        return evaluator
    }

    private fun RouteInfo.observedVpnRoute(): ObservedVpnRoute? {
        val kind = when {
            Build.VERSION.SDK_INT < 33 -> ObservedRouteKind.UNICAST
            type == RouteInfo.RTN_UNICAST -> ObservedRouteKind.UNICAST
            type == RouteInfo.RTN_THROW -> ObservedRouteKind.THROW
            else -> return null
        }
        return ObservedVpnRoute(destination.toCidr(), kind)
    }

    private fun LinkProperties.hasEveryTunAddress(required: List<RequiredTunAddress>): Boolean =
        required.all { expected ->
            linkAddresses.any { observed ->
                observed.prefixLength == expected.prefixLength &&
                    observed.address.address.contentEquals(expected.address.address)
            }
        }

    private fun List<RouteInfo>.effectivelyCaptures(target: InetAddress): Boolean {
        val matching = filter { it.matches(target) }
        val longestPrefix = matching.maxOfOrNull { it.destination.prefixLength } ?: return false
        return matching.any { route ->
            route.destination.prefixLength == longestPrefix && route.isUnicast()
        }
    }

    private fun RouteInfo.isUnicast(): Boolean =
        Build.VERSION.SDK_INT < 33 || type == RouteInfo.RTN_UNICAST

    private fun android.net.IpPrefix.toCidr(): Cidr {
        val family = if (address.address.size == IpFamily.IPV4.byteCount) {
            IpFamily.IPV4
        } else {
            IpFamily.IPV6
        }
        return Cidr(
            network = BigInteger(1, address.address),
            prefixLength = prefixLength,
            family = family,
        )
    }

    private fun probeTunAddress(family: IpFamily): RequiredTunAddress = when (family) {
        IpFamily.IPV4 -> IPV4_TUN_ADDRESS
        IpFamily.IPV6 -> IPV6_TUN_ADDRESS
    }

    private data class ObservedVpnNetwork(
        val network: Network,
        val linkProperties: LinkProperties?,
    )

    private data class RequiredTunAddress(
        val address: InetAddress,
        val prefixLength: Int,
    )

    private companion object {
        val IPV4_TUN_ADDRESS = RequiredTunAddress(
            address = Cidr.parse("10.252.0.2/32").address(),
            prefixLength = 32,
        )
        val IPV6_TUN_ADDRESS = RequiredTunAddress(
            address = Cidr.parse("fd00:252::2/128").address(),
            prefixLength = 128,
        )
    }
}

private fun InetAddress.numericAddress(): String = requireNotNull(hostAddress)
