package com.msitools.anyconnectmobile.service

internal object DnsRefreshPolicy {
    fun delayMillis(minimumTtlSeconds: Long): Long =
        (minimumTtlSeconds.coerceAtLeast(0L) * REFRESH_FRACTION_NUMERATOR * 1000L /
            REFRESH_FRACTION_DENOMINATOR)
            .coerceIn(MINIMUM_REFRESH_MS, MAXIMUM_REFRESH_MS)

    fun expirationMillis(nowMillis: Long, ttlSeconds: Long): Long =
        nowMillis + (ttlSeconds.coerceAtLeast(MINIMUM_RETENTION_SECONDS) * 1000L)

    private const val REFRESH_FRACTION_NUMERATOR = 4L
    private const val REFRESH_FRACTION_DENOMINATOR = 5L
    private const val MINIMUM_REFRESH_MS = 30_000L
    private const val MAXIMUM_REFRESH_MS = 1_800_000L
    private const val MINIMUM_RETENTION_SECONDS = 30L
}
