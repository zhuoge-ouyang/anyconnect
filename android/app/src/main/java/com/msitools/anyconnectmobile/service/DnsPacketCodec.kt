package com.msitools.anyconnectmobile.service

import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import java.io.IOException
import java.net.IDN
import java.net.InetAddress

internal enum class DnsRecordType(
    val code: Int,
    val addressBytes: Int,
) {
    A(code = 1, addressBytes = 4),
    AAAA(code = 28, addressBytes = 16),
}

internal data class DnsAddressRecord(
    val address: InetAddress,
    val ttlSeconds: Long,
)

internal data class DnsWireResponse(
    val records: List<DnsAddressRecord>,
    val responseCode: Int,
    val truncated: Boolean,
) {
    val addresses: List<InetAddress>
        get() = records.map(DnsAddressRecord::address).distinct()

    val minimumTtlSeconds: Long
        get() = records.minOfOrNull(DnsAddressRecord::ttlSeconds) ?: DEFAULT_NEGATIVE_TTL_SECONDS

    companion object {
        private const val DEFAULT_NEGATIVE_TTL_SECONDS = 60L
    }
}

internal class DnsProtocolException(message: String) : IOException(message)

internal object DnsPacketCodec {
    fun buildQuery(
        id: Int,
        host: String,
        type: DnsRecordType,
    ): ByteArray {
        require(id in 0..0xffff) { "DNS query id is outside uint16" }
        val asciiHost = IDN.toASCII(host.trim().trimEnd('.')).lowercase()
        require(asciiHost.isNotEmpty()) { "DNS host is empty" }

        return ByteArrayOutputStream().use { buffer ->
            DataOutputStream(buffer).use { output ->
                output.writeShort(id)
                output.writeShort(FLAG_RECURSION_DESIRED)
                output.writeShort(1)
                output.writeShort(0)
                output.writeShort(0)
                output.writeShort(0)
                for (label in asciiHost.split('.')) {
                    val bytes = label.toByteArray(Charsets.US_ASCII)
                    require(bytes.isNotEmpty() && bytes.size <= MAX_LABEL_BYTES) {
                        "Invalid DNS label in $asciiHost"
                    }
                    output.writeByte(bytes.size)
                    output.write(bytes)
                }
                output.writeByte(0)
                output.writeShort(type.code)
                output.writeShort(CLASS_IN)
            }
            buffer.toByteArray()
        }
    }

    fun parseResponse(
        packet: ByteArray,
        expectedId: Int,
        expectedType: DnsRecordType,
    ): DnsWireResponse {
        if (packet.size < DNS_HEADER_BYTES) {
            throw DnsProtocolException("DNS response is shorter than its header")
        }
        val id = uint16(packet, 0)
        if (id != expectedId) {
            throw DnsProtocolException("DNS response id $id does not match query id $expectedId")
        }
        val flags = uint16(packet, 2)
        if ((flags and FLAG_RESPONSE) == 0) {
            throw DnsProtocolException("DNS packet is not a response")
        }
        val truncated = (flags and FLAG_TRUNCATED) != 0
        val responseCode = flags and RESPONSE_CODE_MASK
        if (truncated) {
            return DnsWireResponse(
                records = emptyList(),
                responseCode = responseCode,
                truncated = true,
            )
        }
        val questionCount = uint16(packet, 4)
        val answerCount = uint16(packet, 6)
        var offset = DNS_HEADER_BYTES

        repeat(questionCount) {
            offset = skipName(packet, offset)
            requireAvailable(packet, offset, QUESTION_TRAILER_BYTES)
            offset += QUESTION_TRAILER_BYTES
        }

        val records = mutableListOf<DnsAddressRecord>()
        repeat(answerCount) {
            offset = skipName(packet, offset)
            requireAvailable(packet, offset, ANSWER_FIXED_BYTES)
            val type = uint16(packet, offset)
            val dnsClass = uint16(packet, offset + 2)
            val ttlSeconds = uint32(packet, offset + 4)
            val dataLength = uint16(packet, offset + 8)
            offset += ANSWER_FIXED_BYTES
            requireAvailable(packet, offset, dataLength)
            if (
                type == expectedType.code &&
                dnsClass == CLASS_IN &&
                dataLength == expectedType.addressBytes
            ) {
                records += DnsAddressRecord(
                    address = InetAddress.getByAddress(packet.copyOfRange(offset, offset + dataLength)),
                    ttlSeconds = ttlSeconds,
                )
            }
            offset += dataLength
        }

        return DnsWireResponse(
            records = records,
            responseCode = responseCode,
            truncated = truncated,
        )
    }

    private fun skipName(packet: ByteArray, start: Int): Int {
        var offset = start
        var labels = 0
        while (true) {
            requireAvailable(packet, offset, 1)
            val length = packet[offset].toInt() and 0xff
            when {
                length == 0 -> return offset + 1
                (length and POINTER_MASK) == POINTER_MASK -> {
                    requireAvailable(packet, offset, 2)
                    return offset + 2
                }
                (length and POINTER_MASK) != 0 -> {
                    throw DnsProtocolException("DNS name uses an unsupported label encoding")
                }
                else -> {
                    labels++
                    if (labels > MAX_LABELS) {
                        throw DnsProtocolException("DNS name contains too many labels")
                    }
                    requireAvailable(packet, offset + 1, length)
                    offset += length + 1
                }
            }
        }
    }

    private fun uint16(packet: ByteArray, offset: Int): Int {
        requireAvailable(packet, offset, 2)
        return ((packet[offset].toInt() and 0xff) shl 8) or
            (packet[offset + 1].toInt() and 0xff)
    }

    private fun uint32(packet: ByteArray, offset: Int): Long {
        requireAvailable(packet, offset, 4)
        return ((packet[offset].toLong() and 0xff) shl 24) or
            ((packet[offset + 1].toLong() and 0xff) shl 16) or
            ((packet[offset + 2].toLong() and 0xff) shl 8) or
            (packet[offset + 3].toLong() and 0xff)
    }

    private fun requireAvailable(packet: ByteArray, offset: Int, length: Int) {
        if (offset < 0 || length < 0 || offset > packet.size - length) {
            throw DnsProtocolException("DNS response ended before the current record")
        }
    }

    private const val DNS_HEADER_BYTES = 12
    private const val QUESTION_TRAILER_BYTES = 4
    private const val ANSWER_FIXED_BYTES = 10
    private const val MAX_LABEL_BYTES = 63
    private const val MAX_LABELS = 127
    private const val CLASS_IN = 1
    private const val FLAG_RECURSION_DESIRED = 0x0100
    private const val FLAG_RESPONSE = 0x8000
    private const val FLAG_TRUNCATED = 0x0200
    private const val RESPONSE_CODE_MASK = 0x000f
    private const val POINTER_MASK = 0xc0
}
