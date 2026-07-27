package com.msitools.anyconnectmobile.routeprobe.packet

import java.net.InetAddress
import java.net.Inet6Address

object PacketDestinationParser {
    fun destination(packet: ByteArray?, length: Int): InetAddress? {
        if (packet == null || length <= 0 || length > packet.size) return null

        return when ((packet[0].toInt() ushr 4) and 0x0f) {
            4 -> ipv4Destination(packet, length)
            6 -> ipv6Destination(packet, length)
            else -> null
        }
    }

    private fun ipv4Destination(packet: ByteArray, length: Int): InetAddress? {
        if (length < IPV4_MIN_HEADER_BYTES) return null
        val headerBytes = (packet[0].toInt() and 0x0f) * 4
        if (headerBytes < IPV4_MIN_HEADER_BYTES || length < headerBytes) return null
        return InetAddress.getByAddress(packet.copyOfRange(IPV4_DESTINATION_OFFSET, 20))
    }

    private fun ipv6Destination(packet: ByteArray, length: Int): InetAddress? {
        if (length < IPV6_HEADER_BYTES) return null
        return Inet6Address.getByAddress(
            null,
            packet.copyOfRange(IPV6_DESTINATION_OFFSET, 40),
            -1,
        )
    }

    private const val IPV4_MIN_HEADER_BYTES = 20
    private const val IPV4_DESTINATION_OFFSET = 16
    private const val IPV6_HEADER_BYTES = 40
    private const val IPV6_DESTINATION_OFFSET = 24
}
