package com.msitools.anyconnectmobile.service

import org.junit.Assert.assertEquals
import org.junit.Test

class DnsRefreshPolicyTest {
    @Test
    fun refreshesBeforeDnsTtlExpires() {
        assertEquals(240_000L, DnsRefreshPolicy.delayMillis(300L))
    }

    @Test
    fun clampsVeryShortAndVeryLongTtls() {
        assertEquals(30_000L, DnsRefreshPolicy.delayMillis(1L))
        assertEquals(1_800_000L, DnsRefreshPolicy.delayMillis(86_400L))
    }

    @Test
    fun routeRetentionHonorsTtlAndProtectsZeroTtlAnswersForOneRefreshWindow() {
        assertEquals(130_000L, DnsRefreshPolicy.expirationMillis(100_000L, 30L))
        assertEquals(400_000L, DnsRefreshPolicy.expirationMillis(100_000L, 300L))
        assertEquals(130_000L, DnsRefreshPolicy.expirationMillis(100_000L, 0L))
    }
}
