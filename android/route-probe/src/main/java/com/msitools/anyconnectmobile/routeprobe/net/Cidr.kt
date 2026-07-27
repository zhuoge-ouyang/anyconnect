package com.msitools.anyconnectmobile.routeprobe.net

import java.math.BigInteger
import java.net.InetAddress
import java.net.Inet6Address

enum class IpFamily(val bitCount: Int, val byteCount: Int) {
    IPV4(32, 4),
    IPV6(128, 16),
}

data class Cidr(
    val network: BigInteger,
    val prefixLength: Int,
    val family: IpFamily,
) : Comparable<Cidr> {
    init {
        require(prefixLength in 0..family.bitCount) {
            "Prefix $prefixLength is invalid for ${family.name}"
        }
        require(network.signum() >= 0) { "Network must be unsigned" }
        require(network <= family.maxValue()) { "Network exceeds ${family.name}" }
        require(network == network.and(mask(family, prefixLength))) {
            "Network contains host bits"
        }
    }

    val size: BigInteger
        get() = BigInteger.ONE.shiftLeft(family.bitCount - prefixLength)

    val end: BigInteger
        get() = network + size - BigInteger.ONE

    fun contains(value: BigInteger): Boolean = value >= network && value <= end

    fun overlaps(other: Cidr): Boolean =
        family == other.family && network <= other.end && other.network <= end

    fun address(offset: BigInteger = BigInteger.ZERO): InetAddress {
        require(offset.signum() >= 0 && offset < size) {
            "Address offset must be inside $this"
        }
        val bytes = (network + offset).toFixedBytes(family.byteCount)
        return when (family) {
            IpFamily.IPV4 -> InetAddress.getByAddress(bytes)
            IpFamily.IPV6 -> Inet6Address.getByAddress(null, bytes, -1)
        }
    }

    override fun compareTo(other: Cidr): Int {
        val familyOrder = family.compareTo(other.family)
        if (familyOrder != 0) return familyOrder
        val networkOrder = network.compareTo(other.network)
        if (networkOrder != 0) return networkOrder
        return prefixLength.compareTo(other.prefixLength)
    }

    override fun toString(): String = requireNotNull(address().hostAddress) + "/" + prefixLength

    companion object {
        fun parse(raw: String): Cidr {
            val parts = raw.trim().split('/')
            require(parts.size == 2) { "CIDR must contain exactly one slash: $raw" }

            val addressText = parts[0]
            require(addressText.isNotEmpty()) { "CIDR address is empty: $raw" }
            val family = if (':' in addressText) {
                IpFamily.IPV6
            } else {
                IpFamily.IPV4
            }
            val addressBytes = parseNumericAddress(addressText, family)
            val prefixText = parts[1]
            require(prefixText.isNotEmpty() && prefixText.all { it in '0'..'9' }) {
                "CIDR prefix must contain ASCII digits only: $raw"
            }
            val prefix = prefixText.toIntOrNull()
                ?: throw IllegalArgumentException("CIDR prefix is not an integer: $raw")
            require(prefix in 0..family.bitCount) {
                "Prefix $prefix is invalid for ${family.name}"
            }

            val value = BigInteger(1, addressBytes)
            return Cidr(value.and(mask(family, prefix)), prefix, family)
        }

        private fun parseNumericAddress(raw: String, family: IpFamily): ByteArray {
            require(raw.none(Char::isWhitespace)) { "Address contains whitespace: $raw" }
            return when (family) {
                IpFamily.IPV4 -> parseIpv4Bytes(raw)
                IpFamily.IPV6 -> {
                    require('%' !in raw) { "Scoped IPv6 addresses are not valid CIDRs: $raw" }
                    parseIpv6Bytes(raw)
                }
            }
        }

        private fun parseIpv4Bytes(raw: String): ByteArray {
            val octets = raw.split('.')
            require(octets.size == IpFamily.IPV4.byteCount) { "Invalid IPv4 address: $raw" }
            return ByteArray(IpFamily.IPV4.byteCount) { index ->
                val octet = octets[index]
                require(octet.isNotEmpty() && octet.all { it in '0'..'9' }) {
                    "Invalid IPv4 address: $raw"
                }
                val value = octet.toIntOrNull()
                require(value != null && value in 0..255) { "Invalid IPv4 address: $raw" }
                value.toByte()
            }
        }

        private fun parseIpv6Bytes(raw: String): ByteArray {
            val compressionAt = raw.indexOf("::")
            require(compressionAt == raw.lastIndexOf("::")) {
                "IPv6 address contains multiple compression markers: $raw"
            }

            val groups = if (compressionAt >= 0) {
                val left = parseIpv6Side(raw.substring(0, compressionAt), allowEmbeddedIpv4 = false)
                val right = parseIpv6Side(
                    raw.substring(compressionAt + 2),
                    allowEmbeddedIpv4 = true,
                )
                require(left.size + right.size < 8) {
                    "IPv6 compression must replace at least one group: $raw"
                }
                left + List(8 - left.size - right.size) { 0 } + right
            } else {
                parseIpv6Side(raw, allowEmbeddedIpv4 = true).also { parsed ->
                    require(parsed.size == 8) { "IPv6 address must contain eight groups: $raw" }
                }
            }

            return ByteArray(IpFamily.IPV6.byteCount).also { bytes ->
                groups.forEachIndexed { index, group ->
                    bytes[index * 2] = (group ushr 8).toByte()
                    bytes[index * 2 + 1] = group.toByte()
                }
            }
        }

        private fun parseIpv6Side(raw: String, allowEmbeddedIpv4: Boolean): List<Int> {
            if (raw.isEmpty()) return emptyList()
            val tokens = raw.split(':')
            require(tokens.none(String::isEmpty)) { "IPv6 address contains an empty group: $raw" }

            return buildList {
                tokens.forEachIndexed { index, token ->
                    if ('.' in token) {
                        require(allowEmbeddedIpv4 && index == tokens.lastIndex) {
                            "Embedded IPv4 must be the final IPv6 component: $raw"
                        }
                        val ipv4 = parseIpv4Bytes(token)
                        add(((ipv4[0].toInt() and 0xff) shl 8) or (ipv4[1].toInt() and 0xff))
                        add(((ipv4[2].toInt() and 0xff) shl 8) or (ipv4[3].toInt() and 0xff))
                    } else {
                        require(token.length in 1..4 && token.all(::isAsciiHexDigit)) {
                            "Invalid IPv6 group: $token"
                        }
                        add(token.toInt(16))
                    }
                }
            }
        }

        private fun isAsciiHexDigit(value: Char): Boolean =
            value in '0'..'9' || value in 'a'..'f' || value in 'A'..'F'
    }
}

fun IpFamily.maxValue(): BigInteger = BigInteger.ONE.shiftLeft(bitCount) - BigInteger.ONE

private fun mask(family: IpFamily, prefixLength: Int): BigInteger {
    if (prefixLength == 0) return BigInteger.ZERO
    val hostBits = family.bitCount - prefixLength
    val hostMask = BigInteger.ONE.shiftLeft(hostBits) - BigInteger.ONE
    return family.maxValue().xor(hostMask)
}

private fun BigInteger.toFixedBytes(length: Int): ByteArray {
    val signed = toByteArray()
    val unsigned = if (signed.size > length) {
        signed.copyOfRange(signed.size - length, signed.size)
    } else {
        signed
    }
    return ByteArray(length - unsigned.size) + unsigned
}
