package com.msitools.anyconnectmobile.routeprobe.packet

import android.os.ParcelFileDescriptor
import java.io.Closeable
import java.io.EOFException
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

class TunCapture internal constructor(
    private val input: InputStream,
    private val bufferFactory: () -> ByteArray = { ByteArray(MAX_PACKET_BYTES) },
    private val closeOwnedDescriptor: () -> Unit,
) : Closeable {
    private val packets = ConcurrentLinkedQueue<CapturedPacket>()
    private val reads = ConcurrentLinkedQueue<ByteArray>()
    private val running = AtomicBoolean(true)
    private val closed = AtomicBoolean(false)
    private val readerFailure = AtomicReference<IOException?>()
    private val readerStarted = CountDownLatch(1)
    private val readerFinished = AtomicBoolean(false)
    private val unexpectedEof = AtomicBoolean(false)
    private val readCount = AtomicInteger()
    private val parsedCount = AtomicInteger()
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "route-probe-tun-capture").apply { isDaemon = true }
    }
    private val future = executor.submit {
        readerStarted.countDown()
        try {
            val buffer = bufferFactory()
            while (running.get()) {
                val count = input.read(buffer)
                if (count < 0) {
                    unexpectedEof.set(true)
                    throw EOFException("TUN reader reached EOF while capture was active")
                }
                if (count == 0) continue
                val bytes = buffer.copyOf(count)
                reads += bytes
                readCount.incrementAndGet()
                PacketDestinationParser.destination(bytes, count)?.let { destination ->
                    parsedCount.incrementAndGet()
                    packets += CapturedPacket(
                        destination = requireNotNull(destination.hostAddress),
                        bytes = bytes,
                    )
                }
            }
        } catch (error: Throwable) {
            if (running.get()) {
                readerFailure.compareAndSet(null, error.asReaderIOException())
            }
        } finally {
            readerFinished.set(true)
        }
    }

    constructor(pfd: ParcelFileDescriptor) : this(
        input = FileInputStream(pfd.fileDescriptor),
        closeOwnedDescriptor = { pfd.close() },
    )

    fun awaitReaderStarted(timeoutMs: Long): Boolean {
        require(timeoutMs >= 0) { "timeoutMs must not be negative" }
        return readerStarted.await(timeoutMs, TimeUnit.MILLISECONDS)
    }

    fun await(destination: InetAddress, nonce: ByteArray, timeoutMs: Long): Boolean {
        require(timeoutMs >= 0) { "timeoutMs must not be negative" }
        val startedAtNanos = System.nanoTime()
        val timeoutNanos = TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        while (System.nanoTime() - startedAtNanos < timeoutNanos) {
            if (captured(destination, nonce)) return true
            throwReaderFailure()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        if (captured(destination, nonce)) return true
        throwReaderFailure()
        return false
    }

    fun captured(destination: InetAddress, nonce: ByteArray): Boolean {
        val matched = packets.any { packet ->
            packet.destination == destination.hostAddress &&
                packet.bytes.indexOfSubsequence(nonce) >= 0
        }
        if (matched) return true
        throwReaderFailure(includeUnexpectedEof = false)
        return false
    }

    fun diagnostics(destination: InetAddress, nonce: ByteArray): TunCaptureDiagnostics {
        val destinationAddress = destination.hostAddress
        val destinationSeen = packets.any { it.destination == destinationAddress }
        val nonceSeen = reads.any { it.indexOfSubsequence(nonce) >= 0 }
        return TunCaptureDiagnostics(
            readerStarted = readerStarted.count == 0L,
            readerFinished = readerFinished.get(),
            unexpectedEof = unexpectedEof.get(),
            readCount = readCount.get(),
            parsedCount = parsedCount.get(),
            destinationSeen = destinationSeen,
            nonceSeen = nonceSeen,
            exactPacketSeen = packets.any { packet ->
                packet.destination == destinationAddress &&
                    packet.bytes.indexOfSubsequence(nonce) >= 0
            },
            readerFailureClass = readerFailure.get()?.javaClass?.name,
            readerFailureMessage = readerFailure.get()?.message,
        )
    }

    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        running.set(false)
        var closeFailure: Throwable? = null
        try {
            closeOwnedDescriptor()
        } catch (error: Throwable) {
            closeFailure = error
        } finally {
            future.cancel(true)
            executor.shutdownNow()
            try {
                if (!executor.awaitTermination(READER_CLOSE_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                    closeFailure = closeFailure.mergeCloseFailure(
                        IOException(
                            "TUN reader did not terminate within ${READER_CLOSE_TIMEOUT_MS}ms",
                        ),
                    )
                }
            } catch (error: InterruptedException) {
                Thread.currentThread().interrupt()
                closeFailure = closeFailure.mergeCloseFailure(
                    IOException("Interrupted while waiting for TUN reader termination", error),
                )
            }
        }
        closeFailure?.let { throw it }
    }

    private fun throwReaderFailure(includeUnexpectedEof: Boolean = true) {
        readerFailure.get()?.let { failure ->
            if (includeUnexpectedEof || failure !is EOFException) throw failure
        }
    }

    private companion object {
        const val MAX_PACKET_BYTES = 65_535
        const val POLL_INTERVAL_MS = 25L
        const val READER_CLOSE_TIMEOUT_MS = 1_000L
    }
}

data class CapturedPacket(val destination: String, val bytes: ByteArray)

data class TunCaptureDiagnostics(
    val readerStarted: Boolean,
    val readerFinished: Boolean,
    val unexpectedEof: Boolean,
    val readCount: Int,
    val parsedCount: Int,
    val destinationSeen: Boolean,
    val nonceSeen: Boolean,
    val exactPacketSeen: Boolean,
    val readerFailureClass: String?,
    val readerFailureMessage: String?,
)

private fun Throwable.asReaderIOException(): IOException = when (this) {
    is IOException -> this
    else -> IOException("TUN reader failed with ${javaClass.name}: $message", this)
}

private fun Throwable?.mergeCloseFailure(next: Throwable): Throwable = when {
    this == null -> next
    this === next -> this
    else -> apply { addSuppressed(next) }
}

internal fun ByteArray.indexOfSubsequence(needle: ByteArray): Int {
    if (needle.isEmpty()) return 0
    if (needle.size > size) return -1
    for (start in 0..size - needle.size) {
        var matches = true
        for (offset in needle.indices) {
            if (this[start + offset] != needle[offset]) {
                matches = false
                break
            }
        }
        if (matches) return start
    }
    return -1
}
