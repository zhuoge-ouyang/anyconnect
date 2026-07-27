package com.msitools.anyconnectmobile.service

import org.infradead.libopenconnect.LibOpenConnect

object OpenConnectInfo {
    fun versionSummary(): String = runCatching {
        System.loadLibrary("openconnect")
        "OpenConnect: ${LibOpenConnect.getVersion()}"
    }.getOrElse { error ->
        "OpenConnect native load failed: ${error.javaClass.simpleName}: ${error.message}"
    }
}
