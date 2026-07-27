package com.msitools.anyconnectmobile.routeprobe.packet

import java.io.ByteArrayInputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class PacketDestinationParserTest {
    @Test
    fun readsIpv4Destination() {
        val packet = ByteArray(20)
        packet[0] = 0x45
        byteArrayOf(8, 8, 8, 8).copyInto(packet, destinationOffset = 16)

        assertEquals(
            "8.8.8.8",
            PacketDestinationParser.destination(packet, packet.size)?.hostAddress,
        )
    }

    @Test
    fun readsIpv6Destination() {
        val packet = ByteArray(40)
        packet[0] = 0x60
        val expected = InetAddress.getByName("2001:4860:4860::8888")
        expected.address.copyInto(packet, destinationOffset = 24)

        assertEquals(expected, PacketDestinationParser.destination(packet, packet.size))
    }

    @Test
    fun preservesIpv4MappedIpv6DestinationAsSixteenBytes() {
        val packet = ByteArray(40)
        packet[0] = 0x60
        val mapped = ByteArray(16).also { bytes ->
            bytes[10] = 0xff.toByte()
            bytes[11] = 0xff.toByte()
            byteArrayOf(192.toByte(), 0, 2, 1).copyInto(bytes, destinationOffset = 12)
        }
        mapped.copyInto(packet, destinationOffset = 24)

        val destination = requireNotNull(PacketDestinationParser.destination(packet, packet.size))
        assertEquals(16, destination.address.size)
        assertArrayEquals(mapped, destination.address)
    }

    @Test
    fun rejectsNullUnknownTruncatedAndInvalidLengths() {
        assertNull(PacketDestinationParser.destination(null, 0))
        assertNull(PacketDestinationParser.destination(byteArrayOf(0x30), 1))
        assertNull(PacketDestinationParser.destination(ByteArray(19).also { it[0] = 0x45 }, 19))
        assertNull(PacketDestinationParser.destination(ByteArray(39).also { it[0] = 0x60 }, 39))
        assertNull(PacketDestinationParser.destination(ByteArray(20).also { it[0] = 0x45 }, 21))
        assertNull(PacketDestinationParser.destination(ByteArray(40).also { it[0] = 0x60 }, 41))
        assertNull(PacketDestinationParser.destination(ByteArray(20).also { it[0] = 0x45 }, -1))
    }

    @Test
    fun nonceSubsequenceMatchingHonorsBothPacketBoundaries() {
        val packet = byteArrayOf(1, 2, 3, 4)

        assertEquals(0, packet.indexOfSubsequence(byteArrayOf(1, 2)))
        assertEquals(2, packet.indexOfSubsequence(byteArrayOf(3, 4)))
        assertEquals(-1, packet.indexOfSubsequence(byteArrayOf(4, 5)))
        assertEquals(-1, byteArrayOf(1).indexOfSubsequence(byteArrayOf(1, 2)))
        assertEquals(0, packet.indexOfSubsequence(byteArrayOf()))
    }

    @Test
    fun tunCaptureMatchesDestinationAndNonceAndClosesItsOwnerOnlyOnce() {
        val destination = InetAddress.getByName("8.8.8.8")
        val nonce = byteArrayOf(9, 8, 7, 6)
        val packet = ByteArray(24)
        packet[0] = 0x45
        destination.address.copyInto(packet, destinationOffset = 16)
        nonce.copyInto(packet, destinationOffset = 20)
        val closeCount = AtomicInteger()
        val capture = TunCapture(ByteArrayInputStream(packet)) { closeCount.incrementAndGet() }

        val deadline = System.nanoTime() + 1_000_000_000L
        while (!capture.captured(destination, nonce) && System.nanoTime() < deadline) {
            Thread.yield()
        }

        assertTrue(capture.captured(destination, nonce))
        assertFalse(capture.captured(InetAddress.getByName("1.1.1.1"), nonce))
        assertFalse(capture.captured(destination, byteArrayOf(8, 7, 6, 5)))
        capture.close()
        capture.close()
        assertEquals(1, closeCount.get())
    }

    @Test
    fun tunCaptureSurfacesIOExceptionRaisedWhileReaderIsRunning() {
        val readAttempted = CountDownLatch(1)
        val failure = IOException("reader failed")
        val input = object : InputStream() {
            override fun read(): Int {
                readAttempted.countDown()
                throw failure
            }
        }
        val capture = TunCapture(input) {}
        assertTrue(readAttempted.await(1, TimeUnit.SECONDS))

        val deadline = System.nanoTime() + 1_000_000_000L
        var observed: IOException? = null
        while (observed == null && System.nanoTime() < deadline) {
            try {
                capture.captured(InetAddress.getByName("8.8.8.8"), byteArrayOf(1))
            } catch (error: IOException) {
                observed = error
            }
            Thread.yield()
        }

        assertEquals(failure, observed)
        capture.close()
    }

    @Test
    fun tunCaptureSurfacesUnexpectedEofBeforeTheTargetArrives() {
        val capture = TunCapture(ByteArrayInputStream(byteArrayOf())) {}
        assertTrue(capture.awaitReaderStarted(1_000))

        val failure = assertThrows(EOFException::class.java) {
            capture.await(
                InetAddress.getByName("8.8.8.8"),
                byteArrayOf(1),
                1_000,
            )
        }

        assertEquals("TUN reader reached EOF while capture was active", failure.message)
        capture.close()
    }

    @Test
    fun tunCaptureWrapsUncheckedReaderFailureInsteadOfReturningFalse() {
        val original = IllegalStateException("unchecked reader failure")
        val readAttempted = CountDownLatch(1)
        val input = object : InputStream() {
            override fun read(): Int {
                readAttempted.countDown()
                throw original
            }
        }
        val capture = TunCapture(input) {}
        assertTrue(readAttempted.await(1, TimeUnit.SECONDS))

        val failure = assertThrows(IOException::class.java) {
            capture.await(
                InetAddress.getByName("8.8.8.8"),
                byteArrayOf(1),
                1_000,
            )
        }

        assertEquals(original, failure.cause)
        assertTrue(failure.message.orEmpty().contains("java.lang.IllegalStateException"))
        capture.close()
    }

    @Test
    fun tunCaptureReportsFailureDuringReaderInitialization() {
        val original = IllegalStateException("buffer initialization failed")
        val capture = TunCapture(
            input = ByteArrayInputStream(byteArrayOf()),
            bufferFactory = { throw original },
            closeOwnedDescriptor = {},
        )
        assertTrue(capture.awaitReaderStarted(1_000))

        val failure = assertThrows(IOException::class.java) {
            capture.await(
                InetAddress.getByName("8.8.8.8"),
                byteArrayOf(1),
                1_000,
            )
        }

        assertEquals(original, failure.cause)
        assertTrue(capture.diagnostics(InetAddress.getByName("8.8.8.8"), byteArrayOf(1)).readerFinished)
        capture.close()
    }

    @Test
    fun tunCaptureDiagnosticsDistinguishReadParsedAndMatchedPackets() {
        val destination = InetAddress.getByName("8.8.8.8")
        val nonce = byteArrayOf(9, 8, 7, 6)
        val packet = ByteArray(24).also { bytes ->
            bytes[0] = 0x45
            destination.address.copyInto(bytes, destinationOffset = 16)
            nonce.copyInto(bytes, destinationOffset = 20)
        }
        val capture = TunCapture(ByteArrayInputStream(packet)) {}
        assertTrue(capture.awaitReaderStarted(1_000))
        assertTrue(capture.await(destination, nonce, 1_000))

        val diagnostics = capture.diagnostics(destination, nonce)
        assertTrue(diagnostics.readerStarted)
        assertEquals(1, diagnostics.readCount)
        assertEquals(1, diagnostics.parsedCount)
        assertTrue(diagnostics.destinationSeen)
        assertTrue(diagnostics.nonceSeen)
        assertTrue(diagnostics.exactPacketSeen)
        capture.close()
    }

    @Test
    fun tunCaptureTreatsDescriptorClosureIOExceptionAsNormalTermination() {
        val readStarted = CountDownLatch(1)
        val descriptorClosed = CountDownLatch(1)
        val closeCount = AtomicInteger()
        val input = object : InputStream() {
            override fun read(): Int {
                readStarted.countDown()
                try {
                    descriptorClosed.await()
                } catch (error: InterruptedException) {
                    throw IOException("descriptor closed", error)
                }
                throw IOException("descriptor closed")
            }
        }
        val capture = TunCapture(input) {
            closeCount.incrementAndGet()
            descriptorClosed.countDown()
        }
        assertTrue(readStarted.await(1, TimeUnit.SECONDS))

        capture.close()
        capture.close()

        assertEquals(1, closeCount.get())
        assertTrue(
            capture.diagnostics(InetAddress.getByName("8.8.8.8"), byteArrayOf(1)).readerFinished,
        )
    }
}
