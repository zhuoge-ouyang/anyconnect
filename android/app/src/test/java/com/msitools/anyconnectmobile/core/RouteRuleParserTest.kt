package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Test

class RouteRuleParserTest {
    @Test
    fun parsesIpCidrHostAndUrlLines() {
        val rules = RouteRuleParser.parse(
            """
            8.8.8.8
            1.1.1.0/24
            https://www.google.com/generate_204
            bilibili.com # comment
            """.trimIndent(),
        )

        assertEquals(
            listOf(
                ParsedRouteRule("8.8.8.8", null, isHost = false),
                ParsedRouteRule("1.1.1.0", 24, isHost = false),
                ParsedRouteRule("www.google.com", null, isHost = true),
                ParsedRouteRule("bilibili.com", null, isHost = true),
            ),
            rules,
        )
    }

    @Test
    fun dropsBlankDuplicateAndCommentLines() {
        val rules = RouteRuleParser.parse(
            """
            # only comment
            8.8.8.8
            8.8.8.8

            """.trimIndent(),
        )

        assertEquals(listOf(ParsedRouteRule("8.8.8.8", null, isHost = false)), rules)
    }
}
