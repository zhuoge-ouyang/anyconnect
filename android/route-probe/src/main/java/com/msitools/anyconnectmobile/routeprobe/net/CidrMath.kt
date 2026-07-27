package com.msitools.anyconnectmobile.routeprobe.net

import java.math.BigInteger

object CidrMath {
    private data class Range(val start: BigInteger, val end: BigInteger)

    fun collapse(cidrs: List<Cidr>): List<Cidr> {
        if (cidrs.isEmpty()) return emptyList()
        val family = cidrs.first().family
        require(cidrs.all { it.family == family }) {
            "CIDRs from different address families cannot be collapsed together"
        }

        val merged = mutableListOf<Range>()
        for (cidr in cidrs.sorted()) {
            val previous = merged.lastOrNull()
            if (previous == null || cidr.network > previous.end + BigInteger.ONE) {
                merged += Range(cidr.network, cidr.end)
            } else if (cidr.end > previous.end) {
                merged[merged.lastIndex] = previous.copy(end = cidr.end)
            }
        }
        return merged.flatMap { range -> rangeToCidrs(range.start, range.end, family) }
    }

    fun complement(excluded: List<Cidr>, family: IpFamily): List<Cidr> {
        val normalized = collapse(excluded.filter { it.family == family })
        val result = mutableListOf<Cidr>()
        var cursor = BigInteger.ZERO

        for (cidr in normalized) {
            if (cursor < cidr.network) {
                result += rangeToCidrs(cursor, cidr.network - BigInteger.ONE, family)
            }
            if (cidr.end == family.maxValue()) return result
            cursor = cidr.end + BigInteger.ONE
        }

        if (cursor <= family.maxValue()) {
            result += rangeToCidrs(cursor, family.maxValue(), family)
        }
        return result
    }

    private fun rangeToCidrs(
        start: BigInteger,
        end: BigInteger,
        family: IpFamily,
    ): List<Cidr> {
        require(start.signum() >= 0 && start <= end && end <= family.maxValue()) {
            "Range must be inside ${family.name}"
        }

        val result = mutableListOf<Cidr>()
        var cursor = start
        while (cursor <= end) {
            val alignmentBits = if (cursor == BigInteger.ZERO) {
                family.bitCount
            } else {
                cursor.lowestSetBit.coerceAtMost(family.bitCount)
            }
            val remainingBits = (end - cursor + BigInteger.ONE).bitLength() - 1
            val hostBits = minOf(alignmentBits, remainingBits)
            result += Cidr(cursor, family.bitCount - hostBits, family)
            cursor += BigInteger.ONE.shiftLeft(hostBits)
        }
        return result
    }
}
