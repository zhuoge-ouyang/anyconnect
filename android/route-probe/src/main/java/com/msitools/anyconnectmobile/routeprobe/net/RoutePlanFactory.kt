package com.msitools.anyconnectmobile.routeprobe.net

import java.math.BigInteger
import java.net.InetAddress

object RoutePlanFactory {
    private val systemDirectV4 = listOf(
        "0.0.0.0/8",
        "10.0.0.0/8",
        "100.64.0.0/10",
        "127.0.0.0/8",
        "169.254.0.0/16",
        "172.16.0.0/12",
        "192.0.0.0/24",
        "192.0.2.0/24",
        "192.168.0.0/16",
        "198.18.0.0/15",
        "198.51.100.0/24",
        "203.0.113.0/24",
        "224.0.0.0/4",
        "240.0.0.0/4",
    ).map(Cidr::parse)

    private val systemDirectV6 = listOf(
        "::/128",
        "::1/128",
        "fc00::/7",
        "fe80::/10",
        "ff00::/8",
        "2001:db8::/32",
    ).map(Cidr::parse)

    private val foreignProbeRoutes = listOf(
        Cidr.parse("8.8.8.8/32"),
        Cidr.parse("2001:4860:4860::8888/128"),
    )

    private val domesticProbeRoutes = listOf(
        Cidr.parse("114.114.114.114/32"),
        Cidr.parse("2400:3200::1/128"),
    )

    private val allPathProbeRoutes = foreignProbeRoutes + domesticProbeRoutes

    fun create(
        scenario: ProbeScenario,
        sdkInt: Int,
        chinaCidrs: List<Cidr>,
    ): RoutePlan {
        require(sdkInt >= 26) { "Gate 0 requires Android API 26 or newer: $sdkInt" }
        val families = scenario.families.sorted()
        val chinaByFamily = families.associateWith { family ->
            CidrMath.collapse(chinaCidrs.filter { it.family == family })
        }
        val routeShape = when (scenario.mode) {
            ProbeMode.DOMESTIC_DIRECT -> createDomesticShape(
                families = families,
                chinaByFamily = chinaByFamily,
            )

            ProbeMode.FOREIGN_DIRECT -> createForeignShape(
                families = families,
                chinaByFamily = chinaByFamily,
            )
        }
        val expectedCaptured = routeShape.expectedCaptured
            .map(Cidr::address)
        val expectedBypass = routeShape.expectedBypass
            .map(Cidr::address)
        val plan = RoutePlan(
            scenario = scenario,
            strategy = routeShape.strategy,
            includes = routeShape.includes,
            excludes = routeShape.excludes,
            directSet = routeShape.directSet,
            syntheticMappings = routeShape.syntheticMappings,
            expectedCaptured = expectedCaptured,
            expectedBypass = expectedBypass,
        )

        val effectiveVpn = materializeVpnRanges(plan)
        require(expectedCaptured.all { effectiveVpn.containsAddress(it) }) {
            "A fixed captured target is outside the effective VPN set for $scenario"
        }
        require(expectedBypass.all { plan.directSet.containsAddress(it) }) {
            "A fixed direct DNS target is outside directSet for $scenario"
        }
        require(expectedBypass.none { effectiveVpn.containsAddress(it) }) {
            "A fixed direct DNS target is inside the effective VPN set for $scenario"
        }
        return plan
    }

    fun materializeVpnRanges(plan: RoutePlan): List<Cidr> = when (plan.strategy) {
        RouteStrategy.INCLUDES_ONLY -> collapseByFamily(plan.includes, plan.scenario.families)
        RouteStrategy.DEFAULT_WITH_EXCLUDES -> {
            val expectedDefaults = plan.scenario.families.map { family ->
                Cidr(BigInteger.ZERO, 0, family)
            }.toSet()
            require(plan.includes.toSet() == expectedDefaults) {
                "Default-with-excludes plan must contain one default per requested family"
            }
            plan.scenario.families.sorted().flatMap { family ->
                CidrMath.complement(plan.excludes.filter { it.family == family }, family)
            }
        }
    }

    private fun createDomesticShape(
        families: List<IpFamily>,
        chinaByFamily: Map<IpFamily, List<Cidr>>,
    ): RouteShape {
        val capturedRoutes = foreignProbeRoutes.filter { it.family in families }
        val bypassRoutes = domesticProbeRoutes.filter { it.family in families }
        val syntheticMappings = families.flatMap { family ->
            generateSyntheticMappings(
                family = family,
                candidateRanges = CidrMath.complement(chinaByFamily.getValue(family), family),
                label = "non-CN whitelist",
            )
        }
        return createWhitelistShape(
            families = families,
            syntheticMappings = syntheticMappings,
            capturedRoutes = capturedRoutes,
            bypassRoutes = bypassRoutes,
            label = "Domestic-direct",
        )
    }

