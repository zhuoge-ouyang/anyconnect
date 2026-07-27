package com.msitools.anyconnectmobile

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.infradead.libopenconnect.LibOpenConnect
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class NativeLibraryInstrumentedTest {
    @Test
    fun loadsOpenConnectNativeLibrary() {
        System.loadLibrary("openconnect")

        val version = LibOpenConnect.getVersion()

        assertTrue(version, version.contains("OpenConnect", ignoreCase = true))
    }
}
