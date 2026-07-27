package com.msitools.anyconnectmobile.routeprobe.service

import android.annotation.TargetApi
import android.content.Context
import android.net.ConnectivityManager
import android.net.IpPrefix
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import android.system.Os
import android.system.OsConstants
import com.msitools.anyconnectmobile.routeprobe.data.ProbeIpDb
import com.msitools.anyconnectmobile.routeprobe.net.Cidr
import com.msitools.anyconnectmobile.routeprobe.net.CidrMath
import com.msitools.anyconnectmobile.routeprobe.net.IpFamily
import com.msitools.anyconnectmobile.routeprobe.net.ProbeScenario
import com.msitools.anyconnectmobile.routeprobe.net.RoutePlan
import com.msitools.anyconnectmobile.routeprobe.net.RoutePlanFactory
import com.msitools.anyconnectmobile.routeprobe.packet.DirectProbeResult
import com.msitools.anyconnectmobile.routeprobe.packet.DirectResponseStatus
import com.msitools.anyconnectmobile.routeprobe.packet.TunCapture
import com.msitools.anyconnectmobile.routeprobe.packet.UdpPathProbe
import com.msitools.anyconnectmobile.routeprobe.report.ProbeDeviceMetadata
import com.msitools.anyconnectmobile.routeprobe.report.GateDecision
import com.msitools.anyconnectmobile.routeprobe.report.CheckpointPhase
import com.msitools.anyconnectmobile.routeprobe.report.ProbeCheckpoint
import com.msitools.anyconnectmobile.routeprobe.report.ProbeIpDbMetadata
import com.msitools.anyconnectmobile.routeprobe.report.ProbeReport
import com.msitools.anyconnectmobile.routeprobe.report.ProbeReportStore
import com.msitools.anyconnectmobile.routeprobe.report.ProbeScenarioResult
import java.io.Closeable
import java.io.File
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.Inet6Address
import java.security.MessageDigest
import java.net.InetAddress
import java.net.InetSocketAddress
import java.security.SecureRandom
import java.util.UUID
import java.util.concurrent.CancellationException
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

internal enum class EstablishStatus { SUCCESS, NULL, EXCEPTION }

internal enum class DirectPathStatus { PASS, INCONCLUSIVE }

internal interface ProbeBuilderFacade {
    fun setSession(value: String)
    fun setMtu(value: Int)
    fun setBlocking(value: Boolean)
    fun addAddress(address: String, prefixLength: Int)
    fun allowFamily(family: IpFamily)
    fun addRoute(cidr: Cidr)
    fun excludeRoute(cidr: Cidr)
    fun establish(): ParcelFileDescriptor?
}

internal fun applyRoutePlan(builder: ProbeBuilderFacade, plan: RoutePlan, sdkInt: Int) {
    builder.setSession("AnyConnect Route Probe: " + plan.scenario.name)
    builder.setMtu(1_500)
    builder.setBlocking(true)

    if (IpFamily.IPV4 in plan.scenario.families) {
        builder.addAddress("10.252.0.2", 32)
    } else {
        builder.allowFamily(IpFamily.IPV4)
    }
    if (IpFamily.IPV6 in plan.scenario.families) {
        builder.addAddress("fd00:252::2", 128)
    } else {
        builder.allowFamily(IpFamily.IPV6)
    }
    plan.includes.forEach(builder::addRoute)
    if (plan.excludes.isNotEmpty()) {
        require(sdkInt >= 33) { "excludeRoute requires Android API 33 or newer" }
        plan.excludes.forEach(builder::excludeRoute)
    }
}

internal fun normalizedPlanSha256(plan: RoutePlan): String {
    val canonical = buildString {
        append("scenario=").append(plan.scenario.name).append('\n')
        append("strategy=").append(plan.strategy.name).append('\n')
        append("families=")
            .append(plan.scenario.families.sorted().joinToString(",") { it.name })
            .append('\n')
        plan.includes.sorted().forEach { append("include=").append(it).append('\n') }
        plan.excludes.sorted().forEach { append("exclude=").append(it).append('\n') }
    }
    return MessageDigest.getInstance("SHA-256")
        .digest(canonical.toByteArray(Charsets.UTF_8))
        .joinToString("") { "%02X".format(it.toInt() and 0xff) }
}

internal data class ProcessMetrics(
    val rssKb: Int,
    val highWaterKb: Int,
    val openFdCount: Int,
)

internal data class ProbeEstablishMetadata(
    val runId: String,
    val request: ProbeAttemptRequest,
    val plan: RoutePlan,
    val planSha256: String,
)

internal fun interface ProbeCheckpointWriter {
    fun append(checkpoint: ProbeCheckpoint)
}

internal interface ProbeCaptureSession : Closeable {
    fun awaitReaderStarted(timeoutMs: Long): Boolean = true
    fun await(destination: InetAddress, nonce: ByteArray, timeoutMs: Long): Boolean
    fun captured(destination: InetAddress, nonce: ByteArray): Boolean
    fun diagnosticSummary(destination: InetAddress, nonce: ByteArray): String =
        "capture diagnostics unavailable"
}

internal data class ProbeReadinessBaseline(
    val activeOwnedVpnIdentity: VpnRouteIdentity?,
    val requiresFreshVpnIdentity: Boolean,
) {
    init {
        require(!requiresFreshVpnIdentity || activeOwnedVpnIdentity != null) {
            "A fresh VPN identity cannot be required without a baseline identity"
        }
    }
}

internal interface ProbeAttemptReadiness {
    fun beforeEstablish(plan: RoutePlan): ProbeReadinessBaseline
    fun await(
        plan: RoutePlan,
        capture: ProbeCaptureSession,
        baseline: ProbeReadinessBaseline,
    )
}

internal fun interface CapturedDatagramSender {
    fun send(address: InetAddress, nonce: ByteArray): CapturedProbeSendObservation
}

