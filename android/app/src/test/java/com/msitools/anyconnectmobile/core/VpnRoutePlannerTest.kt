package com.msitools.anyconnectmobile.core

import java.net.InetAddress
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnRoutePlannerTest {
    private val ipv4 = setOf(IpFamily.IPV4)
    private val foreignHost = IpCidr.parse("8.8.8.8/32")
    private val domesticHost = IpCidr.parse("110.242.68.66/32")
    private val vpnDns = IpCidr.parse("172.31.255.250/32")

    @Test
    fun domesticDirectBootstrapCapturesOnlyVpnDnsBeforeHostResolution() {
        val plan = VpnRoutePlanner.create(
            mode = ConnectionMode.DOMESTIC_DIRECT,
            families = ipv4,
            resolvedRules = emptyList(),
            dnsRoutes = listOf(vpnDns),
            sdkInt = 31,
        )

        assertTrue(plan.includes == listOf(vpnDns))
        assertTrue(plan.excludes.isEmpty())
    }

    @Test
    fun domesticDirectOnlyIncludesForeignWhitelistAndVpnDns() {
        val plan = VpnRoutePlanner.create(
            mode = ConnectionMode.DOMESTIC_DIRECT,
            families = ipv4,
            resolvedRules = listOf(foreignHost),
            dnsRoutes = listOf(vpnDns),
            sdkInt = 31,
        )

        assertTrue(plan.includes.any { it.contains(InetAddress.getByName("8.8.8.8")) })
        assertTrue(plan.includes.any { it.contains(InetAddress.getByName("172.31.255.250")) })
        assertFalse(plan.includes.any { it.prefixLength == 0 })
        assertTrue(plan.excludes.isEmpty())
    }

    @Test
    fun foreignDirectUsesDefaultVpnAndDirectExclusionsOnApi33() {
        val plan = VpnRoutePlanner.create(
            mode = ConnectionMode.FOREIGN_DIRECT,
            families = ipv4,
            resolvedRules = listOf(domesticHost),
            dnsRoutes = listOf(vpnDns),
            sdkInt = 33,
        )

        assertTrue(plan.includes.any { it.prefixLength == 0 })
        assertTrue(plan.includes.any { it == vpnDns })
        assertTrue(plan.excludes.any { it.contains(InetAddress.getByName("110.242.68.66")) })
        assertTrue(plan.excludes.any { it.contains(InetAddress.getByName("192.168.3.1")) })
    }

    @Test
    fun foreignDirectUsesComplementRoutesOnApi26To32() {
        val plan = VpnRoutePlanner.create(
            mode = ConnectionMode.FOREIGN_DIRECT,
            families = ipv4,
            resolvedRules = listOf(domesticHost),
            dnsRoutes = listOf(vpnDns),
            sdkInt = 31,
        )

        assertTrue(plan.excludes.isEmpty())
        assertFalse(plan.includes.any { it.prefixLength == 0 })
        assertTrue(plan.includes.any { it == vpnDns })
        assertTrue(plan.includes.any { it.contains(InetAddress.getByName("8.8.8.8")) })
        assertFalse(plan.includes.any { it.contains(InetAddress.getByName("110.242.68.66")) })
        assertFalse(plan.includes.any { it.contains(InetAddress.getByName("192.168.3.1")) })
    }
}