    private fun createForeignShape(
        families: List<IpFamily>,
        chinaByFamily: Map<IpFamily, List<Cidr>>,
    ): RouteShape {
        val capturedRoutes = domesticProbeRoutes.filter { it.family in families }
        val bypassRoutes = foreignProbeRoutes.filter { it.family in families }
        val syntheticMappings = families.flatMap { family ->
            generateSyntheticMappings(
                family = family,
                candidateRanges = chinaByFamily.getValue(family),
                label = "CN whitelist",
            )
        }
        return createWhitelistShape(
            families = families,
            syntheticMappings = syntheticMappings,
            capturedRoutes = capturedRoutes,
            bypassRoutes = bypassRoutes,
            label = "Foreign-direct",
        )
    }

    private fun createWhitelistShape(
        families: List<IpFamily>,
        syntheticMappings: List<Cidr>,
        capturedRoutes: List<Cidr>,
        bypassRoutes: List<Cidr>,
        label: String,
    ): RouteShape {
        val protectedRanges = families.flatMap(::systemDirect)
        require(capturedRoutes.none { route -> protectedRanges.any(route::overlaps) }) {
            "$label captured target conflicts with a system-direct protection range"
        }
        require(syntheticMappings.none { route -> protectedRanges.any(route::overlaps) }) {
            "$label synthetic VPN mapping conflicts with a system-direct protection range"
        }

        val includeInputs = syntheticMappings + capturedRoutes
        val includes = collapseByFamily(includeInputs, families)
        require(includes.size == includeInputs.size && includes.all { it.prefixLength == it.family.bitCount }) {
            "$label mappings must remain distinct host routes"
        }
        return RouteShape(
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = includes,
            excludes = emptyList(),
            directSet = collapseByFamily(families.flatMap(::systemDirect) + bypassRoutes, families),
            syntheticMappings = syntheticMappings.toList(),
            expectedCaptured = capturedRoutes,
            expectedBypass = bypassRoutes,
        )
    }

    private fun generateSyntheticMappings(
        family: IpFamily,
        candidateRanges: List<Cidr>,
        label: String,
    ): List<Cidr> {
        val sourceBlocks = CidrMath.collapse(candidateRanges.filter { it.family == family })
        val system = systemDirect(family)
        val pathTargets = allPathProbeRoutes.filter { it.family == family }
        val mappings = mutableListOf<Cidr>()

        for (block in sourceBlocks) {
            val candidate = firstEligibleAddress(
                block = block,
                minimum = mappings.lastOrNull()?.network?.plus(BigInteger.valueOf(2)),
                system = system,
                pathTargets = pathTargets,
            ) ?: continue
            mappings += Cidr(candidate, family.bitCount, family)
            if (mappings.size == MAX_ACTIVE_RULE_IPS_PER_FAMILY) break
        }

        require(mappings.size == MAX_ACTIVE_RULE_IPS_PER_FAMILY) {
            "${family.name} requires $MAX_ACTIVE_RULE_IPS_PER_FAMILY distinct eligible " +
                "$label blocks, found ${mappings.size}"
        }
        return mappings
    }

    private fun firstEligibleAddress(
        block: Cidr,
        minimum: BigInteger?,
        system: List<Cidr>,
        pathTargets: List<Cidr>,
    ): BigInteger? {
        var candidate = if (minimum != null && minimum > block.network) minimum else block.network
        while (candidate <= block.end) {
            val systemBlock = system.firstOrNull { it.contains(candidate) }
            if (systemBlock != null) {
                candidate = systemBlock.end + BigInteger.ONE
                continue
            }
            val nearTarget = pathTargets.firstOrNull { target ->
                (candidate - target.network).abs() <= BigInteger.ONE
            }
            if (nearTarget != null) {
                candidate = nearTarget.network + BigInteger.valueOf(2)
                continue
            }
            return candidate
        }
        return null
    }

    private fun systemDirect(family: IpFamily): List<Cidr> = when (family) {
        IpFamily.IPV4 -> systemDirectV4
        IpFamily.IPV6 -> systemDirectV6
    }

    private fun collapseByFamily(
        cidrs: List<Cidr>,
        families: Collection<IpFamily>,
    ): List<Cidr> = families.sorted().flatMap { family ->
        CidrMath.collapse(cidrs.filter { it.family == family })
    }

    private fun List<Cidr>.containsAddress(address: InetAddress): Boolean {
        val family = if (address.address.size == IpFamily.IPV4.byteCount) {
            IpFamily.IPV4
        } else {
            IpFamily.IPV6
        }
        val value = BigInteger(1, address.address)
        return any { it.family == family && it.contains(value) }
    }

    private data class RouteShape(
        val strategy: RouteStrategy,
        val includes: List<Cidr>,
        val excludes: List<Cidr>,
        val directSet: List<Cidr>,
        val syntheticMappings: List<Cidr>,
        val expectedCaptured: List<Cidr>,
        val expectedBypass: List<Cidr>,
    )
}
