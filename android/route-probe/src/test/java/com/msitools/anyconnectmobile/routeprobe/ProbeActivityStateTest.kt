package com.msitools.anyconnectmobile.routeprobe

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProbeActivityStateTest {
    @Test
    fun coldStartAcceptsTheMostRecentCompletedReport() {
        assertTrue(
            shouldRestoreLatestReport(
                flowActive = false,
                flowStartedAtEpochMs = null,
                latestModifiedEpochMs = 100,
                displayedLatestModifiedEpochMs = null,
            ),
        )
    }

    @Test
    fun activeFlowAcceptsOnlyAReportWrittenAtOrAfterItsStart() {
        assertFalse(
            shouldRestoreLatestReport(
                flowActive = true,
                flowStartedAtEpochMs = 100,
                latestModifiedEpochMs = 99,
                displayedLatestModifiedEpochMs = null,
            ),
        )
        assertTrue(
            shouldRestoreLatestReport(
                flowActive = true,
                flowStartedAtEpochMs = 100,
                latestModifiedEpochMs = 100,
                displayedLatestModifiedEpochMs = null,
            ),
        )
        assertTrue(
            shouldRestoreLatestReport(
                flowActive = true,
                flowStartedAtEpochMs = 100,
                latestModifiedEpochMs = 101,
                displayedLatestModifiedEpochMs = null,
            ),
        )
    }

    @Test
    fun activeFlowWithoutARecordedStartFailsClosed() {
        assertFalse(
            shouldRestoreLatestReport(
                flowActive = true,
                flowStartedAtEpochMs = null,
                latestModifiedEpochMs = 100,
                displayedLatestModifiedEpochMs = null,
            ),
        )
    }

    @Test
    fun anAlreadyDisplayedVersionIsNotRestoredAgain() {
        assertFalse(
            shouldRestoreLatestReport(
                flowActive = false,
                flowStartedAtEpochMs = null,
                latestModifiedEpochMs = 100,
                displayedLatestModifiedEpochMs = 100,
            ),
        )
    }

    @Test
    fun aNewerVersionReplacesTheHistoricalReportOnScreen() {
        assertTrue(
            shouldRestoreLatestReport(
                flowActive = false,
                flowStartedAtEpochMs = null,
                latestModifiedEpochMs = 101,
                displayedLatestModifiedEpochMs = 100,
            ),
        )
    }

    @Test
    fun stateSnapshotDetachesTheReceiverBeforeSavingAnyField() {
        val sourceFile = File(
            "src/main/java/com/msitools/anyconnectmobile/routeprobe/ProbeActivity.kt",
        )
        assertTrue("ProbeActivity source must exist", sourceFile.isFile)
        val stateMethod = sourceFile.readText()
            .substringAfter("override fun onSaveInstanceState")
            .substringBefore("override fun onRequestPermissionsResult")
        val detachIndex = stateMethod.indexOf("RESULT_RECEIVER.detach(this)")
        val firstStateWriteIndex = stateMethod.indexOf("outState.put")

        assertTrue("onSaveInstanceState must detach the receiver", detachIndex >= 0)
        assertTrue(
            "Receiver detach must precede every saved-state write",
            detachIndex < firstStateWriteIndex,
        )
    }

    @Test
    fun warmsHelperPackageBeforeRequestingVpnPermission() {
        val source = File(
            "src/main/java/com/msitools/anyconnectmobile/routeprobe/ProbeActivity.kt",
        ).readText()
        val beginFlow = source
            .substringAfter("private fun beginPermissionFlow()")
            .substringBefore("private fun requestVpnPermission()")

        assertTrue(source.contains("HelperWarmupActivity"))
        assertTrue(beginFlow.contains("warmUpHelperPackage()"))
        assertTrue(
            "Helper warm-up must run before VPN permission flow starts",
            beginFlow.indexOf("warmUpHelperPackage()") <
                beginFlow.indexOf("requestVpnPermission()"),
        )
    }
}
