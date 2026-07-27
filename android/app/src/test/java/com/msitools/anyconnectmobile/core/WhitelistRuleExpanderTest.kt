package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WhitelistRuleExpanderTest {
    @Test
    fun expandsGoogleKeywordIntoRelatedHosts() {
        val result = WhitelistRuleExpander.mergeAndExpand("", "google")
        val lines = result.rulesText.lines()

        assertTrue("www.google.com should be included", "www.google.com" in lines)
        assertTrue("accounts.google.com should be included", "accounts.google.com" in lines)
        assertTrue("googleapis.com should be included", "googleapis.com" in lines)
        assertTrue("youtube.com should be included", "youtube.com" in lines)
        assertTrue("bare keyword should not be stored", "google" !in lines)
    }

    @Test
    fun mergesExistingRulesAndDeduplicatesNewRules() {
        val result = WhitelistRuleExpander.mergeAndExpand(
            existingRules = """
                8.8.8.8
                https://www.google.com/search
            """.trimIndent(),
            newRules = """
                8.8.8.8
                google
            """.trimIndent(),
        )
        val lines = result.rulesText.lines()

        assertEquals(1, lines.count { it == "8.8.8.8" })
        assertEquals(1, lines.count { it == "www.google.com" })
        assertTrue("google alias should expand to related hosts", "accounts.google.com" in lines)
    }

    @Test
    fun preservesIpAndCidrRules() {
        val result = WhitelistRuleExpander.mergeAndExpand("", "1.1.1.1\n114.114.114.0/24")

        assertEquals(
            listOf("1.1.1.1", "114.114.114.0/24"),
            result.rulesText.lines(),
        )
    }
}
