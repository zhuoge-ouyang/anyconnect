package com.msitools.anyconnectmobile.routeprobe

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.msitools.anyconnectmobile.routeprobe.data.ProbeIpDb
import com.msitools.anyconnectmobile.routeprobe.net.CidrMath
import com.msitools.anyconnectmobile.routeprobe.net.IpFamily
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ProbeAssetInstrumentedTest {
    @Test
    fun packagedSnapshotMatchesFrozenInput() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val db = ProbeIpDb.load(context)

        assertEquals(10_826, db.rawRecordCount)
        assertEquals(
            "00CFDE18AFC0C0C7B5E46DF143BD47141F7F22A83EEE4DF24C722806ADDE3E89",
            db.sha256,
        )
        assertEquals(8_786, db.cidrs.count { it.family == IpFamily.IPV4 })
        assertEquals(2_040, db.cidrs.count { it.family == IpFamily.IPV6 })
        assertEquals(
            5_488,
            CidrMath.collapse(db.cidrs.filter { it.family == IpFamily.IPV4 }).size,
        )
        assertEquals(
            2_012,
            CidrMath.collapse(db.cidrs.filter { it.family == IpFamily.IPV6 }).size,
        )
        assertEquals(
            11_953,
            CidrMath.complement(
                db.cidrs.filter { it.family == IpFamily.IPV4 },
                IpFamily.IPV4,
            ).size,
        )
        assertEquals(
            15_212,
            CidrMath.complement(
                db.cidrs.filter { it.family == IpFamily.IPV6 },
                IpFamily.IPV6,
            ).size,
        )
    }
}
