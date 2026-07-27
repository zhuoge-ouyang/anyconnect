package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnSiteCatalogTest {
    @Test
    fun defaultCatalogContainsDistributionSitesWithoutCredentials() {
        val sites = VpnSiteCatalog.defaultSites()

        assertEquals(18, sites.size)
        assertEquals("01.国内专线-上海节点", sites.first().name)
        assertEquals("https://api008621.ciscovnp.com:10000", sites.first().server)
        assertEquals("28.南非", sites.last().name)
        assertTrue(sites.all { it.server.startsWith("https://") })
        assertFalse(sites.any { it.name.contains("Yyy", ignoreCase = true) })
        assertFalse(sites.any { it.server.contains("Yyy", ignoreCase = true) })
    }

    @Test
    fun preferredSiteUsesConfiguredAustraliaNode() {
        val site = VpnSiteCatalog.findPreferred("23.澳大利亚")

        assertNotNull(site)
        assertEquals("https://api0061.ciscovnp.com:10000", site?.server)
    }

    @Test
    fun defaultAndSmartCandidatesPreferOverseasExitNodes() {
        assertEquals("22.日本", VpnSiteCatalog.defaultSite().name)
        assertEquals(
            listOf(
                "22.日本", "21.韩国", "10.香港地区", "20.泰国",
                "24.美国", "23.澳大利亚", "25.英国",
            ),
            VpnSiteCatalog.smartCandidates().map { it.name },
        )
    }
}