internal data class CapturedProbeSendObservation(
    val sourceAddress: InetAddress?,
) {
    fun diagnosticFor(destination: InetAddress): String {
        val targetBytes = destination.address
        val expectedTunAddress = when (targetBytes.size) {
            IPV4_ADDRESS_BYTES -> IPV4_TUN_ADDRESS
            IPV6_ADDRESS_BYTES -> IPV6_TUN_ADDRESS
            else -> return "senderSource=UNAVAILABLE senderFamily=UNKNOWN"
        }
        val source = sourceAddress
            ?: return "senderSource=UNAVAILABLE senderFamily=UNKNOWN"
        val family = when (source.address.size) {
            IPV4_ADDRESS_BYTES -> "IPV4"
            IPV6_ADDRESS_BYTES -> "IPV6"
            else -> "UNKNOWN"
        }
        val sourceKind = if (source.address.contentEquals(expectedTunAddress)) {
            "VPN_TUN"
        } else {
            "NON_VPN"
        }
        return "senderSource=$sourceKind senderFamily=$family"
    }

    companion object {
        val UNAVAILABLE = CapturedProbeSendObservation(null)
        private const val IPV4_ADDRESS_BYTES = 4
        private const val IPV6_ADDRESS_BYTES = 16
        private val IPV4_TUN_ADDRESS = byteArrayOf(10, 252.toByte(), 0, 2)
        private val IPV6_TUN_ADDRESS = byteArrayOf(
            0xfd.toByte(), 0x00, 0x02, 0x52,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x02,
        )
    }
}

internal fun interface DirectDnsVerifier {
    fun verify(address: InetAddress, nonce: ByteArray): DirectProbeResult
}

internal enum class HelperProbeSendStatus { SUCCESS, FAILED, UNAVAILABLE }

internal fun interface HelperProbeSender {
    fun send(address: InetAddress, nonce: ByteArray): HelperProbeSendResult
}

internal data class HelperProbeSendResult(
    val status: HelperProbeSendStatus,
    val detail: String?,
) {
    fun diagnostic(captured: Boolean?): String = buildString {
        append("helperSend=").append(status.name)
        when (status) {
            HelperProbeSendStatus.SUCCESS -> {
                append(" helperCaptured=").append(captured ?: false)
            }

            HelperProbeSendStatus.FAILED,
            HelperProbeSendStatus.UNAVAILABLE,
            -> {
                detail?.let { append(" helperDetail=").append(it.asSingleToken()) }
                append(" helperCaptured=NOT_SENT")
            }
        }
    }

    companion object {
        fun success(): HelperProbeSendResult =
            HelperProbeSendResult(HelperProbeSendStatus.SUCCESS, null)

        fun unavailable(detail: String): HelperProbeSendResult =
            HelperProbeSendResult(HelperProbeSendStatus.UNAVAILABLE, detail)

        fun failed(error: Exception): HelperProbeSendResult =
            HelperProbeSendResult(
                HelperProbeSendStatus.FAILED,
                error.javaClass.name + ":" + error.message.orEmpty(),
            )
    }
}

internal data class PathVerificationResult(
    val capturedChecksPassed: Boolean,
    val bypassChecksPassed: Boolean,
    val bypassPathCaptured: Boolean,
    val directResponseStatus: DirectPathStatus,
    val helperCapturedOwnerUidMiss: Boolean = false,
    val captureError: ProbeCaptureMissException? = null,
)

internal class ProbeCaptureMissException(message: String) : Exception(message)

internal class ProbePathVerifier(
    private val nonceSource: () -> ByteArray,
    private val capturedSender: CapturedDatagramSender,
    private val directDnsVerifier: DirectDnsVerifier,
    private val helperSender: HelperProbeSender? = null,
) {
    fun verify(plan: RoutePlan, capture: ProbeCaptureSession): PathVerificationResult {
        val usedNonces = mutableSetOf<List<Byte>>()
        if (helperSender != null) {
            return verifyWithHelperUid(plan, capture, usedNonces)
        }
        return verifyWithVpnOwnerUid(plan, capture, usedNonces)
    }

    private fun verifyWithHelperUid(
        plan: RoutePlan,
        capture: ProbeCaptureSession,
        usedNonces: MutableSet<List<Byte>>,
    ): PathVerificationResult {
        var capturedChecksPassed = true
        val pathErrors = mutableListOf<String>()
        plan.expectedCaptured.forEach { address ->
            val helper = helperCapturedProof(address, usedNonces, capture)
            if (helper.captured != true) {
                capturedChecksPassed = false
                pathErrors += "captured destination=${address.hostAddress}" +
                    helper.text
            }
        }

        var bypassChecksPassed = true
        var bypassPathCaptured = false
        var directResponseStatus = DirectPathStatus.PASS
        plan.expectedBypass.forEach { address ->
            val nonce = nextUniqueNonce(usedNonces)
            val helper = helperDiagnostic(address, nonce, capture)
            when (helper.captured) {
                true -> {
                    bypassChecksPassed = false
                    bypassPathCaptured = true
                    pathErrors += "bypass destination=${address.hostAddress}" +
                        helper.text + " " +
                        capture.diagnosticSummary(address, nonce)
                }

                false -> Unit
                null -> {
                    bypassChecksPassed = false
                    directResponseStatus = DirectPathStatus.INCONCLUSIVE
                    pathErrors += "bypass destination=${address.hostAddress}" +
                        helper.text
                }
            }
        }

        return PathVerificationResult(
            capturedChecksPassed = capturedChecksPassed,
            bypassChecksPassed = bypassChecksPassed,
            bypassPathCaptured = bypassPathCaptured,
            directResponseStatus = directResponseStatus,
            captureError = pathErrors.takeIf(List<String>::isNotEmpty)?.let { errors ->
                ProbeCaptureMissException(
                    "Helper UID route proof failed after VPN route readiness: " +
                        errors.joinToString("; "),
                )
            },
        )
    }

    private fun verifyWithVpnOwnerUid(
        plan: RoutePlan,
        capture: ProbeCaptureSession,
        usedNonces: MutableSet<List<Byte>>,
    ): PathVerificationResult {
        var capturedChecksPassed = true
        var helperCapturedOwnerUidMiss = false
        val captureMisses = mutableListOf<String>()
        plan.expectedCaptured.forEach { address ->
            val nonce = nextUniqueNonce(usedNonces)
            val sent = capturedSender.send(address, nonce)
            if (!capture.await(address, nonce, PATH_TIMEOUT_MS)) {
                capturedChecksPassed = false
                val helper = if (helperSender == null) {
                    HelperProbeDiagnostic.EMPTY
                } else {
                    helperDiagnostic(address, nextUniqueNonce(usedNonces), capture)
                }
                if (helper.captured == true) {
                    helperCapturedOwnerUidMiss = true
                }
                captureMisses += "destination=${address.hostAddress} " +
                    sent.diagnosticFor(address) + " " +
                    capture.diagnosticSummary(address, nonce) +
                    helper.text
            }
        }

        var bypassChecksPassed = true
        var bypassPathCaptured = false
        var directResponseStatus = DirectPathStatus.PASS
        plan.expectedBypass.forEach { address ->
            val nonce = nextUniqueNonce(usedNonces)
            val direct = directDnsVerifier.verify(address, nonce)
            val captured = capture.await(address, direct.wireNonce, PATH_TIMEOUT_MS)
            if (captured) {
                bypassPathCaptured = true
            }
            if (direct.status != DirectResponseStatus.PASS) {
                directResponseStatus = DirectPathStatus.INCONCLUSIVE
            }
            if (direct.status != DirectResponseStatus.PASS || captured) {
                bypassChecksPassed = false
            }
        }
        return PathVerificationResult(
            capturedChecksPassed = capturedChecksPassed,
            bypassChecksPassed = bypassChecksPassed,
            bypassPathCaptured = bypassPathCaptured,
            directResponseStatus = directResponseStatus,
            helperCapturedOwnerUidMiss = helperCapturedOwnerUidMiss,
            captureError = captureMisses.takeIf(List<String>::isNotEmpty)?.let { misses ->
                ProbeCaptureMissException(
                    "Expected packet was not captured after VPN route readiness: " +
                        misses.joinToString("; "),
                )
            },
        )
    }

    private fun helperCapturedProof(
        address: InetAddress,
        usedNonces: MutableSet<List<Byte>>,
        capture: ProbeCaptureSession,
    ): HelperProbeDiagnostic {
        val failedAttempts = mutableListOf<String>()
        var last = HelperProbeDiagnostic.EMPTY
        repeat(HELPER_CAPTURE_ATTEMPTS) { index ->
            val nonce = nextUniqueNonce(usedNonces)
            val helper = helperDiagnostic(address, nonce, capture)
            if (helper.captured == true) {
                return helper.copy(text = helper.text + " helperAttempt=${index + 1}")
            }
            val failure = "helperAttempt=${index + 1}" +
                helper.text + " " +
                capture.diagnosticSummary(address, nonce)
            failedAttempts += failure
            last = helper
            if (helper.captured == null) {
                return HelperProbeDiagnostic(
                    text = " " + failedAttempts.joinToString(" "),
                    captured = null,
                )
            }
        }
        return HelperProbeDiagnostic(
            text = " " + failedAttempts.joinToString(" "),
            captured = last.captured,
        )
    }

    private fun helperDiagnostic(
        address: InetAddress,
        nonce: ByteArray,
        capture: ProbeCaptureSession,
    ): HelperProbeDiagnostic {
        val sender = helperSender ?: return HelperProbeDiagnostic.EMPTY
        val sendResult = try {
            sender.send(address, nonce)
        } catch (error: Exception) {
            HelperProbeSendResult.failed(error)
        }
        val helperCaptured = if (sendResult.status == HelperProbeSendStatus.SUCCESS) {
            capture.await(address, nonce, PATH_TIMEOUT_MS)
        } else {
            null
        }
        return HelperProbeDiagnostic(
            text = " " + sendResult.diagnostic(helperCaptured),
            captured = helperCaptured,
        )
    }

    private fun nextUniqueNonce(used: MutableSet<List<Byte>>): ByteArray {
        val nonce = nonceSource()
        require(nonce.size == NONCE_BYTES) { "Probe nonce must be exactly $NONCE_BYTES bytes" }
        check(used.add(nonce.toList())) { "Probe nonce collision" }
        return nonce
    }

    private companion object {
        const val NONCE_BYTES = 16
        const val PATH_TIMEOUT_MS = 1_500L
        const val HELPER_CAPTURE_ATTEMPTS = 3
    }
}

