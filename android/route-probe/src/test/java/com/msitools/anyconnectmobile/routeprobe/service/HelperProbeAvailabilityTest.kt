package com.msitools.anyconnectmobile.routeprobe.service

import org.junit.Assert.assertEquals
import org.junit.Test

class HelperProbeAvailabilityTest {
    @Test
    fun missingHelperPackageDiagnosticNamesPackageAndProviderState() {
        val availability = HelperProbeAvailability(
            packageInstalled = false,
            providerResolved = false,
            broadcastReceiverResolved = false,
            lookupError = null,
        )

        assertEquals(
            "helperPackageInstalled=false helperProviderResolved=false " +
                "helperBroadcastReceiverResolved=false",
            availability.diagnostic(),
        )
    }

    @Test
    fun lookupErrorIsCollapsedToASingleDiagnosticToken() {
        val availability = HelperProbeAvailability(
            packageInstalled = false,
            providerResolved = false,
            broadcastReceiverResolved = false,
            lookupError = "java.lang.SecurityException: package visibility denied",
        )

        assertEquals(
            "helperPackageInstalled=false helperProviderResolved=false " +
                "helperBroadcastReceiverResolved=false " +
                "helperLookupError=java.lang.SecurityException:_package_visibility_denied",
            availability.diagnostic(),
        )
    }
}
