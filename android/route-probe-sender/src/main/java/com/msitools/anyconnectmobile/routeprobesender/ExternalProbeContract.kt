package com.msitools.anyconnectmobile.routeprobesender

object ExternalProbeContract {
    const val AUTHORITY = "com.msitools.anyconnectmobile.routeprobesender.probe"
    const val ACTION_SEND = "com.msitools.anyconnectmobile.routeprobesender.SEND_UDP_PROBE"
    const val METHOD_SEND = "send_udp_probe"
    const val KEY_DESTINATION = "destination"
    const val KEY_NONCE = "nonce"
    const val KEY_RESULT_RECEIVER = "result_receiver"
    const val KEY_STATUS = "status"
    const val KEY_ERROR_CLASS = "error_class"
    const val KEY_ERROR_MESSAGE = "error_message"
    const val STATUS_SUCCESS = "SUCCESS"
    const val STATUS_FAILED = "FAILED"
}
