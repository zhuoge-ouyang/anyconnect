package com.msitools.anyconnectmobile.core

data class VpnRoutePlan(
    val includes: List<IpCidr>,
    val excludes: List<IpCidr>,
    val directSet: List<IpCidr>,
)

object VpnRoutePlanner {
    private val systemDirect = listOf(
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
        "::/128",
        "::1/128",
        "fc00::/7",
        "fe80::/10",
        "ff00::/8",
        "2001:db8::/32",
    ).map(IpCidr::parse)

    fun create(
        mode: ConnectionMode,
        families: Set<IpFamily>,
        resolvedRules: List<IpCidr>,
        dnsRoutes: List<IpCidr>,
        sdkInt: Int,
    ): VpnRoutePlan {
        require(families.isNotEmpty()) { "VPN did not provide an IP address" }
        val rules = IpCidrMath.collapse(resolvedRules.filter { it.family in families })
        val dns = dnsRoutes.filter { it.family in families }.distinct()
        return when (mode) {
            ConnectionMode.DOMESTIC_DIRECT -> VpnRoutePlan(
                includes = IpCidrMath.collapse(rules + dns),
                excludes = emptyList(),
                directSet = emptyList(),
            )

            ConnectionMode.FOREIGN_DIRECT -> {
                val direct = IpCidrMath.collapse(systemDirect.filter { it.family in families } + rules)
                if (sdkInt >= 33) {
                    VpnRoutePlan(
                        includes = families.sorted().map { IpCidr(java.math.BigInteger.ZERO, 0, it) } + dns,
                        excludes = direct,
                        directSet = direct,
                    )
                } else {
                    VpnRoutePlan(
                        includes = families.sorted().flatMap { IpCidrMath.complement(direct, it) } + dns,
                        excludes = emptyList(),
                        directSet = direct,
                    )
                }
            }
        }
    }
}
