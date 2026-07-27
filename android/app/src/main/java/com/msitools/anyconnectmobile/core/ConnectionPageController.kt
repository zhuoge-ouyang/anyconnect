package com.msitools.anyconnectmobile.core

class ConnectionPageController {
    private var siteSelectionRequested = false

    fun requestSiteSelection() {
        siteSelectionRequested = true
    }

    fun finishSiteSelection() {
        siteSelectionRequested = false
    }

    fun shouldShowConnected(connectionActive: Boolean): Boolean {
        return connectionActive && !siteSelectionRequested
    }
}
