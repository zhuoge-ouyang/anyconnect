package com.msitools.anyconnectmobile.service

import java.net.InetAddress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DnsPacketCodecTest {
    @Test
    fun buildsStandardRecursiveQuery() {
        val query = DnsPacketCodec.buildQuery(
            id = 0x1234,
            host = "www.google.com",
            type = DnsRecordType.A,
        )

        assertEquals(0x12, query[0].toInt() and 0xff)
        assertEquals(0x34, query[1].toInt() and 0xff)
        assertEquals(0x01, query[2].toInt() and 0xff)
        assertEquals(1, query[5].toInt() and 0xff)
        assertTrue(query.copyOfRange(12, query.size).containsSlice("google".toByteArray()))
    }

    @Test
    fun parsesCompressedIpv4AnswerAndTtl() {
        val query = DnsPacketCodec.buildQuery(0x2233, "www.google.com", DnsRecordType.A)
        val response = responseWithAnswer(
            query = query,
            ttlSeconds = 120,
            rdata = byteArrayOf(8, 8, 8, 8),
        )

        val parsed = DnsPacketCodec.parseResponse(
            packet = response,
            expectedId = 0x2233,
            expectedType = DnsRecordType.A,
        )

        assertFalse(parsed.truncated)
        assertEquals(0, parsed.responseCode)
        assertEquals(listOf(InetAddress.getByName("8.8.8.8")), parsed.addresses)
        assertEquals(120L, parsed.minimumTtlSeconds)
    }

    @Test
    fun reportsTruncatedResponseWithoutAcceptingPartialRoutes() {
        val query = DnsPacketCodec.buildQuery(0x3344, "chatgpt.com", DnsRecordType.A)
        val response = query.copyOf().apply {
            this[2] = 0x83.toByte()
            this[3] = 0x80.toByte()
        }

        val parsed = DnsPacketCodec.parseResponse(
            packet = response,
            expectedId = 0x3344,
            expectedType = DnsRecordType.A,
        )

        assertTrue(parsed.truncated)
        assertTrue(parsed.addresses.isEmpty())
    }

    @Test
    fun truncatedHeaderTriggersTcpRetryEvenWhenUdpBodyIsIncomplete() {
        val response = bytes(
            0x35, 0x45,
            0x82, 0x00,
            0x00, 0x01,
            0x00, 0x01,
            0x00, 0x00,
            0x00, 0x00,
        )

        val parsed = DnsPacketCodec.parseResponse(
            packet = response,
            expectedId = 0x3545,
            expectedType = DnsRecordType.A,
        )

        assertTrue(parsed.truncated)
        assertTrue(parsed.addresses.isEmpty())
    }

    @Test(expected = DnsProtocolException::class)
    fun rejectsResponseForDifferentQueryId() {
        val query = DnsPacketCodec.buildQuery(0x4455, "github.com", DnsRecordType.A)
        val response = responseWithAnswer(
            query = query,
            ttlSeconds = 30,
            rdata = byteArrayOf(1, 1, 1, 1),
        )

        DnsPacketCodec.parseResponse(
            packet = response,
            expectedId = 0x4456,
            expectedType = DnsRecordType.A,
        )
    }

    private fun responseWithAnswer(
        query: ByteArray,
        ttlSeconds: Int,
        rdata: ByteArray,
    ): ByteArray {
        val responseHeaderAndQuestion = query.copyOf().apply {
            this[2] = 0x81.toByte()
            this[3] = 0x80.toByte()
            this[6] = 0
            this[7] = 1
        }
        val answer = bytes(
            0xc0, 0x0c,
            0x00, 0x01,
            0x00, 0x01,
            ttlSeconds ushr 24,
            ttlSeconds ushr 16,
            ttlSeconds ushr 8,
            ttlSeconds,
            0x00, rdata.size,
            *rdata.map { it.toInt() and 0xff }.toIntArray(),
        )
        return responseHeaderAndQuestion + answer
    }

    private fun bytes(vararg values: Int): ByteArray =
        ByteArray(values.size) { index -> values[index].toByte() }

    private fun ByteArray.containsSlice(needle: ByteArray): Boolean =
        indices.any { start ->
            start + needle.size <= size &&
                needle.indices.all { offset -> this[start + offset] == needle[offset] }
        }
}
