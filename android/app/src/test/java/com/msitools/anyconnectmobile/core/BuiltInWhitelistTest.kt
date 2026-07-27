package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BuiltInWhitelistTest {
    @Test
    fun carriesDesktopForeignWhitelistForDomesticDirectMode() {
        assertEquals(32, BuiltInWhitelist.foreignVpnDomains.size)
        assertTrue("chatgpt.com" in BuiltInWhitelist.foreignVpnDomains)
        assertTrue("youtube.com" in BuiltInWhitelist.foreignVpnDomains)
        assertTrue("githubcopilot.com" in BuiltInWhitelist.foreignVpnDomains)
        assertTrue("claude.ai" in BuiltInWhitelist.foreignVpnDomains)
    }

    @Test
    fun carriesDesktopDomesticWhitelistForForeignDirectMode() {
        assertEquals(132, BuiltInWhitelist.domesticDirectDomains.size)
        assertTrue("baidu.com" in BuiltInWhitelist.domesticDirectDomains)
        assertTrue("weixin.qq.com" in BuiltInWhitelist.domesticDirectDomains)
        assertTrue("xiaohongshu.com" in BuiltInWhitelist.domesticDirectDomains)
        assertTrue("steamcontent.com" in BuiltInWhitelist.domesticDirectDomains)
    }

    @Test
    fun mergesBuiltInAndCustomRulesWithoutDuplicates() {
        val rules = BuiltInWhitelist.effectiveRules(
            ConnectionMode.DOMESTIC_DIRECT,
            "chatgpt.com\nexample.com",
        ).lineSequence().toList()

        assertEquals(1, rules.count { it == "chatgpt.com" })
        assertTrue("example.com" in rules)
    }

    @Test
    fun exposesEveryBuiltInRuleForTheViewer() {
        for (mode in ConnectionMode.entriesForUi) {
            assertEquals(
                BuiltInWhitelist.domainsFor(mode),
                BuiltInWhitelist.displayText(mode).lines(),
            )
        }
    }
}
