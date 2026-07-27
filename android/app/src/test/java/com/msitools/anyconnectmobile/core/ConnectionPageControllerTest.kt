package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionPageControllerTest {
    @Test
    fun activeConnectionDoesNotOverrideRequestedSiteSelection() {
        val controller = ConnectionPageController()

        controller.requestSiteSelection()

        assertFalse(controller.shouldShowConnected(connectionActive = true))
    }

    @Test
    fun activeConnectionShowsConnectedPageByDefault() {
        val controller = ConnectionPageController()

        assertTrue(controller.shouldShowConnected(connectionActive = true))
    }

    @Test
    fun beginningNewConnectionRestoresConnectedPresentation() {
        val controller = ConnectionPageController()
        controller.requestSiteSelection()

        controller.finishSiteSelection()

        assertTrue(controller.shouldShowConnected(connectionActive = true))
    }
}