private data class HelperProbeDiagnostic(
    val text: String,
    val captured: Boolean?,
) {
    companion object {
        val EMPTY = HelperProbeDiagnostic(text = "", captured = null)
    }
}

private fun String.asSingleToken(): String =
    replace(Regex("\\s+"), "_")

internal data class RecordedEstablish<T>(
    val status: EstablishStatus,
    val value: T?,
    val establishMs: Long,
    val metricsBefore: ProcessMetrics,
    val metricsAfter: ProcessMetrics,
    val error: Exception?,
) {
    val errorClass: String?
        get() = error?.javaClass?.name

    val errorMessage: String?
        get() = error?.message
}

internal class ProbeEstablishRecorder(
    private val elapsedRealtime: () -> Long,
    private val processMetrics: () -> ProcessMetrics,
    private val checkpointWriter: ProbeCheckpointWriter,
) {
    fun <T> execute(
        metadata: ProbeEstablishMetadata,
        establish: () -> T?,
    ): RecordedEstablish<T> {
        val metricsBefore = processMetrics()
        val beforeCheckpointAt = elapsedRealtime()
        checkpointWriter.append(
            metadata.checkpoint(CheckpointPhase.BEFORE_ESTABLISH, beforeCheckpointAt),
        )
        val startedAt = elapsedRealtime()

        var value: T? = null
        var failure: Exception? = null
        try {
            value = establish()
        } catch (error: Exception) {
            failure = error
        }
        val finishedAt = elapsedRealtime()
        val postEstablishFailures = mutableListOf<Exception>()
        try {
            checkpointWriter.append(metadata.checkpoint(CheckpointPhase.AFTER_ESTABLISH, finishedAt))
        } catch (error: Exception) {
            postEstablishFailures += error
        }
        var metricsAfter: ProcessMetrics? = null
        try {
            metricsAfter = processMetrics()
        } catch (error: Exception) {
            postEstablishFailures += error
        }
        if (postEstablishFailures.isNotEmpty()) {
            val primary = failure ?: postEstablishFailures.removeAt(0)
            postEstablishFailures.forEach(primary::addSuppressed)
            throw primary
        }
        val status = when {
            failure != null -> EstablishStatus.EXCEPTION
            value == null -> EstablishStatus.NULL
            else -> EstablishStatus.SUCCESS
        }
        return RecordedEstablish(
            status = status,
            value = value,
            establishMs = finishedAt - startedAt,
            metricsBefore = metricsBefore,
            metricsAfter = checkNotNull(metricsAfter),
            error = failure,
        )
    }

    private fun ProbeEstablishMetadata.checkpoint(
        phase: CheckpointPhase,
        now: Long,
    ): ProbeCheckpoint = ProbeCheckpoint(
        runId = runId,
        attempt = request.attempt,
        scenario = plan.scenario.name,
        strategy = plan.strategy.name,
        includeCount = plan.includes.size,
        excludeCount = plan.excludes.size,
        builderEntryCount = plan.builderEntryCount,
        planSha256 = planSha256,
        phase = phase,
        elapsedRealtime = now,
    )
}

