package com.msitools.anyconnectmobile.routeprobe.net

import java.net.InetAddress

const val MAX_ACTIVE_RULE_IPS_PER_FAMILY: Int = 1_024

enum class ProbeScenario(val mode: ProbeMode, val families: Set<IpFamily>) {
    DOMESTIC_DIRECT_IPV4(ProbeMode.DOMESTIC_DIRECT, setOf(IpFamily.IPV4)),
    DOMESTIC_DIRECT_IPV6(ProbeMode.DOMESTIC_DIRECT, setOf(IpFamily.IPV6)),
    DOMESTIC_DIRECT_DUAL(ProbeMode.DOMESTIC_DIRECT, setOf(IpFamily.IPV4, IpFamily.IPV6)),
    FOREIGN_DIRECT_IPV4(ProbeMode.FOREIGN_DIRECT, setOf(IpFamily.IPV4)),
    FOREIGN_DIRECT_IPV6(ProbeMode.FOREIGN_DIRECT, setOf(IpFamily.IPV6)),
    FOREIGN_DIRECT_DUAL(ProbeMode.FOREIGN_DIRECT, setOf(IpFamily.IPV4, IpFamily.IPV6)),
}

enum class ProbeMode {
    DOMESTIC_DIRECT,
    FOREIGN_DIRECT,
}

enum class RouteStrategy {
    INCLUDES_ONLY,
    DEFAULT_WITH_EXCLUDES,
}

data class RoutePlan(
    val scenario: ProbeScenario,
    val strategy: RouteStrategy,
    val includes: List<Cidr>,
    val excludes: List<Cidr>,
    val directSet: List<Cidr>,
    val syntheticMappings: List<Cidr>,
    val expectedCaptured: List<InetAddress>,
    val expectedBypass: List<InetAddress>,
) {
    val builderEntryCount: Int
        get() = includes.size + excludes.size
}
