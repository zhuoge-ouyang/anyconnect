package com.msitools.anyconnectmobile.routeprobe

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class ProbeManifestTest {
    @Test
    fun declaresHelperProviderAuthorityForAndroidPackageVisibility() {
        val manifest = File("src/main/AndroidManifest.xml").readText()

        assertTrue(
            manifest.contains(
                "android:authorities=\"com.msitools.anyconnectmobile.routeprobesender.probe\"",
            ),
        )
    }
}