internal data class ProbeAttemptRequest(
    val attempt: Int,
    val scenario: ProbeScenario,
    val isHandover: Boolean,
)

internal data class ProbeAttemptOutcome(
    val establishStatus: EstablishStatus,
    val establishMs: Long,
    val capturedChecksPassed: Boolean,
    val bypassChecksPassed: Boolean,
    val directResponseStatus: DirectPathStatus,
    val bypassPathCaptured: Boolean = false,
    val helperCapturedOwnerUidMiss: Boolean = false,
    val scenarioResult: ProbeScenarioResult? = null,
    val error: Exception? = null,
)

internal fun interface ProbeAttemptOutcomeProvider {
    fun execute(request: ProbeAttemptRequest): ProbeAttemptOutcome
}

internal data class ProbeExecutionResult(
    val decision: GateDecision,
    val outcomes: List<ProbeAttemptOutcome>,
    val handoverP50Ms: Long?,
    val handoverP95Ms: Long?,
)

internal class ProbeExecutionController(
    private val outcomeProvider: ProbeAttemptOutcomeProvider,
    private val progress: (ProbeAttemptRequest) -> Unit = {},
) {
    fun execute(physicalDualStackAvailable: Boolean): ProbeExecutionResult {
        if (!physicalDualStackAvailable) {
            return ProbeExecutionResult(
                decision = GateDecision.INCONCLUSIVE,
                outcomes = emptyList(),
                handoverP50Ms = null,
                handoverP95Ms = null,
            )
        }

        val requests = buildList {
            ProbeScenario.entries.forEach { scenario ->
                add(
                    ProbeAttemptRequest(
                        attempt = size + 1,
                        scenario = scenario,
                        isHandover = false,
                    ),
                )
            }
            repeat(HANDOVER_ATTEMPTS) {
                add(
                    ProbeAttemptRequest(
                        attempt = size + 1,
                        scenario = ProbeScenario.FOREIGN_DIRECT_DUAL,
                        isHandover = true,
                    ),
                )
            }
        }
        val outcomes = mutableListOf<ProbeAttemptOutcome>()
        for (request in requests) {
            progress(request)
            val outcome = outcomeProvider.execute(request)
            outcomes += outcome
            val decision = outcome.decision()
            if (decision != GateDecision.PASS) {
                return ProbeExecutionResult(
                    decision = decision,
                    outcomes = outcomes,
                    handoverP50Ms = null,
                    handoverP95Ms = null,
                )
            }
        }
        val handoverTimes = outcomes.drop(ProbeScenario.entries.size).map { it.establishMs }
        return ProbeExecutionResult(
            decision = GateDecision.PASS,
            outcomes = outcomes,
            handoverP50Ms = percentile(handoverTimes, 50),
            handoverP95Ms = percentile(handoverTimes, 95),
        )
    }

    private fun ProbeAttemptOutcome.decision(): GateDecision = when {
        establishStatus != EstablishStatus.SUCCESS -> GateDecision.BLOCKED
        bypassPathCaptured -> GateDecision.BLOCKED
        !capturedChecksPassed && !helperCapturedOwnerUidMiss -> GateDecision.BLOCKED
        directResponseStatus == DirectPathStatus.INCONCLUSIVE -> GateDecision.INCONCLUSIVE
        !bypassChecksPassed -> GateDecision.BLOCKED
        else -> GateDecision.PASS
    }

    private fun percentile(values: List<Long>, percentile: Int): Long? {
        if (values.size != HANDOVER_ATTEMPTS) return null
        val sorted = values.sorted()
        val rank = (percentile * sorted.size + 99) / 100
        return sorted[rank - 1]
    }

    private companion object {
        const val HANDOVER_ATTEMPTS = 20
    }
}

internal class HandoverOwner<T : Closeable> : Closeable {
    private val lock = Any()
    private var current: T? = null
    private var closed = false

    fun seed(value: T) {
        check(trySeed(value)) { "Handover owner is closed" }
    }

    fun trySeed(value: T): Boolean {
        val accepted = synchronized(lock) {
            if (closed) {
                false
            } else {
                check(current == null) { "Handover owner already has a TUN" }
                current = value
                true
            }
        }
        if (!accepted) value.close()
        return accepted
    }

    fun adoptCandidate(candidate: T): Boolean {
        var old: T? = null
        val accepted = synchronized(lock) {
            if (closed) {
                false
            } else {
                old = checkNotNull(current) { "Handover owner has no seed TUN" }
                current = candidate
                true
            }
        }
        if (!accepted) {
            candidate.close()
            return false
        }
        checkNotNull(old).close()
        return true
    }

    fun release(value: T) {
        val shouldClose = synchronized(lock) {
            if (current === value) {
                current = null
                true
            } else {
                false
            }
        }
        if (shouldClose) value.close()
    }

    fun failCandidate(candidate: T?) {
        val old = synchronized(lock) {
            if (closed) {
                null
            } else {
                closed = true
                val owned = current
                current = null
                owned
            }
        }
        closeBoth(candidate, old)
    }

    override fun close() {
        val owned = synchronized(lock) {
            if (closed) {
                null
            } else {
                closed = true
                val value = current
                current = null
                value
            }
        }
        owned?.close()
    }

    private fun closeBoth(first: T?, second: T?) {
        var failure: Throwable? = null
        try {
            first?.close()
        } catch (error: Throwable) {
            failure = error
        }
        if (second !== first) {
            try {
                second?.close()
            } catch (error: Throwable) {
                if (failure == null) failure = error else failure.addSuppressed(error)
            }
        }
        failure?.let { throw it }
    }
}

internal enum class ProbeRunStartResult { STARTED, ALREADY_RUNNING, TERMINAL }

internal class ProbeRunLifecycle {
    private val state = AtomicReference(State.IDLE)

    fun tryStart(): ProbeRunStartResult {
        while (true) {
            when (val current = state.get()) {
                State.IDLE -> if (state.compareAndSet(current, State.RUNNING)) {
                    return ProbeRunStartResult.STARTED
                }

                State.RUNNING -> return ProbeRunStartResult.ALREADY_RUNNING
                State.FINISHED,
                State.CANCELLED,
                -> return ProbeRunStartResult.TERMINAL
            }
        }
    }

