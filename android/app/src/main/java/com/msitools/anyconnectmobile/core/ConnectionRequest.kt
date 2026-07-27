package com.msitools.anyconnectmobile.core

data class ConnectionRequest(
    val site: VpnSite,
    val username: String,
    val password: String,
) {
    init {
        require(username.isNotBlank()) { "username is blank" }
        require(password.isNotEmpty()) { "password is empty" }
    }
}
