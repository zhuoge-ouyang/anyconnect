package com.msitools.anyconnectmobile.core

import java.math.BigInteger
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress

enum class IpFamily(val bitCount: Int, val byteCount: Int) {
    IPV4(32, 4),
    IPV6(128, 16),

    ;

    companion object {
        fun from(address: InetAddress): IpFamily = when (address) {
            is Inet4Address -> IPV4
            is Inet6Address -> IPV6
            else -> error("Unsupported address family: $address")
        }
    }
}

data class IpCidr(
    val network: BigInteger,
    val prefixLength: Int,
    val family: IpFamily,
) : Comparable<IpCidr> {
    init {
        require(prefixLength in 0..family.bitCount)
        require(network.signum() >= 0 && network <= family.maxValue())
        require(network == network.and(mask(family, prefixLength)))
    }

    val size: BigInteger
        get() = BigInteger.ONE.shiftLeft(family.bitCount - prefixLength)

    val end: BigInteger
        get() = network + size - BigInteger.ONE

    fun contains(address: InetAddress): Boolean {
        val addressFamily = IpFamily.from(address)
        if (addressFamily != family) return false
        val value = BigInteger(1, address.address)
        return value in network..end
    }

    fun address(): InetAddress {
        val bytes = network.toFixedBytes(family.byteCount)
        return when (family) {
            IpFamily.IPV4 -> InetAddress.getByAddress(bytes)
            IpFamily.IPV6 -> Inet6Address.getByAddress(null, bytes, -1)
        }
    }

    override fun compareTo(other: IpCidr): Int =
        compareValuesBy(this, other, IpCidr::family, IpCidr::network, IpCidr::prefixLength)

    override fun toString(): String = "${address().hostAddress}/$prefixLength"

    companion object {
        fun parse(raw: String): IpCidr {
            val parts = raw.trim().split('/')
            require(parts.size == 2) { "CIDR must contain exactly one slash: $raw" }
            val address = InetAddress.getByName(parts[0])
            val family = IpFamily.from(address)
            val prefix = parts[1].toInt()
            return from(address, prefix)
        }

        fun from(address: InetAddress, prefixLength: Int = IpFamily.from(address).bitCount): IpCidr {
            val family = IpFamily.from(address)
            require(prefixLength in 0..family.bitCount)
            val value = BigInteger(1, address.address)
            return IpCidr(value.and(mask(family, prefixLength)), prefixLength, family)
        }
    }
}

fun IpFamily.maxValue(): BigInteger = BigInteger.ONE.shiftLeft(bitCount) - BigInteger.ONE

private fun mask(family: IpFamily, prefixLength: Int): BigInteger {
    if (prefixLength == 0) return BigInteger.ZERO
    val hostBits = family.bitCount - prefixLength
    return family.maxValue().xor(BigInteger.ONE.shiftLeft(hostBits) - BigInteger.ONE)
}

private fun BigInteger.toFixedBytes(length: Int): ByteArray {
    val signed = toByteArray()
    val unsigned = if (signed.size > length) signed.copyOfRange(signed.size - length, signed.size) else signed
    return ByteArray(length - unsigned.size) + unsigned
}

object IpCidrMath {
    private data class Range(val start: BigInteger, val end: BigInteger)

    fun collapse(cidrs: List<IpCidr>): List<IpCidr> = IpFamily.entries.flatMap { family ->
        val merged = mutableListOf<Range>()
        for (cidr in cidrs.filter { it.family == family }.sorted()) {
            val previous = merged.lastOrNull()
            if (previous == null || cidr.network > previous.end + BigInteger.ONE) {
                merged += Range(cidr.network, cidr.end)
            } else if (cidr.end > previous.end) {
                merged[merged.lastIndex] = previous.copy(end = cidr.end)
            }
        }
        merged.flatMap { rangeToCidrs(it.start, it.end, family) }
    }

    fun complement(excluded: List<IpCidr>, family: IpFamily): List<IpCidr> {
        val normalized = collapse(excluded.filter { it.family == family })
        val result = mutableListOf<IpCidr>()
        var cursor = BigInteger.ZERO
        for (cidr in normalized) {
            if (cursor < cidr.network) result += rangeToCidrs(cursor, cidr.network - BigInteger.ONE, family)
            if (cidr.end == family.maxValue()) return result
            cursor = cidr.end + BigInteger.ONE
        }
        if (cursor <= family.maxValue()) result += rangeToCidrs(cursor, family.maxValue(), family)
        return result
    }

    private fun rangeToCidrs(start: BigInteger, end: BigInteger, family: IpFamily): List<IpCidr> {
        require(start.signum() >= 0 && start <= end && end <= family.maxValue())
        val result = mutableListOf<IpCidr>()
        var cursor = start
        while (cursor <= end) {
            val alignmentBits = if (cursor == BigInteger.ZERO) family.bitCount else cursor.lowestSetBit
            val remainingBits = (end - cursor + BigInteger.ONE).bitLength() - 1
            val hostBits = minOf(alignmentBits, remainingBits, family.bitCount)
            result += IpCidr(cursor, family.bitCount - hostBits, family)
            cursor += BigInteger.ONE.shiftLeft(hostBits)
        }
        return result
    }
}
