package com.msitools.anyconnectmobile.routeprobe.net

import java.math.BigInteger
import java.net.InetAddress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class RoutePlanFactoryTest {
    private val capacityChina: List<Cidr> by lazy {
        val ipv4Base = Cidr.parse("11.0.0.0/32").network
        val ipv6Base = Cidr.parse("3000::/128").network
        buildList {
            repeat(2_048) { index ->
                add(
                    Cidr(
                        ipv4Base + BigInteger.valueOf(index.toLong() * 2),
                        IpFamily.IPV4.bitCount,
                        IpFamily.IPV4,
                    ),
                )
                add(
                    Cidr(
                        ipv6Base + BigInteger.valueOf(index.toLong() * 2),
                        IpFamily.IPV6.bitCount,
                        IpFamily.IPV6,
                    ),
                )
            }
            add(Cidr.parse("114.114.114.114/32"))
            add(Cidr.parse("2400:3200::/32"))
        }
    }

    @Test
    fun definesExactlySixPrimaryScenarios() {
        assertEquals(6, ProbeScenario.entries.size)
        assertEquals(
            setOf(IpFamily.IPV4, IpFamily.IPV6),
            ProbeScenario.DOMESTIC_DIRECT_DUAL.families,
        )
    }

    @Test
    fun insufficientFixtureFailsClosedInsteadOfReducingCapacity() {
        val insufficient = listOf(
            Cidr.parse("1.0.1.0/24"),
            Cidr.parse("114.114.114.114/32"),
            Cidr.parse("2400:3200::/32"),
        )

        val error = assertThrows(IllegalArgumentException::class.java) {
            RoutePlanFactory.create(
                ProbeScenario.FOREIGN_DIRECT_DUAL,
                sdkInt = 35,
                chinaCidrs = insufficient,
            )
        }

        assertTrue(error.message.orEmpty().contains(MAX_ACTIVE_RULE_IPS_PER_FAMILY.toString()))
    }

    @Test
    fun everyScenarioUsesExactDeterministicNonAdjacentMappings() {
        for (scenario in ProbeScenario.entries) {
            val first = RoutePlanFactory.create(scenario, sdkInt = 35, chinaCidrs = capacityChina)
            val second = RoutePlanFactory.create(scenario, sdkInt = 35, chinaCidrs = capacityChina)
            assertEquals(first.syntheticMappings, second.syntheticMappings)
            assertEquals(scenario.families, first.syntheticMappings.map(Cidr::family).toSet())

            for (family in scenario.families) {
                val mappings = first.syntheticMappings.filter { it.family == family }
                assertEquals(MAX_ACTIVE_RULE_IPS_PER_FAMILY, mappings.size)
                assertTrue(mappings.all { it.prefixLength == family.bitCount })
                assertEquals(mappings.size, mappings.map(Cidr::network).toSet().size)
                assertEquals(mappings.size, CidrMath.collapse(mappings).size)
                assertTrue(
                    mappings.sorted().zipWithNext().all { (left, right) ->
                        right.network - left.network > BigInteger.ONE
                    },
                )

                val cn = CidrMath.collapse(capacityChina.filter { it.family == family })
                val sourceBlocks = when (scenario.mode) {
                    ProbeMode.DOMESTIC_DIRECT -> CidrMath.complement(cn, family)
                    ProbeMode.FOREIGN_DIRECT -> cn
                }
                val sourceIndexes = mappings.map { mapping ->
                    sourceBlocks.indexOfFirst { it.contains(mapping.network) }.also { index ->
                        assertTrue("mapping must belong to ${scenario.mode} whitelist source", index >= 0)
                    }
                }
                assertEquals(mappings.size, sourceIndexes.toSet().size)
                assertFalse(mappings.any { mapping -> forbiddenForTest(family).any(mapping::overlaps) })
            }
        }
    }

    @Test
    fun api32ForeignDirectUsesSmallIncludesOnlyPlan() {
        val plan = RoutePlanFactory.create(
            ProbeScenario.FOREIGN_DIRECT_DUAL,
            sdkInt = 32,
            chinaCidrs = capacityChina,
        )

        assertEquals(RouteStrategy.INCLUDES_ONLY, plan.strategy)
        assertTrue(plan.excludes.isEmpty())
        assertTrue(
            "foreign-direct whitelist mode must stay within small-route capacity",
            plan.builderEntryCount <= MAX_ACTIVE_RULE_IPS_PER_FAMILY * plan.scenario.families.size +
                plan.expectedCaptured.size,
        )
    }

    @Test
    fun api33ForeignDirectAlsoUsesSmallIncludesOnlyPlan() {
        val plan = RoutePlanFactory.create(
            ProbeScenario.FOREIGN_DIRECT_DUAL,
            sdkInt = 33,
            chinaCidrs = capacityChina,
        )

        assertEquals(RouteStrategy.INCLUDES_ONLY, plan.strategy)
        assertTrue(plan.excludes.isEmpty())
        assertTrue(plan.builderEntryCount <= 2_050)
    }

    @Test
    fun foreignDirectWhitelistCapturesDomesticTargetsAndBypassesForeignTargets() {
        val plan = RoutePlanFactory.create(
            ProbeScenario.FOREIGN_DIRECT_DUAL,
            sdkInt = 31,
            chinaCidrs = capacityChina,
        )

        assertEquals(RouteStrategy.INCLUDES_ONLY, plan.strategy)
        assertEquals(
            setOf("114.114.114.114", "2400:3200:0:0:0:0:0:1"),
            plan.expectedCaptured.map { it.hostAddress }.toSet(),
        )
        assertEquals(
            setOf("8.8.8.8", "2001:4860:4860:0:0:0:0:8888"),
            plan.expectedBypass.map { it.hostAddress }.toSet(),
        )
        assertTrue(plan.expectedCaptured.all { RoutePlanFactory.materializeVpnRanges(plan).containsAddress(it) })
        assertFalse(plan.expectedBypass.any { RoutePlanFactory.materializeVpnRanges(plan).containsAddress(it) })
    }

    @Test
    fun domesticDirectContainsOnlyExplicitHostRoutes() {
        val plan = RoutePlanFactory.create(
            ProbeScenario.DOMESTIC_DIRECT_DUAL,
            sdkInt = 35,
            chinaCidrs = capacityChina,
        )

        assertEquals(RouteStrategy.INCLUDES_ONLY, plan.strategy)
        assertTrue(plan.includes.all { it.prefixLength == it.family.bitCount })
        assertTrue(plan.excludes.isEmpty())
        val expected = (
            plan.syntheticMappings + plan.expectedCaptured.map(::hostCidr)
        ).toSet()
        assertEquals(expected, plan.includes.toSet())
    }

    @Test
    fun sdkBranchMatrixHasEquivalentSemantics() {
        val foreignScenarios = ProbeScenario.entries.filter { it.mode == ProbeMode.FOREIGN_DIRECT }
        for (scenario in foreignScenarios) {
            for (sdk in listOf(26, 32, 33, 35)) {
                val plan = RoutePlanFactory.create(scenario, sdk, capacityChina)
                assertEquals(RouteStrategy.INCLUDES_ONLY, plan.strategy)
                assertTrue(plan.excludes.isEmpty())
            }
            assertEquals(
                RoutePlanFactory.materializeVpnRanges(
                    RoutePlanFactory.create(scenario, 32, capacityChina),
                ),
                RoutePlanFactory.materializeVpnRanges(
                    RoutePlanFactory.create(scenario, 33, capacityChina),
                ),
            )
        }
    }

    @Test
    fun expectedPathsMatchEveryEffectivePlan() {
        for (scenario in ProbeScenario.entries) {
            val plan = RoutePlanFactory.create(scenario, sdkInt = 35, chinaCidrs = capacityChina)
            val effectiveVpn = RoutePlanFactory.materializeVpnRanges(plan)

            assertTrue(plan.expectedCaptured.all { effectiveVpn.containsAddress(it) })
            assertTrue(plan.expectedBypass.all { plan.directSet.containsAddress(it) })
            assertFalse(plan.expectedBypass.any { effectiveVpn.containsAddress(it) })
            assertEquals(plan.includes.size + plan.excludes.size, plan.builderEntryCount)
        }
    }

    @Test
    fun rejectsSdkBelowSupportedMinimum() {
        assertThrows(IllegalArgumentException::class.java) {
            RoutePlanFactory.create(
                ProbeScenario.DOMESTIC_DIRECT_IPV4,
                sdkInt = 25,
                chinaCidrs = capacityChina,
            )
        }
    }

    private fun forbiddenForTest(family: IpFamily): List<Cidr> {
        val systemV4 = listOf(
            "0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
            "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
            "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
            "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
        )
        val systemV6 = listOf(
            "::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32",
        )
        val pathTargets = listOf(
            "8.8.8.8/32",
            "2001:4860:4860::8888/128",
            "114.114.114.114/32",
            "2400:3200::1/128",
        )
        return (systemV4 + systemV6 + pathTargets)
            .map(Cidr::parse)
            .filter { it.family == family }
    }

    private fun hostCidr(address: InetAddress): Cidr {
        val family = if (address.address.size == IpFamily.IPV4.byteCount) {
            IpFamily.IPV4
        } else {
            IpFamily.IPV6
        }
        return Cidr(BigInteger(1, address.address), family.bitCount, family)
    }

    private fun List<Cidr>.containsAddress(address: InetAddress): Boolean {
        val value = BigInteger(1, address.address)
        val family = if (address.address.size == IpFamily.IPV4.byteCount) {
            IpFamily.IPV4
        } else {
            IpFamily.IPV6
        }
        return any { it.family == family && it.contains(value) }
    }
}