    fun isRunning(): Boolean = state.get() == State.RUNNING

    fun finish() {
        state.compareAndSet(State.RUNNING, State.FINISHED)
    }

    fun cancelOnce(): Boolean = state.getAndSet(State.CANCELLED) != State.CANCELLED

    private enum class State { IDLE, RUNNING, FINISHED, CANCELLED }
}

fun interface ProbeTunFactory {
    fun establish(plan: RoutePlan): ParcelFileDescriptor?
}

data class ProbeRunResult(
    val report: ProbeReport,
    val reportPath: File,
    val handoverP50Ms: Long?,
    val handoverP95Ms: Long?,
) {
    fun completionMessage(): String = buildString {
        append("Gate 0 ").append(report.gateDecision.name)
        if (handoverP50Ms != null && handoverP95Ms != null) {
            append("; FOREIGN_DIRECT_DUAL handover establish p50=")
                .append(handoverP50Ms)
                .append("ms p95=")
                .append(handoverP95Ms)
                .append("ms")
        }
    }
}

class ProbeRunner private constructor(
    private val service: VpnService,
    private val reportStore: ProbeReportStore,
    private val tunFactory: ProbeTunFactory,
    private val captureFactory: (ParcelFileDescriptor) -> ProbeCaptureSession,
    private val readiness: ProbeAttemptReadiness,
    private val pathVerifier: ProbePathVerifier,
    private val physicalDualStackAvailable: () -> Boolean,
    private val progress: (String) -> Unit,
) : Closeable {
    private val closed = AtomicBoolean(false)
    private val captureOwner = HandoverOwner<ProbeCaptureSession>()

    fun run(): ProbeRunResult {
        check(!closed.get()) { "ProbeRunner is closed" }
        val unmatched = try {
            reportStore.unmatchedBeforeEstablish()
        } catch (error: Exception) {
            return persistFailure("CHECKPOINT_RECOVERY", error, emptyIpDbMetadata())
        }

        val ipDb = try {
            ProbeIpDb.load(service)
        } catch (error: Exception) {
            return persistFailure("IPDB_LOAD", error, emptyIpDbMetadata())
        }
        val ipDbMetadata = try {
            ipDb.metadata()
        } catch (error: Exception) {
            return persistFailure("IPDB_METADATA", error, emptyIpDbMetadata())
        }
        if (unmatched.isNotEmpty()) {
            progress("检测到未配对的 BEFORE_ESTABLISH，Gate 0 已阻断")
            return persist(
                ProbeReport(
                    generatedAtEpochMs = System.currentTimeMillis(),
                    device = deviceMetadata(),
                    ipdb = ipDbMetadata,
                    scenarios = emptyList(),
                    gateDecision = GateDecision.BLOCKED,
                ),
                handoverP50Ms = null,
                handoverP95Ms = null,
            )
        }

        val hasPhysicalDualStack = try {
            physicalDualStackAvailable()
        } catch (error: Exception) {
            return persistFailure("PHYSICAL_DUAL_STACK_CHECK", error, ipDbMetadata)
        }
        if (!hasPhysicalDualStack) {
            progress("物理网络不具备 IPv4/IPv6 双栈，结果为 INCONCLUSIVE")
        }

        val runId = UUID.randomUUID().toString()
        val recorder = ProbeEstablishRecorder(
            elapsedRealtime = SystemClock::elapsedRealtime,
            processMetrics = ::readProcessMetrics,
            checkpointWriter = ProbeCheckpointWriter(reportStore::appendCheckpoint),
        )
        val provider = AndroidAttemptOutcomeProvider<ParcelFileDescriptor>(
            planFactory = { scenario ->
                RoutePlanFactory.create(scenario, Build.VERSION.SDK_INT, ipDb.cidrs)
            },
            runId = runId,
            establish = tunFactory::establish,
            recorder = recorder,
            captureFactory = captureFactory,
            captureOwner = captureOwner,
            readiness = readiness,
            pathVerifier = pathVerifier,
        )
        val execution = try {
            ProbeExecutionController(
                outcomeProvider = provider,
                progress = { request ->
                    progress(
                        "attempt ${request.attempt}/26: ${request.scenario.name}" +
                            if (request.isHandover) " candidate handover" else "",
                    )
                },
            ).execute(hasPhysicalDualStack)
        } catch (error: Exception) {
            try {
                captureOwner.close()
            } catch (closeError: Exception) {
                error.addSuppressed(closeError)
            }
            progress("执行异常，Gate 0 已阻断: ${error.javaClass.name}")
            return persistFailure("RUN_EXECUTION", error, ipDbMetadata)
        }
        if (execution.handoverP50Ms != null && execution.handoverP95Ms != null) {
            progress(
                "FOREIGN_DIRECT_DUAL establish p50=${execution.handoverP50Ms}ms " +
                    "p95=${execution.handoverP95Ms}ms",
            )
        }
        val closeError = try {
            captureOwner.close()
            null
        } catch (error: Exception) {
            error
        }
        val scenarioResults = execution.outcomes
            .mapNotNull(ProbeAttemptOutcome::scenarioResult)
            .toMutableList()
        if (closeError != null && scenarioResults.isNotEmpty()) {
            val last = scenarioResults.last()
            scenarioResults[scenarioResults.lastIndex] = last.copy(
                errorClass = closeError.javaClass.name,
                errorMessage = closeError.message,
            )
            progress("最终 TUN 释放失败，Gate 0 已阻断: ${closeError.javaClass.name}")
        }
        val report = ProbeReport(
            generatedAtEpochMs = System.currentTimeMillis(),
            device = deviceMetadata(),
            ipdb = ipDbMetadata,
            scenarios = scenarioResults,
            gateDecision = if (closeError == null) execution.decision else GateDecision.BLOCKED,
        )
        return persist(report, execution.handoverP50Ms, execution.handoverP95Ms)
    }

    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        captureOwner.close()
    }

    private fun persistFailure(
        label: String,
        error: Exception,
        ipDbMetadata: ProbeIpDbMetadata,
    ): ProbeRunResult {
        val scenario = ProbeScenarioResult(
            scenario = label,
            attempt = 1,
            strategy = "NONE",
            includeCount = UNAVAILABLE_INT,
            excludeCount = UNAVAILABLE_INT,
            builderEntryCount = UNAVAILABLE_INT,
            planSha256 = "UNAVAILABLE",
            establishMs = UNAVAILABLE_LONG,
            rssBeforeKb = UNAVAILABLE_INT,
            rssAfterKb = UNAVAILABLE_INT,
            rssHighWaterKb = UNAVAILABLE_INT,
            fdCountBefore = UNAVAILABLE_INT,
            fdCountAfter = UNAVAILABLE_INT,
            expectedCaptured = emptyList(),
            capturedChecksPassed = false,
            expectedBypass = emptyList(),
            bypassChecksPassed = false,
            directResponseStatus = DirectPathStatus.INCONCLUSIVE.name,
            establishStatus = EstablishStatus.EXCEPTION.name,
            errorClass = error.javaClass.name,
            errorMessage = error.message.orEmpty() + UNAVAILABLE_METRICS_DETAIL,
        )
        return persist(
            ProbeReport(
                generatedAtEpochMs = System.currentTimeMillis(),
                device = deviceMetadata(),
                ipdb = ipDbMetadata,
                scenarios = listOf(scenario),
                gateDecision = GateDecision.BLOCKED,
            ),
            handoverP50Ms = null,
            handoverP95Ms = null,
        )
    }

    private fun persist(
        report: ProbeReport,
        handoverP50Ms: Long?,
        handoverP95Ms: Long?,
    ): ProbeRunResult = ProbeRunResult(
        report = report,
        reportPath = reportStore.write(report),
        handoverP50Ms = handoverP50Ms,
        handoverP95Ms = handoverP95Ms,
    )

    companion object {
        fun create(service: VpnService, progress: (String) -> Unit): ProbeRunner {
            val secureRandom = SecureRandom()
            val vpnRouteSnapshots = AndroidVpnRouteSnapshotProvider(service)
            return ProbeRunner(
                service = service,
                reportStore = ProbeReportStore(service),
                tunFactory = AndroidProbeTunFactory(service),
                captureFactory = { pfd -> AndroidTunCaptureSession(TunCapture(pfd)) },
                readiness = object : ProbeAttemptReadiness {
                    override fun beforeEstablish(plan: RoutePlan): ProbeReadinessBaseline =
                        vpnRouteSnapshots.snapshot(plan).readinessBaseline(plan.expectedCaptured)

                    override fun await(
                        plan: RoutePlan,
                        capture: ProbeCaptureSession,
                        baseline: ProbeReadinessBaseline,
                    ) {
                        if (!capture.awaitReaderStarted(VPN_READINESS_TIMEOUT_MS)) {
                            throw VpnRouteReadinessException(
                                "TUN reader did not start within ${VPN_READINESS_TIMEOUT_MS}ms",
                            )
                        }
                        VpnRouteReadinessWaiter(
                            snapshot = { vpnRouteSnapshots.snapshot(plan) },
                        ).await(
                            expectedTargets = plan.expectedCaptured,
                            previousActiveOwnedVpnIdentity = baseline.activeOwnedVpnIdentity
                                .takeIf { baseline.requiresFreshVpnIdentity },
                        )
                    }
                },
                pathVerifier = ProbePathVerifier(
                    nonceSource = {
                        ByteArray(PROBE_NONCE_BYTES).also(secureRandom::nextBytes)
                    },
                    capturedSender = CapturedDatagramSender(::sendUnprotectedProbe),
                    directDnsVerifier = DirectDnsVerifier(UdpPathProbe::verifyDirectDns),
                    helperSender = HelperProbeContentProviderSender(service),
                ),
                physicalDualStackAvailable = { service.hasPhysicalDualStack() },
                progress = progress,
            )
        }

        private const val PROBE_NONCE_BYTES = 16
        private const val VPN_READINESS_TIMEOUT_MS = 5_000L
        private const val UNAVAILABLE_INT = -1
        private const val UNAVAILABLE_LONG = -1L
        private const val UNAVAILABLE_METRICS_DETAIL =
            " [route counts and process metrics unavailable; sentinel=-1]"
    }
}

