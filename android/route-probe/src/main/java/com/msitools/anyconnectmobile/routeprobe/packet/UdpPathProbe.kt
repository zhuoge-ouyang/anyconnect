package com.msitools.anyconnectmobile.routeprobe.packet

import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import java.io.IOException
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress

enum class DirectResponseStatus { PASS, INCONCLUSIVE }

data class DirectProbeResult(
    val status: DirectResponseStatus,
    val wireNonce: ByteArray,
    val detail: String,
)

object UdpPathProbe {
    fun verifyDirectDns(address: InetAddress, nonce: ByteArray): DirectProbeResult {
        require(nonce.size == NONCE_BYTES) { "DNS probe nonce must be exactly $NONCE_BYTES bytes" }
        val addressBytes = address.address
        require(addressBytes.size == IPV4_BYTES || addressBytes.size == IPV6_BYTES) {
            "DNS probe address must be IPv4 or IPv6"
        }

        val transactionId =
            ((nonce[0].toInt() and 0xff) shl 8) or (nonce[1].toInt() and 0xff)
        val label = "probe-" + nonce.joinToString("") {
            "%02x".format(it.toInt() and 0xff)
        }
        val wireNonce = label.toByteArray(Charsets.US_ASCII)
        val queryType = if (addressBytes.size == IPV4_BYTES) DNS_TYPE_A else DNS_TYPE_AAAA
        val query = buildDnsQuery(transactionId, "$label.invalid", queryType)

        return try {
            DatagramSocket().use { socket ->
                socket.soTimeout = SOCKET_TIMEOUT_MS
                socket.connect(InetSocketAddress(address, DNS_PORT))
                socket.send(DatagramPacket(query, query.size))
                val response = ByteArray(MAX_DNS_RESPONSE_BYTES)
                val packet = DatagramPacket(response, response.size)
                socket.receive(packet)
                if (responseMatches(response, packet.length, transactionId)) {
                    DirectProbeResult(
                        status = DirectResponseStatus.PASS,
                        wireNonce = wireNonce,
                        detail = "matching DNS response",
                    )
                } else {
                    DirectProbeResult(
                        status = DirectResponseStatus.INCONCLUSIVE,
                        wireNonce = wireNonce,
                        detail = "DNS response validation failed",
                    )
                }
            }
        } catch (error: IOException) {
            DirectProbeResult(
                status = DirectResponseStatus.INCONCLUSIVE,
                wireNonce = wireNonce,
                detail = error.javaClass.simpleName + ": " + error.message,
            )
        }
    }

    internal fun responseMatches(response: ByteArray, length: Int, id: Int): Boolean {
        if (length < DNS_HEADER_BYTES || length > response.size) return false
        val responseId =
            ((response[0].toInt() and 0xff) shl 8) or
                (response[1].toInt() and 0xff)
        val isResponse = response[2].toInt() and DNS_RESPONSE_FLAG != 0
        return isResponse && responseId == id
    }

    internal fun buildDnsQuery(id: Int, name: String, type: Int): ByteArray {
        require(id in 0..0xffff) { "DNS transaction ID is outside the unsigned 16-bit range" }
        require(type == DNS_TYPE_A || type == DNS_TYPE_AAAA) { "DNS probe must request A or AAAA" }
        require(PROBE_NAME.matches(name)) { "DNS probe name must be a nonce label under .invalid" }

        val output = ByteArrayOutputStream()
        DataOutputStream(output).use { data ->
            data.writeShort(id)
            data.writeShort(DNS_RECURSION_DESIRED)
            data.writeShort(1)
            data.writeShort(0)
            data.writeShort(0)
            data.writeShort(0)
            name.split('.').forEach { label ->
                val bytes = label.toByteArray(Charsets.US_ASCII)
                require(bytes.size in 1..MAX_DNS_LABEL_BYTES)
                data.writeByte(bytes.size)
                data.write(bytes)
            }
            data.writeByte(0)
            data.writeShort(type)
            data.writeShort(DNS_CLASS_IN)
        }
        return output.toByteArray()
    }

    private const val NONCE_BYTES = 16
    private const val IPV4_BYTES = 4
    private const val IPV6_BYTES = 16
    private const val DNS_PORT = 53
    private const val SOCKET_TIMEOUT_MS = 1_500
    private const val MAX_DNS_RESPONSE_BYTES = 2_048
    private const val DNS_HEADER_BYTES = 12
    private const val DNS_RESPONSE_FLAG = 0x80
    private const val DNS_RECURSION_DESIRED = 0x0100
    private const val DNS_TYPE_A = 1
    private const val DNS_TYPE_AAAA = 28
    private const val DNS_CLASS_IN = 1
    private const val MAX_DNS_LABEL_BYTES = 63
    private val PROBE_NAME = Regex("probe-[0-9a-f]{32}\\.invalid")
}
