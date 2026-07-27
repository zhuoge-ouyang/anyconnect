package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Test

class ContactAuthorPresentationTest {
    @Test
    fun loginShowsInlineQrOnly() {
        assertEquals(
            ContactAuthorPresentation(showInlineQr = true, showMainButton = false),
            contactAuthorPresentation(isConnected = false),
        )
    }

    @Test
    fun connectedMainScreenShowsButtonOnly() {
        assertEquals(
            ContactAuthorPresentation(showInlineQr = false, showMainButton = true),
            contactAuthorPresentation(isConnected = true),
        )
    }
}
