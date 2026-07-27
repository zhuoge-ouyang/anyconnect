package com.msitools.anyconnectmobile.routeprobe.service

import com.msitools.anyconnectmobile.routeprobe.net.Cidr
import com.msitools.anyconnectmobile.routeprobe.net.ProbeScenario
import com.msitools.anyconnectmobile.routeprobe.net.RoutePlan
import com.msitools.anyconnectmobile.routeprobe.net.RouteStrategy
import java.net.InetAddress
import java.util.ArrayDeque
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnRouteReadinessTest {
    @Test
    fun waitsUntilTheOwnedVpnReportsEveryExpectedRoute() {
        val first = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 0,
            ownedRouteCount = 0,
            matchedTargets = emptySet(),
            activeNetworkIsOwnedVpn = false,
            activeOwnedNetworkHandle = null,
            activeOwnedInterfaceName = null,
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 0,
            missingPlanRouteCount = 1_025,
            missingPlanRouteSample = listOf("include:8.8.8.8/32"),
        )
        val second = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 1,
            ownedRouteCount = 1_025,
            matchedTargets = setOf("8.8.8.8"),
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 22L,
            activeOwnedInterfaceName = "tun1",
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 1_025,
            missingPlanRouteCount = 0,
        )
        val snapshots = ArrayDeque(listOf(first, second))
        val pauses = mutableListOf<Long>()
        val waiter = VpnRouteReadinessWaiter(
            snapshot = { snapshots.removeFirst() },
            elapsedRealtime = sequence(0L, 25L),
            pause = { pauses += it },
            timeoutMs = 5_000,
            pollIntervalMs = 25,
        )

        val ready = waiter.await(listOf(InetAddress.getByName("8.8.8.8")))

        assertEquals(second, ready)
        assertEquals(listOf(25L), pauses)
    }

    @Test
    fun timeoutReportsTheLastObservedVpnAndRouteCounts() {
        val snapshot = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 1,
            ownedRouteCount = 1_024,
            matchedTargets = emptySet(),
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 22L,
            activeOwnedInterfaceName = "tun1",
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 1_024,
            missingPlanRouteCount = 1,
            missingPlanRouteSample = listOf("include:49.128.8.0/32"),
        )
        val waiter = VpnRouteReadinessWaiter(
            snapshot = { snapshot },
            elapsedRealtime = sequence(0L, 5_001L),
            pause = {},
            timeoutMs = 5_000,
            pollIntervalMs = 25,
        )

        val failure = assertThrows(VpnRouteReadinessException::class.java) {
            waiter.await(listOf(InetAddress.getByName("8.8.8.8")))
        }

        assertTrue(failure.message.orEmpty().contains("ownedRouteCount=1024"))
        assertTrue(failure.message.orEmpty().contains("missingTargets=[8.8.8.8]"))
    }

    @Test
    fun doesNotReportReadyUntilTheOwnedVpnBecomesTheActiveNetwork() {
        val inactive = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 1,
            ownedRouteCount = 1_025,
            matchedTargets = setOf("8.8.8.8"),
            activeNetworkIsOwnedVpn = false,
            activeOwnedNetworkHandle = null,
            activeOwnedInterfaceName = null,
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 1_025,
            missingPlanRouteCount = 0,
        )
        val active = inactive.copy(
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 22L,
            activeOwnedInterfaceName = "tun1",
        )
        val snapshots = ArrayDeque(listOf(inactive, active))
        val pauses = mutableListOf<Long>()
        val waiter = VpnRouteReadinessWaiter(
            snapshot = { snapshots.removeFirst() },
            elapsedRealtime = sequence(0L, 25L),
            pause = { pauses += it },
            timeoutMs = 5_000,
            pollIntervalMs = 25,
        )

        val ready = waiter.await(listOf(InetAddress.getByName("8.8.8.8")))

        assertEquals(active, ready)
        assertEquals(listOf(25L), pauses)
    }

    @Test
    fun waitsForAFreshTunInterfaceWhenAndroidReusesTheVpnNetworkAgent() {
        val previous = VpnRouteSnapshot(
            vpnNetworkCount = 2,
            ownedVpnNetworkCount = 2,
            ownedRouteCount = 2_050,
            matchedTargets = setOf("8.8.8.8"),
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 11L,
            activeOwnedInterfaceName = "tun0",
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 1_025,
            missingPlanRouteCount = 0,
        )
        val replacement = previous.copy(activeOwnedInterfaceName = "tun1")
        val snapshots = ArrayDeque(listOf(previous, replacement))
        val pauses = mutableListOf<Long>()
        val waiter = VpnRouteReadinessWaiter(
            snapshot = { snapshots.removeFirst() },
            elapsedRealtime = sequence(0L, 25L),
            pause = { pauses += it },
            timeoutMs = 5_000,
            pollIntervalMs = 25,
        )

        val ready = waiter.await(
            expectedTargets = listOf(InetAddress.getByName("8.8.8.8")),
            previousActiveOwnedVpnIdentity = VpnRouteIdentity(11L, "tun0"),
        )

        assertEquals(replacement, ready)
        assertEquals(listOf(25L), pauses)
    }

    @Test
    fun sentinelRouteAloneCannotMakeAnIncompletePlanReady() {
        val incomplete = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 1,
            ownedRouteCount = 40,
            matchedTargets = setOf("8.8.8.8"),
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 22L,
            activeOwnedInterfaceName = "tun1",
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 40,
            missingPlanRouteCount = 985,
            missingPlanRouteSample = listOf("include:49.128.8.0/32"),
        )
        val complete = incomplete.copy(
            ownedRouteCount = 1_025,
            matchedPlanRouteCount = 1_025,
            missingPlanRouteCount = 0,
            missingPlanRouteSample = emptyList(),
        )
        val snapshots = ArrayDeque(listOf(incomplete, complete))
        val pauses = mutableListOf<Long>()
        val waiter = VpnRouteReadinessWaiter(
            snapshot = { snapshots.removeFirst() },
            elapsedRealtime = sequence(0L, 25L),
            pause = { pauses += it },
            timeoutMs = 5_000,
            pollIntervalMs = 25,
        )

        val ready = waiter.await(listOf(InetAddress.getByName("8.8.8.8")))

        assertEquals(complete, ready)
        assertEquals(listOf(25L), pauses)
    }

    @Test
    fun planCoverageRequiresEveryExactIncludeRatherThanOnlyTheSentinel() {
        val includes = listOf(
            Cidr.parse("8.8.8.8/32"),
            Cidr.parse("49.128.1.0/32"),
            Cidr.parse("49.128.8.0/32"),
        )
        val evaluator = PlanRouteCoverageEvaluator(
            routePlan(RouteStrategy.INCLUDES_ONLY, includes, emptyList()),
        )

        val incomplete = evaluator.evaluate(
            listOf(ObservedVpnRoute(includes.first(), ObservedRouteKind.UNICAST)),
        )
        val complete = evaluator.evaluate(
            includes.map { ObservedVpnRoute(it, ObservedRouteKind.UNICAST) },
        )

        assertEquals(3, incomplete.expectedCount)
        assertEquals(1, incomplete.matchedCount)
        assertEquals(2, incomplete.missingCount)
        assertTrue(incomplete.missingSample.contains("include:49.128.8.0/32"))
        assertEquals(3, complete.matchedCount)
        assertEquals(0, complete.missingCount)
    }

    @Test
    fun excludedRouteOnlyMatchesAThrowRoute() {
        val defaultRoute = Cidr.parse("0.0.0.0/0")
        val excluded = Cidr.parse("114.114.114.114/32")
        val evaluator = PlanRouteCoverageEvaluator(
            routePlan(
                strategy = RouteStrategy.DEFAULT_WITH_EXCLUDES,
                includes = listOf(defaultRoute),
                excludes = listOf(excluded),
            ),
        )

        val wrongKind = evaluator.evaluate(
            listOf(
                ObservedVpnRoute(defaultRoute, ObservedRouteKind.UNICAST),
                ObservedVpnRoute(excluded, ObservedRouteKind.UNICAST),
            ),
        )
        val complete = evaluator.evaluate(
            listOf(
                ObservedVpnRoute(defaultRoute, ObservedRouteKind.UNICAST),
                ObservedVpnRoute(excluded, ObservedRouteKind.THROW),
            ),
        )

        assertEquals(1, wrongKind.matchedCount)
        assertEquals(listOf("exclude:114.114.114.114/32"), wrongKind.missingSample)
        assertEquals(2, complete.matchedCount)
        assertEquals(0, complete.missingCount)
    }

    @Test
    fun baselineOnlyRequiresFreshIdentityWhenOldVpnAlreadyMatchesTheNewPlan() {
        val incomplete = VpnRouteSnapshot(
            vpnNetworkCount = 1,
            ownedVpnNetworkCount = 1,
            ownedRouteCount = 40,
            matchedTargets = setOf("8.8.8.8"),
            activeNetworkIsOwnedVpn = true,
            activeOwnedNetworkHandle = 11L,
            activeOwnedInterfaceName = "tun0",
            expectedPlanRouteCount = 1_025,
            matchedPlanRouteCount = 40,
            missingPlanRouteCount = 985,
            missingPlanRouteSample = listOf("include:49.128.8.0/32"),
        )
        val complete = incomplete.copy(
            ownedRouteCount = 1_025,
            matchedPlanRouteCount = 1_025,
            missingPlanRouteCount = 0,
            missingPlanRouteSample = emptyList(),
        )

        val incompleteBaseline = incomplete.readinessBaseline(
            listOf(InetAddress.getByName("8.8.8.8")),
        )
        val completeBaseline = complete.readinessBaseline(
            listOf(InetAddress.getByName("8.8.8.8")),
        )

        assertEquals(VpnRouteIdentity(11L, "tun0"), incompleteBaseline.activeOwnedVpnIdentity)
        assertTrue(!incompleteBaseline.requiresFreshVpnIdentity)
        assertTrue(completeBaseline.requiresFreshVpnIdentity)
    }

    private fun routePlan(
        strategy: RouteStrategy,
        includes: List<Cidr>,
        excludes: List<Cidr>,
    ): RoutePlan = RoutePlan(
        scenario = ProbeScenario.DOMESTIC_DIRECT_IPV4,
        strategy = strategy,
        includes = includes,
        excludes = excludes,
        directSet = excludes,
        syntheticMappings = emptyList(),
        expectedCaptured = listOf(InetAddress.getByName("8.8.8.8")),
        expectedBypass = listOf(InetAddress.getByName("114.114.114.114")),
    )

    private fun sequence(vararg values: Long): () -> Long {
        val remaining = ArrayDeque(values.toList())
        return { remaining.removeFirst() }
    }
}
