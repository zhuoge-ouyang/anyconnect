package com.msitools.anyconnectmobile.routeprobesender

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SenderRequestTest {
    @Test
    fun parsesNumericIpv6DestinationAndSixteenByteNonce() {
        val request = SenderRequest.parse(
            destination = "2001:4860:4860::8888",
            nonce = ByteArray(16) { 7 },
        ).getOrThrow()

        assertEquals("2001:4860:4860:0:0:0:0:8888", request.destination.hostAddress)
        assertEquals(List(16) { 7.toByte() }, request.nonce.toList())
    }

    @Test
    fun rejectsNonNumericDestinationsBeforeAnyDnsLookup() {
        val error = SenderRequest.parse(
            destination = "example.com",
            nonce = ByteArray(16),
        ).exceptionOrNull()

        assertTrue(error is IllegalArgumentException)
        assertTrue(error?.message.orEmpty().contains("numeric IP"))
    }

    @Test
    fun rejectsWrongNonceSize() {
        val error = SenderRequest.parse(
            destination = "8.8.8.8",
            nonce = ByteArray(15),
        ).exceptionOrNull()

        assertTrue(error is IllegalArgumentException)
        assertTrue(error?.message.orEmpty().contains("16 bytes"))
    }
}
