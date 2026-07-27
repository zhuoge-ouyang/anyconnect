package com.msitools.anyconnectmobile.routeprobe.net

import java.math.BigInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class CidrMathTest {
    @Test
    fun canonicalizesHostBits() {
        assertEquals("10.0.0.0/8", Cidr.parse("10.99.8.7/8").toString())
        assertEquals(
            "2001:db8:0:0:0:0:0:0/32",
            Cidr.parse("2001:db8::1234/32").toString(),
        )
    }

    @Test
    fun parsesNumericEmbeddedIpv4AndRejectsInvalidIpv6Characters() {
        val mapped = Cidr.parse("::ffff:192.0.2.1/128")

        assertEquals(IpFamily.IPV6, mapped.family)
        assertEquals("0:0:0:0:0:ffff:c000:201/128", mapped.toString())
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("g:foo/64") }
    }

    @Test
    fun collapsesAdjacentSiblingRanges() {
        val result = CidrMath.collapse(
            listOf(Cidr.parse("10.0.0.0/9"), Cidr.parse("10.128.0.0/9")),
        )

        assertEquals(listOf("10.0.0.0/8"), result.map(Cidr::toString))
    }

    @Test
    fun collapsesDuplicatesAndChildrenIntoParent() {
        val result = CidrMath.collapse(
            listOf(
                Cidr.parse("10.0.0.0/8"),
                Cidr.parse("10.0.0.0/8"),
                Cidr.parse("10.64.0.0/10"),
            ),
        )

        assertEquals(listOf("10.0.0.0/8"), result.map(Cidr::toString))
    }

    @Test
    fun recursivelyCollapsesSiblingsToWholeUniverse() {
        val result = CidrMath.collapse(
            listOf(
                Cidr.parse("0.0.0.0/2"),
                Cidr.parse("64.0.0.0/2"),
                Cidr.parse("128.0.0.0/2"),
                Cidr.parse("192.0.0.0/2"),
            ),
        )

        assertEquals(listOf("0.0.0.0/0"), result.map(Cidr::toString))
    }

    @Test
    fun adjacentRangesDoNotOverCoverWhenTheyCannotBecomeOneCidr() {
        val result = CidrMath.collapse(
            listOf(Cidr.parse("10.0.0.0/24"), Cidr.parse("10.0.1.0/25")),
        )

        assertEquals(
            listOf("10.0.0.0/24", "10.0.1.0/25"),
            result.map(Cidr::toString),
        )
        assertEquals(BigInteger.valueOf(384), result.sumOfBigInteger(Cidr::size))
    }

    @Test
    fun preservesSingleAddressCidrsForBothFamilies() {
        val ipv4 = CidrMath.collapse(listOf(Cidr.parse("203.0.113.7/32")))
        val ipv6 = CidrMath.collapse(listOf(Cidr.parse("2001:db8::7/128")))

        assertEquals(listOf("203.0.113.7/32"), ipv4.map(Cidr::toString))
        assertEquals(
            listOf("2001:db8:0:0:0:0:0:7/128"),
            ipv6.map(Cidr::toString),
        )
    }

    @Test
    fun computesExactIpv4Complement() {
        val excluded = listOf(Cidr.parse("0.0.0.0/1"))
        val result = CidrMath.complement(excluded, IpFamily.IPV4)

        assertEquals(listOf("128.0.0.0/1"), result.map(Cidr::toString))
        assertComplementInvariants(excluded, result, IpFamily.IPV4)
    }

    @Test
    fun computesExactIpv6Complement() {
        val excluded = listOf(Cidr.parse("::/1"))
        val result = CidrMath.complement(excluded, IpFamily.IPV6)

        assertEquals(
            listOf("8000:0:0:0:0:0:0:0/1"),
            result.map(Cidr::toString),
        )
        assertComplementInvariants(excluded, result, IpFamily.IPV6)
    }

    @Test
    fun complementAndExcludedCoverAddressSpaceWithoutOverlap() {
        val excluded = listOf(Cidr.parse("10.0.0.0/8"), Cidr.parse("192.168.0.0/16"))
        val included = CidrMath.complement(excluded, IpFamily.IPV4)

        assertComplementInvariants(excluded, included, IpFamily.IPV4)
    }

    @Test
    fun emptyAndFullComplementsAreExact() {
        val ipv4Excluded = emptyList<Cidr>()
        val ipv4Included = CidrMath.complement(ipv4Excluded, IpFamily.IPV4)
        assertEquals(listOf("0.0.0.0/0"), ipv4Included.map(Cidr::toString))
        assertComplementInvariants(ipv4Excluded, ipv4Included, IpFamily.IPV4)

        val ipv6Excluded = listOf(Cidr.parse("::/0"))
        val ipv6Included = CidrMath.complement(ipv6Excluded, IpFamily.IPV6)
        assertTrue(ipv6Included.isEmpty())
        assertComplementInvariants(ipv6Excluded, ipv6Included, IpFamily.IPV6)
    }

    @Test
    fun complementHandlesSingleAddressHoleExactly() {
        val excluded = listOf(Cidr.parse("0.0.0.0/32"))
        val included = CidrMath.complement(excluded, IpFamily.IPV4)

        assertFalse(included.any { it.contains(BigInteger.ZERO) })
        assertTrue(included.any { it.contains(BigInteger.ONE) })
        assertComplementInvariants(excluded, included, IpFamily.IPV4)
    }

    @Test
    fun complementNormalizesOverlappingAndRepeatedExclusions() {
        val excluded = listOf(
            Cidr.parse("10.0.0.0/8"),
            Cidr.parse("10.0.0.0/9"),
            Cidr.parse("10.0.0.0/8"),
        )
        val included = CidrMath.complement(excluded, IpFamily.IPV4)

        assertComplementInvariants(excluded, included, IpFamily.IPV4)
    }

    @Test
    fun complementSelectsOnlyRequestedFamilyFromDualStackInput() {
        val dualStack = listOf(Cidr.parse("10.0.0.0/8"), Cidr.parse("2001:db8::/32"))

        val ipv4 = CidrMath.complement(dualStack, IpFamily.IPV4)
        val ipv6 = CidrMath.complement(dualStack, IpFamily.IPV6)

        assertComplementInvariants(listOf(dualStack[0]), ipv4, IpFamily.IPV4)
        assertComplementInvariants(listOf(dualStack[1]), ipv6, IpFamily.IPV6)
    }

    @Test
    fun addressUsesExactOffsetsAndRejectsOutOfRangeOffsets() {
        val cidr = Cidr.parse("192.0.2.0/24")

        assertEquals("192.0.2.0", cidr.address().hostAddress)
        assertEquals("192.0.2.255", cidr.address(BigInteger.valueOf(255)).hostAddress)
        assertThrows(IllegalArgumentException::class.java) {
            cidr.address(BigInteger.valueOf(256))
        }
    }

    @Test
    fun rejectsMixedFamiliesAndInvalidCidrs() {
        assertThrows(IllegalArgumentException::class.java) {
            CidrMath.collapse(listOf(Cidr.parse("10.0.0.0/8"), Cidr.parse("::1/128")))
        }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("10.0.0.0/33") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("example.com/24") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("10.0.0.1") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("10.0.0.1/32/7") }
    }

    @Test
    fun rejectsNonAsciiAddressAndPrefixDigits() {
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("١.2.3.4/32") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("::ｆ/128") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("192.0.2.1/٣٢") }
    }

    private fun assertComplementInvariants(
        excludedInput: List<Cidr>,
        included: List<Cidr>,
        family: IpFamily,
    ) {
        val excluded = CidrMath.collapse(excludedInput.filter { it.family == family })
        assertFalse(included.any { candidate -> excluded.any(candidate::overlaps) })
        assertEquals(
            BigInteger.ONE.shiftLeft(family.bitCount),
            (included + excluded).sumOfBigInteger(Cidr::size),
        )
        assertEquals(included, CidrMath.collapse(included))
        assertTrue(included.all { Cidr.parse(it.toString()) == it })
    }
}

private fun <T> Iterable<T>.sumOfBigInteger(selector: (T) -> BigInteger): BigInteger =
    fold(BigInteger.ZERO) { total, value -> total + selector(value) }
