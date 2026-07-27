package com.msitools.anyconnectmobile.routeprobesender

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class SenderManifestTest {
    @Test
    fun declaresMainSignaturePermissionForRestrictedBroadcastDelivery() {
        val manifest = File("src/main/AndroidManifest.xml").readText()

        assertTrue(
            manifest.contains(
                "android:name=\"com.msitools.anyconnectmobile.routeprobe.permission.SEND_HELPER_PROBE\"",
            ),
        )
    }

    @Test
    fun declaresWarmupActivitySoTheMainProbeCanUnstopTheHelperPackage() {
        val manifest = File("src/main/AndroidManifest.xml").readText()

        assertTrue(manifest.contains("android:name=\".HelperWarmupActivity\""))
        assertTrue(manifest.contains("android:theme=\"@android:style/Theme.NoDisplay\""))
    }
}
