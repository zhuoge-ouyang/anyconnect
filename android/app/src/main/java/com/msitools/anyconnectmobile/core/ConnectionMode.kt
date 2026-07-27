package com.msitools.anyconnectmobile.core

enum class ConnectionMode(
    val label: String,
    val ruleTitle: String,
    val ruleHint: String,
) {
    DOMESTIC_DIRECT(
        label = "国内直连",
        ruleTitle = "国外 IP / 网址白名单",
        ruleHint = "国内直连时，在这里添加需要走 VPN 的国外 IP、CIDR 或网址，每行一个",
    ),
    FOREIGN_DIRECT(
        label = "国外直连",
        ruleTitle = "国内 IP / 网址白名单",
        ruleHint = "国外直连时，在这里添加需要国内直连的 IP、CIDR 或网址，每行一个",
    );

    companion object {
        val entriesForUi: List<ConnectionMode> = listOf(DOMESTIC_DIRECT, FOREIGN_DIRECT)

        fun fromStored(value: String?): ConnectionMode =
            entriesForUi.firstOrNull { it.name == value } ?: DOMESTIC_DIRECT
    }
}
