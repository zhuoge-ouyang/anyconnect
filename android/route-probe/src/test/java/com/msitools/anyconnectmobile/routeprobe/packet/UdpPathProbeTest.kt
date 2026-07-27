package com.msitools.anyconnectmobile.routeprobe.packet

import java.io.ByteArrayInputStream
import java.io.DataInputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UdpPathProbeTest {
    @Test
    fun buildsExactlyOneNonceTaggedInvalidAQuestion() {
        val query = UdpPathProbe.buildDnsQuery(
            id = 0x1234,
            name = "probe-00112233445566778899aabbccddeeff.invalid",
            type = 1,
        )

        val decoded = decodeSingleQuestion(query)
        assertEquals(0x1234, decoded.id)
        assertEquals(0x0100, decoded.flags)
        assertEquals(1, decoded.questionCount)
        assertEquals(0, decoded.answerCount)
        assertEquals(0, decoded.authorityCount)
        assertEquals(0, decoded.additionalCount)
        assertEquals("probe-00112233445566778899aabbccddeeff.invalid", decoded.name)
        assertEquals(1, decoded.type)
        assertEquals(1, decoded.queryClass)
        assertEquals(query.size, decoded.consumedBytes)
    }

    @Test
    fun buildsExactlyOneNonceTaggedInvalidAaaaQuestion() {
        val query = UdpPathProbe.buildDnsQuery(
            id = 0xabcd,
            name = "probe-ffeeddccbbaa99887766554433221100.invalid",
            type = 28,
        )

        val decoded = decodeSingleQuestion(query)
        assertEquals(0xabcd, decoded.id)
        assertEquals(1, decoded.questionCount)
        assertEquals("probe-ffeeddccbbaa99887766554433221100.invalid", decoded.name)
        assertEquals(28, decoded.type)
        assertEquals(query.size, decoded.consumedBytes)
    }

    @Test
    fun responseMatchingRequiresActualResponseAndMatchingTransactionId() {
        val matchingResponse = ByteArray(12).also {
            it[0] = 0x12
            it[1] = 0x34
            it[2] = 0x80.toByte()
        }
        val sameIdQuery = matchingResponse.copyOf().also { it[2] = 0x01 }
        val wrongIdResponse = matchingResponse.copyOf().also { it[1] = 0x35 }

        assertTrue(UdpPathProbe.responseMatches(matchingResponse, matchingResponse.size, 0x1234))
        assertFalse(UdpPathProbe.responseMatches(sameIdQuery, sameIdQuery.size, 0x1234))
        assertFalse(UdpPathProbe.responseMatches(wrongIdResponse, wrongIdResponse.size, 0x1234))
        assertFalse(UdpPathProbe.responseMatches(byteArrayOf(0x12), 1, 0x1234))
        assertFalse(UdpPathProbe.responseMatches(ByteArray(12), 13, 0))
    }

    private fun decodeSingleQuestion(query: ByteArray): DecodedQuestion {
        val input = DataInputStream(ByteArrayInputStream(query))
        val id = input.readUnsignedShort()
        val flags = input.readUnsignedShort()
        val questionCount = input.readUnsignedShort()
        val answerCount = input.readUnsignedShort()
        val authorityCount = input.readUnsignedShort()
        val additionalCount = input.readUnsignedShort()
        val labels = buildList {
            while (true) {
                val length = input.readUnsignedByte()
                if (length == 0) break
                val label = ByteArray(length)
                input.readFully(label)
                add(label.toString(Charsets.US_ASCII))
            }
        }
        val type = input.readUnsignedShort()
        val queryClass = input.readUnsignedShort()
        return DecodedQuestion(
            id = id,
            flags = flags,
            questionCount = questionCount,
            answerCount = answerCount,
            authorityCount = authorityCount,
            additionalCount = additionalCount,
            name = labels.joinToString("."),
            type = type,
            queryClass = queryClass,
            consumedBytes = query.size - input.available(),
        )
    }

    private data class DecodedQuestion(
        val id: Int,
        val flags: Int,
        val questionCount: Int,
        val answerCount: Int,
        val authorityCount: Int,
        val additionalCount: Int,
        val name: String,
        val type: Int,
        val queryClass: Int,
        val consumedBytes: Int,
    )
}