internal class AndroidAttemptOutcomeProvider<T : Closeable>(
    private val planFactory: (ProbeScenario) -> RoutePlan,
    private val runId: String,
    private val establish: (RoutePlan) -> T?,
    private val recorder: ProbeEstablishRecorder,
    private val captureFactory: (T) -> ProbeCaptureSession,
    private val captureOwner: HandoverOwner<ProbeCaptureSession>,
    private val readiness: ProbeAttemptReadiness,
    private val pathVerifier: ProbePathVerifier,
) : ProbeAttemptOutcomeProvider {
    override fun execute(request: ProbeAttemptRequest): ProbeAttemptOutcome {
        val plan = try {
            planFactory(request.scenario)
        } catch (error: Exception) {
            val failure = preserveFailure(
                primary = error,
                cleanupActions = if (request.isHandover) {
                    listOf({ captureOwner.failCandidate(null) })
                } else {
                    emptyList()
                },
            )
            return preEstablishFailure(request, checkNotNull(failure))
        }
        val planSha = normalizedPlanSha256(plan)
        val readinessBaseline = try {
            readiness.beforeEstablish(plan)
        } catch (error: Exception) {
            val failure = preserveFailure(
                primary = error,
                cleanupActions = if (request.isHandover) {
                    listOf({ captureOwner.failCandidate(null) })
                } else {
                    emptyList()
                },
            )
            return failedOutcome(request, plan, planSha, checkNotNull(failure))
        }
        var returnedDescriptor: T? = null
        val recorded = try {
            recorder.execute(
                ProbeEstablishMetadata(runId, request, plan, planSha),
            ) {
                establish(plan).also { returnedDescriptor = it }
            }
        } catch (error: Exception) {
            val failure = preserveFailure(
                primary = error,
                cleanupActions = buildList {
                    add { returnedDescriptor?.close() }
                    if (request.isHandover) add { captureOwner.failCandidate(null) }
                },
            )
            return failedOutcome(request, plan, planSha, checkNotNull(failure))
        }
        if (recorded.status != EstablishStatus.SUCCESS) {
            val failure = preserveFailure(
                primary = recorded.error,
                cleanupActions = if (request.isHandover) {
                    listOf({ captureOwner.failCandidate(null) })
                } else {
                    emptyList()
                },
            )
            return outcome(request, plan, planSha, recorded, null, failure)
        }

        val pfd = checkNotNull(recorded.value)
        val capture = try {
            captureFactory(pfd)
        } catch (error: Exception) {
            val failure = preserveFailure(
                primary = error,
                cleanupActions = buildList {
                    add { pfd.close() }
                    if (request.isHandover) add { captureOwner.failCandidate(null) }
                },
            )
            return outcome(request, plan, planSha, recorded, null, failure)
        }
        val accepted = try {
            if (request.isHandover) {
                captureOwner.adoptCandidate(capture)
            } else {
                captureOwner.trySeed(capture)
            }
        } catch (error: Exception) {
            val failure = preserveFailure(error, listOf({ captureOwner.release(capture) }))
            return outcome(request, plan, planSha, recorded, null, failure)
        }
        if (!accepted) {
            return outcome(
                request,
                plan,
                planSha,
                recorded,
                null,
                CancellationException("ProbeRunner was cancelled"),
            )
        }

        val verification = try {
            readiness.await(plan, capture, readinessBaseline)
            pathVerifier.verify(plan, capture)
        } catch (error: Exception) {
            val failure = preserveFailure(error, listOf({ captureOwner.release(capture) }))
            return outcome(request, plan, planSha, recorded, null, failure)
        }
        val retainForHandover = request.isHandover ||
            request.scenario == ProbeScenario.FOREIGN_DIRECT_DUAL
        if (!retainForHandover || !verification.isPass()) {
            val releaseFailure = preserveFailure(null, listOf({ captureOwner.release(capture) }))
            if (releaseFailure != null) {
                return outcome(request, plan, planSha, recorded, verification, releaseFailure)
            }
        }
        return outcome(request, plan, planSha, recorded, verification, null)
    }

    private fun preEstablishFailure(
        request: ProbeAttemptRequest,
        error: Exception,
    ): ProbeAttemptOutcome {
        val metrics = ProcessMetrics(-1, -1, -1)
        val result = ProbeScenarioResult(
            scenario = request.scenario.name,
            attempt = request.attempt,
            strategy = "UNAVAILABLE",
            includeCount = -1,
            excludeCount = -1,
            builderEntryCount = -1,
            planSha256 = "UNAVAILABLE",
            establishMs = -1,
            rssBeforeKb = metrics.rssKb,
            rssAfterKb = metrics.rssKb,
            rssHighWaterKb = metrics.highWaterKb,
            fdCountBefore = metrics.openFdCount,
            fdCountAfter = metrics.openFdCount,
            expectedCaptured = emptyList(),
            capturedChecksPassed = false,
            expectedBypass = emptyList(),
            bypassChecksPassed = false,
            directResponseStatus = DirectPathStatus.INCONCLUSIVE.name,
            establishStatus = EstablishStatus.EXCEPTION.name,
            errorClass = error.javaClass.name,
            errorMessage = error.message.orEmpty() +
                " [route plan and process metrics unavailable; sentinel=-1]",
        )
        return ProbeAttemptOutcome(
            establishStatus = EstablishStatus.EXCEPTION,
            establishMs = -1,
            capturedChecksPassed = false,
            bypassChecksPassed = false,
            directResponseStatus = DirectPathStatus.INCONCLUSIVE,
            scenarioResult = result,
            error = error,
        )
    }

    private fun failedOutcome(
        request: ProbeAttemptRequest,
        plan: RoutePlan,
        planSha: String,
        error: Exception,
    ): ProbeAttemptOutcome {
        val metrics = ProcessMetrics(-1, -1, -1)
        val synthetic = RecordedEstablish<T>(
            status = EstablishStatus.EXCEPTION,
            value = null,
            establishMs = -1,
            metricsBefore = metrics,
            metricsAfter = metrics,
            error = error,
        )
        return outcome(request, plan, planSha, synthetic, null, error)
    }

    private fun outcome(
        request: ProbeAttemptRequest,
        plan: RoutePlan,
        planSha: String,
        recorded: RecordedEstablish<T>,
        verification: PathVerificationResult?,
        pathError: Exception?,
    ): ProbeAttemptOutcome {
        val captureError = verification?.captureError
        if (captureError != null && pathError != null && captureError !== pathError) {
            captureError.addSuppressed(pathError)
        }
        val error = recorded.error ?: captureError ?: pathError
        val capturedPassed = verification?.capturedChecksPassed ?: false
        val bypassPassed = verification?.bypassChecksPassed ?: false
        val directStatus = verification?.directResponseStatus ?: DirectPathStatus.INCONCLUSIVE
        val result = ProbeScenarioResult(
            scenario = plan.scenario.name,
            attempt = request.attempt,
            strategy = plan.strategy.name,
            includeCount = plan.includes.size,
            excludeCount = plan.excludes.size,
            builderEntryCount = plan.builderEntryCount,
            planSha256 = planSha,
            establishMs = recorded.establishMs,
            rssBeforeKb = recorded.metricsBefore.rssKb,
            rssAfterKb = recorded.metricsAfter.rssKb,
            rssHighWaterKb = recorded.metricsAfter.highWaterKb,
            fdCountBefore = recorded.metricsBefore.openFdCount,
            fdCountAfter = recorded.metricsAfter.openFdCount,
            expectedCaptured = plan.expectedCaptured.map { requireNotNull(it.hostAddress) },
            capturedChecksPassed = capturedPassed,
            expectedBypass = plan.expectedBypass.map { requireNotNull(it.hostAddress) },
            bypassChecksPassed = bypassPassed,
            directResponseStatus = directStatus.name,
            establishStatus = recorded.status.name,
            errorClass = error?.javaClass?.name,
            errorMessage = error?.message?.let { message ->
                if (recorded.establishMs < 0) {
                    "$message [establish time and process metrics unavailable; sentinel=-1]"
                } else {
                    message
                }
            },
        )
        return ProbeAttemptOutcome(
            establishStatus = if (pathError == null) recorded.status else EstablishStatus.EXCEPTION,
            establishMs = recorded.establishMs,
            capturedChecksPassed = capturedPassed,
            bypassChecksPassed = bypassPassed,
            directResponseStatus = directStatus,
            bypassPathCaptured = verification?.bypassPathCaptured ?: false,
            helperCapturedOwnerUidMiss = verification?.helperCapturedOwnerUidMiss ?: false,
            scenarioResult = result,
            error = error,
        )
    }

    private fun PathVerificationResult.isPass(): Boolean =
        capturedChecksPassed && bypassChecksPassed &&
            !bypassPathCaptured && directResponseStatus == DirectPathStatus.PASS
}

