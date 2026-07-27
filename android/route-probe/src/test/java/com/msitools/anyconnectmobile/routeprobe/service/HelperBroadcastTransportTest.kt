package com.msitools.anyconnectmobile.routeprobe.service

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class HelperBroadcastTransportTest {
    @Test
    fun helperTransportUsesContentProviderInsteadOfStoppedPackageBroadcast() {
        val source = File(
            "src/main/java/com/msitools/anyconnectmobile/routeprobe/service/" +
                "HelperProbeContentProviderSender.kt",
        ).readText()

        assertTrue(source.contains("contentResolver.call("))
        assertTrue(source.contains("Uri.parse(\"content://\$AUTHORITY\")"))
        assertTrue(
            "Stopped helper packages are not reliably woken by broadcast on Huawei",
            !source.contains("sendBroadcast("),
        )
    }

    @Test
    fun providerTransportHasNoBroadcastCallbackTimeoutWindow() {
        val source = File(
            "src/main/java/com/msitools/anyconnectmobile/routeprobe/service/" +
                "HelperProbeContentProviderSender.kt",
        ).readText()

        assertTrue(!source.contains("RESPONSE_TIMEOUT_MS"))
        assertTrue(!source.contains("helperBroadcastResult=TIMEOUT"))
    }
}
