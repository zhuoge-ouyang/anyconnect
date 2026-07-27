package com.msitools.anyconnectmobile.routeprobe

object ProbeContract {
    const val ACTION_RUN = "com.msitools.anyconnectmobile.routeprobe.RUN"
    const val EXTRA_RECEIVER = "receiver"
    const val RESULT_PROGRESS = 1
    const val RESULT_COMPLETE = 2
    const val RESULT_FAILED = 3
    const val KEY_MESSAGE = "message"
    const val KEY_REPORT_JSON = "report_json"
    const val KEY_REPORT_PATH = "report_path"
}