private fun preserveFailure(
    primary: Exception?,
    cleanupActions: List<() -> Unit>,
): Exception? {
    var failure = primary
    cleanupActions.forEach { cleanup ->
        try {
            cleanup()
        } catch (cleanupError: Exception) {
            if (failure == null) {
                failure = cleanupError
            } else if (failure !== cleanupError) {
                failure.addSuppressed(cleanupError)
            }
        }
    }
    return failure
}

private class AndroidProbeTunFactory(private val service: VpnService) : ProbeTunFactory {
    override fun establish(plan: RoutePlan): ParcelFileDescriptor? {
        val builder = AndroidProbeBuilder(service.Builder())
        applyRoutePlan(builder, plan, Build.VERSION.SDK_INT)
        return builder.establish()
    }
}

private class AndroidProbeBuilder(
    private val builder: VpnService.Builder,
) : ProbeBuilderFacade {
    override fun setSession(value: String) {
        builder.setSession(value)
    }

    override fun setMtu(value: Int) {
        builder.setMtu(value)
    }

    override fun setBlocking(value: Boolean) {
        builder.setBlocking(value)
    }

    override fun addAddress(address: String, prefixLength: Int) {
        builder.addAddress(address, prefixLength)
    }

    override fun allowFamily(family: IpFamily) {
        builder.allowFamily(
            when (family) {
                IpFamily.IPV4 -> OsConstants.AF_INET
                IpFamily.IPV6 -> OsConstants.AF_INET6
            },
        )
    }

    override fun addRoute(cidr: Cidr) {
        builder.addRoute(cidr.address(), cidr.prefixLength)
    }

    @TargetApi(33)
    override fun excludeRoute(cidr: Cidr) {
        builder.excludeRoute(IpPrefix(cidr.address(), cidr.prefixLength))
    }

    override fun establish(): ParcelFileDescriptor? = builder.establish()
}

