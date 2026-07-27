package com.msitools.anyconnectmobile.core

data class ContactAuthorPresentation(
    val showInlineQr: Boolean,
    val showMainButton: Boolean,
)

fun contactAuthorPresentation(isConnected: Boolean) = ContactAuthorPresentation(
    showInlineQr = !isConnected,
    showMainButton = isConnected,
)
