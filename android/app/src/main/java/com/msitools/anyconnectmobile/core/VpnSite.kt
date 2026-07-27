package com.msitools.anyconnectmobile.core

data class VpnSite(
    val name: String,
    val server: String,
)

object VpnSiteCatalog {
    fun defaultSites(): List<VpnSite> = sites

    fun defaultSite(): VpnSite = requireNotNull(findPreferred(DEFAULT_SITE_NAME))

    fun smartCandidates(): List<VpnSite> = smartCandidateNames.mapNotNull(::findPreferred)

    fun findPreferred(name: String): VpnSite? = sites.firstOrNull { it.name == name }

    private val smartCandidateNames = listOf(
        "22.日本",
        "21.韩国",
        "10.香港地区",
        "20.泰国",
        "24.美国",
        "23.澳大利亚",
        "25.英国",
    )

    private val sites = listOf(
        VpnSite("01.国内专线-上海节点", "https://api008621.ciscovnp.com:10000"),
        VpnSite("02.国内专线-杭州节点", "https://api008621.ciscovnp.com:10000"),
        VpnSite("03.国内专线-深圳节点", "https://api008620.ciscovnp.com:10000"),
        VpnSite("04.国内专线-北京节点", "https://api008610.ciscovnp.com:10000"),
        VpnSite("05.国内专线-贵州节点", "https://api008620.ciscovnp.com:10000"),
        VpnSite("06.国内专线-徐州节点", "https://api008621.ciscovnp.com:10000"),
        VpnSite("10.香港地区", "https://api0852.ciscovnp.com:10000"),
        VpnSite("11.台湾地区", "https://api0886.ciscovnp.com:10000"),
        VpnSite("19.印度尼西亚", "https://api0062.ciscovnp.com:10000"),
        VpnSite("20.泰国", "https://api0066.ciscovnp.com:10000"),
        VpnSite("21.韩国", "https://api0082.ciscovnp.com:10000"),
        VpnSite("22.日本", "https://api0081.ciscovnp.com:10000"),
        VpnSite("23.澳大利亚", "https://api0061.ciscovnp.com:10000"),
        VpnSite("24.美国", "https://api0001.ciscovnp.com:10000"),
        VpnSite("25.英国", "https://api0044.ciscovnp.com:10000"),
        VpnSite("26.加拿大", "https://api0001.ciscovnp.com:10000"),
        VpnSite("27.土耳其", "https://api0090.ciscovnp.com:10000"),
        VpnSite("28.南非", "https://api0027.ciscovnp.com:10000"),
    )

    private const val DEFAULT_SITE_NAME = "22.日本"
}