private class AndroidTunCaptureSession(
    private val capture: TunCapture,
) : ProbeCaptureSession {
    override fun awaitReaderStarted(timeoutMs: Long): Boolean =
        capture.awaitReaderStarted(timeoutMs)

    override fun await(destination: InetAddress, nonce: ByteArray, timeoutMs: Long): Boolean =
        capture.await(destination, nonce, timeoutMs)

    override fun captured(destination: InetAddress, nonce: ByteArray): Boolean =
        capture.captured(destination, nonce)

    override fun diagnosticSummary(destination: InetAddress, nonce: ByteArray): String {
        val diagnostics = capture.diagnostics(destination, nonce)
        return buildString {
            append("readerStarted=").append(diagnostics.readerStarted)
            append(" readerFinished=").append(diagnostics.readerFinished)
            append(" unexpectedEof=").append(diagnostics.unexpectedEof)
            append(" readCount=").append(diagnostics.readCount)
            append(" parsedCount=").append(diagnostics.parsedCount)
            append(" destinationSeen=").append(diagnostics.destinationSeen)
            append(" nonceSeen=").append(diagnostics.nonceSeen)
            append(" exactPacketSeen=").append(diagnostics.exactPacketSeen)
            append(" readerFailureClass=").append(diagnostics.readerFailureClass)
            append(" readerFailureMessage=").append(diagnostics.readerFailureMessage)
        }
    }

    override fun close() {
        capture.close()
    }
}

private fun sendUnprotectedProbe(
    address: InetAddress,
    nonce: ByteArray,
): CapturedProbeSendObservation {
    DatagramSocket().use { socket ->
        socket.connect(InetSocketAddress(address, 9))
        socket.send(DatagramPacket(nonce, nonce.size))
        return CapturedProbeSendObservation(socket.localAddress)
    }
}

private fun readProcessMetrics(): ProcessMetrics {
    val values = File("/proc/self/status").readLines()
        .mapNotNull { line ->
            val parts = line.trim().split(Regex("\\s+"))
            if (parts.size >= 2) parts[0].removeSuffix(":") to parts[1] else null
        }
        .toMap()
    return ProcessMetrics(
        rssKb = requireNotNull(values["VmRSS"]).toInt(),
        highWaterKb = requireNotNull(values["VmHWM"]).toInt(),
        openFdCount = requireNotNull(File("/proc/self/fd").list()).size,
    )
}

private fun Context.hasPhysicalDualStack(): Boolean {
    val connectivity = getSystemService(ConnectivityManager::class.java)
    val network = connectivity.activeNetwork ?: return false
    val capabilities = connectivity.getNetworkCapabilities(network) ?: return false
    if (!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN) ||
        !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    ) {
        return false
    }
    val addresses = connectivity.getLinkProperties(network)
        ?.linkAddresses
        ?.map { it.address }
        .orEmpty()
    val hasV4 = addresses.any { it is Inet4Address && !it.isLoopbackAddress }
    val hasV6 = addresses.any {
        it is Inet6Address && !it.isLoopbackAddress && !it.isLinkLocalAddress
    }
    return hasV4 && hasV6
}

private fun deviceMetadata(): ProbeDeviceMetadata = ProbeDeviceMetadata(
    model = Build.MODEL,
    sdkInt = Build.VERSION.SDK_INT,
    supportedAbis = Build.SUPPORTED_ABIS.toList(),
    pageSizeBytes = Os.sysconf(OsConstants._SC_PAGESIZE),
    fingerprint = Build.FINGERPRINT,
)

private fun ProbeIpDb.metadata(): ProbeIpDbMetadata {
    val ipv4 = cidrs.filter { it.family == IpFamily.IPV4 }
    val ipv6 = cidrs.filter { it.family == IpFamily.IPV6 }
    val collapsedV4 = CidrMath.collapse(ipv4)
    val collapsedV6 = CidrMath.collapse(ipv6)
    return ProbeIpDbMetadata(
        sha256 = sha256,
        rawRecordCount = rawRecordCount,
        ipv4RawCount = ipv4.size,
        ipv6RawCount = ipv6.size,
        ipv4CollapsedCount = collapsedV4.size,
        ipv6CollapsedCount = collapsedV6.size,
        ipv4ComplementCount = CidrMath.complement(collapsedV4, IpFamily.IPV4).size,
        ipv6ComplementCount = CidrMath.complement(collapsedV6, IpFamily.IPV6).size,
    )
}

private fun emptyIpDbMetadata(): ProbeIpDbMetadata = ProbeIpDbMetadata(
    sha256 = "UNAVAILABLE",
    rawRecordCount = -1,
    ipv4RawCount = -1,
    ipv6RawCount = -1,
    ipv4CollapsedCount = -1,
    ipv6CollapsedCount = -1,
    ipv4ComplementCount = -1,
    ipv6ComplementCount = -1,
)
