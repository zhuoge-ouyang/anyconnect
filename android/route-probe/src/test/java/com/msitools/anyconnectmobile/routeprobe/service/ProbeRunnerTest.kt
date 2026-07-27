package com.msitools.anyconnectmobile.routeprobe.service

import android.os.ParcelFileDescriptor
import com.msitools.anyconnectmobile.routeprobe.net.ProbeScenario
import com.msitools.anyconnectmobile.routeprobe.net.Cidr
import com.msitools.anyconnectmobile.routeprobe.net.IpFamily
import com.msitools.anyconnectmobile.routeprobe.net.RoutePlan
import com.msitools.anyconnectmobile.routeprobe.net.RouteStrategy
import com.msitools.anyconnectmobile.routeprobe.packet.DirectProbeResult
import com.msitools.anyconnectmobile.routeprobe.packet.DirectResponseStatus
import com.msitools.anyconnectmobile.routeprobe.report.GateDecision
import com.msitools.anyconnectmobile.routeprobe.report.CheckpointPhase
import com.msitools.anyconnectmobile.routeprobe.report.ProbeCheckpoint
import java.io.Closeable
import java.net.InetAddress
import java.util.ArrayDeque
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class ProbeRunnerTest {
    @Test
    fun allPrimaryScenariosAndTwentyHandoversPassInTheRequiredOrder() {
        val requests = mutableListOf<ProbeAttemptRequest>()
        val progress = mutableListOf<ProbeAttemptRequest>()
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider { request ->
                requests += request
                passingOutcome(establishMs = request.attempt.toLong())
            },
            progress = { progress += it },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.PASS, result.decision)
        assertEquals((1..26).toList(), requests.map(ProbeAttemptRequest::attempt))
        assertEquals(ProbeScenario.entries.toList(), requests.take(6).map { it.scenario })
        assertEquals(
            List(20) { ProbeScenario.FOREIGN_DIRECT_DUAL },
            requests.drop(6).map { it.scenario },
        )
        assertEquals(List(6) { false } + List(20) { true }, requests.map { it.isHandover })
        assertEquals(requests, progress)
        assertEquals(16L, result.handoverP50Ms)
        assertEquals(25L, result.handoverP95Ms)
    }

    @Test
    fun nullEstablishBlocksImmediatelyWithoutRetryingOrChangingPlan() {
        val requests = mutableListOf<ProbeAttemptRequest>()
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider { request ->
                requests += request
                passingOutcome(1).copy(establishStatus = EstablishStatus.NULL)
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, result.decision)
        assertEquals(
            listOf(ProbeAttemptRequest(1, ProbeScenario.DOMESTIC_DIRECT_IPV4, false)),
            requests,
        )
        assertNull(result.handoverP50Ms)
        assertNull(result.handoverP95Ms)
    }

    @Test
    fun exceptionBlocksAndDoesNotRetryTheFailedScenario() {
        val requests = mutableListOf<ProbeAttemptRequest>()
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider { request ->
                requests += request
                if (request.attempt == 3) {
                    passingOutcome(3).copy(establishStatus = EstablishStatus.EXCEPTION)
                } else {
                    passingOutcome(request.attempt.toLong())
                }
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, result.decision)
        assertEquals(listOf(1, 2, 3), requests.map { it.attempt })
        assertEquals(1, requests.count { it.scenario == ProbeScenario.DOMESTIC_DIRECT_DUAL })
    }

    @Test
    fun capturedPathFailureBlocksImmediately() {
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider {
                passingOutcome(1).copy(capturedChecksPassed = false)
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, result.decision)
        assertEquals(1, result.outcomes.size)
    }

    @Test
    fun helperCapturedPacketAllowsVpnOwnerUidIpv6CaptureMiss() {
        val requests = mutableListOf<ProbeAttemptRequest>()
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider { request ->
                requests += request
                if (request.attempt == 2) {
                    passingOutcome(request.attempt.toLong()).copy(
                        capturedChecksPassed = false,
                        helperCapturedOwnerUidMiss = true,
                    )
                } else {
                    passingOutcome(request.attempt.toLong())
                }
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.PASS, result.decision)
        assertEquals(26, requests.size)
    }

    @Test
    fun bypassPathFailureBlocksImmediately() {
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider {
                passingOutcome(1).copy(bypassChecksPassed = false)
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, result.decision)
        assertEquals(1, result.outcomes.size)
    }

    @Test
    fun missingPositiveDirectResponseIsInconclusiveAndStopsGateOne() {
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider {
                passingOutcome(1).copy(directResponseStatus = DirectPathStatus.INCONCLUSIVE)
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.INCONCLUSIVE, result.decision)
        assertEquals(1, result.outcomes.size)
    }

    @Test
    fun missingPhysicalDualStackIsInconclusiveWithoutRunningAnyPlan() {
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider {
                throw AssertionError("no route plan may run without physical dual stack")
            },
        )

        val result = controller.execute(physicalDualStackAvailable = false)

        assertEquals(GateDecision.INCONCLUSIVE, result.decision)
        assertEquals(emptyList<ProbeAttemptOutcome>(), result.outcomes)
        assertNull(result.handoverP50Ms)
        assertNull(result.handoverP95Ms)
    }

    @Test
    fun failedCandidateStopsTheHandoverSequenceWithoutPublishingPercentiles() {
        val controller = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider { request ->
                if (request.attempt == 7) {
                    passingOutcome(7).copy(establishStatus = EstablishStatus.EXCEPTION)
                } else {
                    passingOutcome(request.attempt.toLong())
                }
            },
        )

        val result = controller.execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, result.decision)
        assertEquals(7, result.outcomes.size)
        assertNull(result.handoverP50Ms)
        assertNull(result.handoverP95Ms)
    }

    @Test
    fun successfulCandidateTakesOwnershipBeforeTheOldTunCloses() {
        val owner = HandoverOwner<CountingCloseable>()
        val oldTun = CountingCloseable()
        val candidate = CountingCloseable()
        owner.seed(oldTun)

        assertTrue(owner.adoptCandidate(candidate))

        assertEquals(1, oldTun.closeCount)
        assertEquals(0, candidate.closeCount)
        owner.close()
        owner.close()
        assertEquals(1, oldTun.closeCount)
        assertEquals(1, candidate.closeCount)
    }

    @Test
    fun failedCandidateClosesBothCandidateAndOldTunExactlyOnce() {
        val owner = HandoverOwner<CountingCloseable>()
        val oldTun = CountingCloseable()
        val candidate = CountingCloseable()
        owner.seed(oldTun)

        owner.failCandidate(candidate)
        owner.close()

        assertEquals(1, oldTun.closeCount)
        assertEquals(1, candidate.closeCount)
    }

    @Test
    fun captureFactoryErrorRemainsPrimaryWhenDescriptorCloseAlsoFails() {
        val captureFactoryError = IllegalStateException("capture factory failed")
        val descriptorCloseError = IllegalArgumentException("descriptor close failed")
        val descriptor = ThrowingDescriptor(descriptorCloseError)
        val provider = attemptProvider(
            establish = { descriptor },
            captureFactory = { throw captureFactoryError },
        )

        val execution = ProbeExecutionController(provider).execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, execution.decision)
        val outcome = execution.outcomes.single()
        assertEquals(1, outcome.scenarioResult?.attempt)
        assertEquals(ProbeScenario.DOMESTIC_DIRECT_IPV4.name, outcome.scenarioResult?.scenario)
        assertSame(captureFactoryError, outcome.error)
        assertEquals(listOf(descriptorCloseError), outcome.error?.suppressed?.toList())
        assertEquals(captureFactoryError.javaClass.name, outcome.scenarioResult?.errorClass)
        assertEquals(captureFactoryError.message, outcome.scenarioResult?.errorMessage)
        assertEquals(1, descriptor.closeCount)
    }

    @Test
    fun builderErrorRemainsPrimaryWhenFailedHandoverOldTunCloseAlsoFails() {
        val builderError = SecurityException("builder failed")
        val oldCloseError = IllegalStateException("old TUN close failed")
        val oldCapture = ThrowingCapture(oldCloseError)
        val owner = HandoverOwner<ProbeCaptureSession>()
        owner.seed(oldCapture)
        val provider = attemptProvider<ThrowingDescriptor>(
            establish = { throw builderError },
            captureFactory = { error("capture must not be created") },
            captureOwner = owner,
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(7, ProbeScenario.FOREIGN_DIRECT_DUAL, true),
        )

        assertEquals(7, outcome.scenarioResult?.attempt)
        assertSame(builderError, outcome.error)
        assertEquals(listOf(oldCloseError), outcome.error?.suppressed?.toList())
        assertEquals(builderError.javaClass.name, outcome.scenarioResult?.errorClass)
        assertEquals(builderError.message, outcome.scenarioResult?.errorMessage)
        assertEquals(1, oldCapture.closeCount)
        owner.close()
        assertEquals(1, oldCapture.closeCount)
    }

    @Test
    fun planFactoryErrorRemainsPrimaryWhenFailedHandoverSeedCloseAlsoFails() {
        val planError = IllegalArgumentException("route plan failed")
        val seedCloseError = IllegalStateException("seed TUN close failed")
        val seedCapture = ThrowingCapture(seedCloseError)
        val owner = HandoverOwner<ProbeCaptureSession>()
        owner.seed(seedCapture)
        var establishCalls = 0
        val provider = attemptProvider<ThrowingDescriptor>(
            establish = {
                establishCalls++
                ThrowingDescriptor()
            },
            captureFactory = { error("capture must not be created") },
            captureOwner = owner,
            planFactory = { throw planError },
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(7, ProbeScenario.FOREIGN_DIRECT_DUAL, true),
        )

        assertEquals(7, outcome.scenarioResult?.attempt)
        assertEquals(ProbeScenario.FOREIGN_DIRECT_DUAL.name, outcome.scenarioResult?.scenario)
        assertSame(planError, outcome.error)
        assertEquals(listOf(seedCloseError), outcome.error?.suppressed?.toList())
        assertEquals(planError.javaClass.name, outcome.scenarioResult?.errorClass)
        assertTrue(outcome.scenarioResult?.errorMessage.orEmpty().startsWith(planError.message!!))
        assertEquals(0, establishCalls)
        assertEquals(1, seedCapture.closeCount)
        owner.close()
        assertEquals(1, seedCapture.closeCount)
    }

    @Test
    fun verificationErrorRemainsPrimaryWhenCaptureReleaseAlsoFails() {
        val verificationError = IllegalStateException("verification failed")
        val releaseError = IllegalArgumentException("capture release failed")
        val descriptor = ThrowingDescriptor()
        val capture = ThrowingCapture(releaseError)
        val provider = attemptProvider(
            establish = { descriptor },
            captureFactory = { capture },
            pathVerifier = pathVerifierThatThrows(verificationError),
        )

        val execution = ProbeExecutionController(provider).execute(physicalDualStackAvailable = true)

        assertEquals(GateDecision.BLOCKED, execution.decision)
        val outcome = execution.outcomes.single()
        assertEquals(1, outcome.scenarioResult?.attempt)
        assertSame(verificationError, outcome.error)
        assertEquals(listOf(releaseError), outcome.error?.suppressed?.toList())
        assertEquals(verificationError.javaClass.name, outcome.scenarioResult?.errorClass)
        assertEquals(verificationError.message, outcome.scenarioResult?.errorMessage)
        assertEquals(1, capture.closeCount)
        assertEquals(0, descriptor.closeCount)
    }

    @Test
    fun readinessRunsAfterCaptureCreationAndBeforeTheOnlyCapturedProbeSend() {
        val events = mutableListOf<String>()
        val descriptor = ThrowingDescriptor()
        val capture = RecordingCapture()
        val nonces = ArrayDeque(listOf(ByteArray(16) { 1 }, ByteArray(16) { 2 }))
        val provider = attemptProvider(
            establish = {
                events += "establish"
                descriptor
            },
            captureFactory = {
                events += "capture"
                capture
            },
            readiness = object : ProbeAttemptReadiness {
                override fun beforeEstablish(plan: RoutePlan): ProbeReadinessBaseline {
                    events += "baseline"
                    return ProbeReadinessBaseline(
                        activeOwnedVpnIdentity = VpnRouteIdentity(11L, "tun0"),
                        requiresFreshVpnIdentity = true,
                    )
                }

                override fun await(
                    plan: RoutePlan,
                    captureSession: ProbeCaptureSession,
                    baseline: ProbeReadinessBaseline,
                ) {
                    assertSame(capture, captureSession)
                    assertEquals(VpnRouteIdentity(11L, "tun0"), baseline.activeOwnedVpnIdentity)
                    assertTrue(baseline.requiresFreshVpnIdentity)
                    events += "readiness"
                }
            },
            pathVerifier = ProbePathVerifier(
                nonceSource = { nonces.removeFirst() },
                capturedSender = CapturedDatagramSender { _, _ ->
                    events += "send"
                    CapturedProbeSendObservation.UNAVAILABLE
                },
                directDnsVerifier = DirectDnsVerifier { _, _ ->
                    events += "direct"
                    DirectProbeResult(
                        status = DirectResponseStatus.PASS,
                        wireNonce = "wire".toByteArray(),
                        detail = "matching DNS response",
                    )
                },
            ),
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(1, ProbeScenario.DOMESTIC_DIRECT_IPV4, false),
        )

        assertEquals(EstablishStatus.SUCCESS, outcome.establishStatus)
        assertEquals(
            listOf("baseline", "establish", "capture", "readiness", "send", "direct"),
            events,
        )
    }

    @Test
    fun readinessFailureIsReportedAndNoProbePacketIsSent() {
        val failure = VpnRouteReadinessException("VPN route readiness timed out")
        var sendCount = 0
        val capture = RecordingCapture()
        val provider = attemptProvider(
            establish = { ThrowingDescriptor() },
            captureFactory = { capture },
            readiness = object : ProbeAttemptReadiness {
                override fun beforeEstablish(plan: RoutePlan): ProbeReadinessBaseline =
                    ProbeReadinessBaseline(null, false)

                override fun await(
                    plan: RoutePlan,
                    capture: ProbeCaptureSession,
                    baseline: ProbeReadinessBaseline,
                ): Unit = throw failure
            },
            pathVerifier = ProbePathVerifier(
                nonceSource = { ByteArray(16) },
                capturedSender = CapturedDatagramSender { _, _ ->
                    sendCount++
                    CapturedProbeSendObservation.UNAVAILABLE
                },
                directDnsVerifier = DirectDnsVerifier { _, _ ->
                    throw AssertionError("direct verification must not run")
                },
            ),
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(1, ProbeScenario.DOMESTIC_DIRECT_IPV4, false),
        )

        assertSame(failure, outcome.error)
        assertEquals(failure.javaClass.name, outcome.scenarioResult?.errorClass)
        assertEquals(failure.message, outcome.scenarioResult?.errorMessage)
        assertEquals(0, sendCount)
    }

    @Test
    fun capturedMissReportsReaderDiagnosticsWithoutChangingSuccessfulEstablishStatus() {
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean = destination.hostAddress != "8.8.8.8"

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false

            override fun diagnosticSummary(
                destination: InetAddress,
                nonce: ByteArray,
            ): String = "readerStarted=true readCount=0 parsedCount=0 eof=false"

            override fun close() = Unit
        }
        val provider = attemptProvider(
            establish = { ThrowingDescriptor() },
            captureFactory = { capture },
            pathVerifier = ProbePathVerifier(
                nonceSource = ArrayDeque(
                    listOf(ByteArray(16) { 1 }, ByteArray(16) { 2 }),
                )::removeFirst,
                capturedSender = CapturedDatagramSender { _, _ ->
                    CapturedProbeSendObservation.UNAVAILABLE
                },
                directDnsVerifier = DirectDnsVerifier { _, _ ->
                    DirectProbeResult(
                        status = DirectResponseStatus.PASS,
                        wireNonce = "wire".toByteArray(),
                        detail = "matching DNS response",
                    )
                },
            ),
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(1, ProbeScenario.DOMESTIC_DIRECT_IPV4, false),
        )

        assertEquals(EstablishStatus.SUCCESS, outcome.establishStatus)
        assertFalse(outcome.capturedChecksPassed)
        assertEquals(ProbeCaptureMissException::class.java.name, outcome.scenarioResult?.errorClass)
        assertTrue(
            outcome.scenarioResult?.errorMessage.orEmpty()
                .contains("readerStarted=true readCount=0 parsedCount=0 eof=false"),
        )
    }

    @Test
    fun ipv6CaptureMissReportsWhetherTheUdpSocketSelectedTheVpnSourceAddress() {
        val target = InetAddress.getByName("2001:4860:4860::8888")
        val physicalSource = InetAddress.getByName("2001:db8::9")
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean = false

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false

            override fun diagnosticSummary(destination: InetAddress, nonce: ByteArray): String =
                "readerStarted=true readCount=5 parsedCount=5"

            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { ByteArray(16) { 7 } },
            capturedSender = CapturedDatagramSender { _, _ ->
                CapturedProbeSendObservation(physicalSource)
            },
            directDnsVerifier = DirectDnsVerifier {
                    _, _ -> throw AssertionError("IPv6 capture miss must not run bypass verification")
            },
        )
        val plan = RoutePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_IPV6,
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = listOf(Cidr.parse("2001:4860:4860::8888/128")),
            excludes = emptyList(),
            directSet = emptyList(),
            syntheticMappings = emptyList(),
            expectedCaptured = listOf(target),
            expectedBypass = emptyList(),
        )

        val result = verifier.verify(plan, capture)

        assertFalse(result.capturedChecksPassed)
        assertTrue(result.captureError?.message.orEmpty().contains("senderSource=NON_VPN"))
        assertTrue(result.captureError?.message.orEmpty().contains("senderFamily=IPV6"))
    }

    @Test
    fun helperUidCapturedPathMissReportsWhetherItEnteredTun() {
        val target = InetAddress.getByName("2001:4860:4860::8888")
        val helperNonces = ArrayDeque(
            listOf(
                ByteArray(16) { 12 },
                ByteArray(16) { 13 },
                ByteArray(16) { 14 },
            ),
        )
        val helperSends = mutableListOf<Pair<InetAddress, ByteArray>>()
        val awaitedNonces = mutableListOf<ByteArray>()
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean {
                awaitedNonces += nonce
                return false
            }

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false

            override fun diagnosticSummary(destination: InetAddress, nonce: ByteArray): String =
                "readerStarted=true readCount=5 parsedCount=5"

            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { helperNonces.removeFirst() },
            capturedSender = CapturedDatagramSender { _, _ ->
                throw AssertionError("VPN owner UID must not be the primary route proof")
            },
            directDnsVerifier = DirectDnsVerifier {
                    _, _ -> throw AssertionError("no bypass verification in this scenario")
            },
            helperSender = HelperProbeSender { address, nonce ->
                helperSends += address to nonce
                HelperProbeSendResult.success()
            },
        )
        val plan = RoutePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_IPV6,
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = listOf(Cidr.parse("2001:4860:4860::8888/128")),
            excludes = emptyList(),
            directSet = emptyList(),
            syntheticMappings = emptyList(),
            expectedCaptured = listOf(target),
            expectedBypass = emptyList(),
        )

        val result = verifier.verify(plan, capture)

        assertFalse(result.capturedChecksPassed)
        assertFalse(result.helperCapturedOwnerUidMiss)
        assertEquals(3, helperSends.size)
        assertEquals(helperSends.map { it.second.toList() }, awaitedNonces.map { it.toList() })
        assertTrue(result.captureError?.message.orEmpty().contains("helperSend=SUCCESS"))
        assertTrue(result.captureError?.message.orEmpty().contains("helperCaptured=false"))
        assertTrue(result.captureError?.message.orEmpty().contains("helperAttempt=3"))
    }

    @Test
    fun helperUidProbeIsAuthoritativeForCapturedPathWhenVpnOwnerSocketMissesTun() {
        val target = InetAddress.getByName("2001:4860:4860::8888")
        val helperNonce = ByteArray(16) { 21 }
        val helperSends = mutableListOf<Pair<InetAddress, ByteArray>>()
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean = nonce.contentEquals(helperNonce)

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false
            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { helperNonce },
            capturedSender = CapturedDatagramSender {
                    _, _ -> throw AssertionError("VPN owner UID must not be the primary route proof")
            },
            directDnsVerifier = DirectDnsVerifier {
                    _, _ -> throw AssertionError("no bypass verification in this scenario")
            },
            helperSender = HelperProbeSender { address, nonce ->
                helperSends += address to nonce
                HelperProbeSendResult.success()
            },
        )
        val plan = RoutePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_IPV6,
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = listOf(Cidr.parse("2001:4860:4860::8888/128")),
            excludes = emptyList(),
            directSet = emptyList(),
            syntheticMappings = emptyList(),
            expectedCaptured = listOf(target),
            expectedBypass = emptyList(),
        )

        val result = verifier.verify(plan, capture)

        assertTrue(result.capturedChecksPassed)
        assertFalse(result.helperCapturedOwnerUidMiss)
        assertNull(result.captureError)
        assertEquals(listOf(target to helperNonce), helperSends)
    }

    @Test
    fun helperUidCapturedPathRetriesUntilTheVpnDataPlaneAcceptsIpv6Packets() {
        val target = InetAddress.getByName("2001:4860:4860::8888")
        val firstNonce = ByteArray(16) { 31 }
        val secondNonce = ByteArray(16) { 32 }
        val nonces = ArrayDeque(listOf(firstNonce, secondNonce))
        val helperSends = mutableListOf<Pair<InetAddress, ByteArray>>()
        val awaitedNonces = mutableListOf<ByteArray>()
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean {
                awaitedNonces += nonce
                return nonce.contentEquals(secondNonce)
            }

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false
            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { nonces.removeFirst() },
            capturedSender = CapturedDatagramSender {
                    _, _ -> throw AssertionError("VPN owner UID must not warm up helper proof")
            },
            directDnsVerifier = DirectDnsVerifier {
                    _, _ -> throw AssertionError("no bypass verification in this scenario")
            },
            helperSender = HelperProbeSender { address, nonce ->
                helperSends += address to nonce
                HelperProbeSendResult.success()
            },
        )
        val plan = RoutePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_IPV6,
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = listOf(Cidr.parse("2001:4860:4860::8888/128")),
            excludes = emptyList(),
            directSet = emptyList(),
            syntheticMappings = emptyList(),
            expectedCaptured = listOf(target),
            expectedBypass = emptyList(),
        )

        val result = verifier.verify(plan, capture)

        assertTrue(result.capturedChecksPassed)
        assertNull(result.captureError)
        assertEquals(listOf(firstNonce.toList(), secondNonce.toList()), helperSends.map { it.second.toList() })
        assertEquals(listOf(firstNonce.toList(), secondNonce.toList()), awaitedNonces.map { it.toList() })
    }

    @Test
    fun helperUidProbeIsAuthoritativeForBypassPathWhenDnsResponseIsInconclusive() {
        val target = InetAddress.getByName("2001:4860:4860::8888")
        val helperNonce = ByteArray(16) { 22 }
        val helperSends = mutableListOf<Pair<InetAddress, ByteArray>>()
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean = false

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false
            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { helperNonce },
            capturedSender = CapturedDatagramSender {
                    _, _ -> throw AssertionError("VPN owner UID must not be used for bypass proof")
            },
            directDnsVerifier = DirectDnsVerifier {
                    _, _ -> throw AssertionError("DNS response must not decide helper UID bypass proof")
            },
            helperSender = HelperProbeSender { address, nonce ->
                helperSends += address to nonce
                HelperProbeSendResult.success()
            },
        )
        val plan = RoutePlan(
            scenario = ProbeScenario.FOREIGN_DIRECT_IPV6,
            strategy = RouteStrategy.INCLUDES_ONLY,
            includes = emptyList(),
            excludes = emptyList(),
            directSet = listOf(Cidr.parse("2001:4860:4860::8888/128")),
            syntheticMappings = emptyList(),
            expectedCaptured = emptyList(),
            expectedBypass = listOf(target),
        )

        val result = verifier.verify(plan, capture)

        assertTrue(result.bypassChecksPassed)
        assertFalse(result.bypassPathCaptured)
        assertEquals(DirectPathStatus.PASS, result.directResponseStatus)
        assertEquals(listOf(target to helperNonce), helperSends)
    }

    @Test
    fun adoptCloseErrorRemainsPrimaryWhenCandidateReleaseAlsoFails() {
        val oldCloseError = IllegalStateException("old TUN close failed")
        val candidateCloseError = IllegalArgumentException("candidate close failed")
        val oldCapture = ThrowingCapture(oldCloseError)
        val candidateCapture = ThrowingCapture(candidateCloseError)
        val owner = HandoverOwner<ProbeCaptureSession>()
        owner.seed(oldCapture)
        val provider = attemptProvider(
            establish = { ThrowingDescriptor() },
            captureFactory = { candidateCapture },
            captureOwner = owner,
        )

        val outcome = provider.execute(
            ProbeAttemptRequest(7, ProbeScenario.FOREIGN_DIRECT_DUAL, true),
        )

        assertEquals(7, outcome.scenarioResult?.attempt)
        assertSame(oldCloseError, outcome.error)
        assertEquals(listOf(candidateCloseError), outcome.error?.suppressed?.toList())
        assertEquals(oldCloseError.javaClass.name, outcome.scenarioResult?.errorClass)
        assertEquals(oldCloseError.message, outcome.scenarioResult?.errorMessage)
        assertEquals(1, oldCapture.closeCount)
        assertEquals(1, candidateCapture.closeCount)
        owner.close()
        assertEquals(1, candidateCapture.closeCount)
    }

    @Test
    fun cancellationWinningTheSwapRaceClosesTheLateCandidate() {
        val owner = HandoverOwner<CountingCloseable>()
        val oldTun = CountingCloseable()
        val candidate = CountingCloseable()
        owner.seed(oldTun)

        owner.close()

        assertFalse(owner.adoptCandidate(candidate))
        assertEquals(1, oldTun.closeCount)
        assertEquals(1, candidate.closeCount)
    }

    @Test
    fun candidateSwapDoesNotHoldTheOwnershipLockWhileClosingOldTun() {
        val owner = HandoverOwner<Closeable>()
        val closeEntered = CountDownLatch(1)
        val allowOldClose = CountDownLatch(1)
        val cancellationFinished = CountDownLatch(1)
        val oldTun = Closeable {
            closeEntered.countDown()
            allowOldClose.await(5, TimeUnit.SECONDS)
        }
        val candidate = CountingCloseable()
        owner.seed(oldTun)

        val swapThread = thread(start = true) { owner.adoptCandidate(candidate) }
        assertTrue(closeEntered.await(1, TimeUnit.SECONDS))
        val cancellationThread = thread(start = true) {
            owner.close()
            cancellationFinished.countDown()
        }

        try {
            assertTrue(cancellationFinished.await(1, TimeUnit.SECONDS))
        } finally {
            allowOldClose.countDown()
            swapThread.join()
            cancellationThread.join()
        }
        assertEquals(1, candidate.closeCount)
    }

    @Test
    fun lifecycleRejectsConcurrentRunAndCancellationIsOneShot() {
        val lifecycle = ProbeRunLifecycle()

        assertEquals(ProbeRunStartResult.STARTED, lifecycle.tryStart())
        assertEquals(ProbeRunStartResult.ALREADY_RUNNING, lifecycle.tryStart())
        assertTrue(lifecycle.cancelOnce())
        assertFalse(lifecycle.cancelOnce())
        assertEquals(ProbeRunStartResult.TERMINAL, lifecycle.tryStart())
    }

    @Test
    fun finishedLifecycleIsTerminalRatherThanAlreadyRunning() {
        val lifecycle = ProbeRunLifecycle()

        assertEquals(ProbeRunStartResult.STARTED, lifecycle.tryStart())
        lifecycle.finish()

        assertEquals(ProbeRunStartResult.TERMINAL, lifecycle.tryStart())
        assertFalse(lifecycle.isRunning())
    }

    @Test
    fun unsupportedActionDoesNotAttemptToUnpackReceiver() {
        var receiverReads = 0

        val request = parseProbeStartRequest(
            action = "unsupported",
            expectedAction = "run",
            receiverLoader = {
                receiverReads++
                throw AssertionError("receiver must not be read for an unsupported action")
            },
        )

        assertEquals(ProbeStartRequest.UnsupportedAction, request)
        assertEquals(0, receiverReads)
    }

    @Test
    fun corruptReceiverIsReturnedAsInvalidInsteadOfEscapingParser() {
        val corruptReceiver = ClassCastException("wrong parcelable type")

        val request = parseProbeStartRequest<Any>(
            action = "run",
            expectedAction = "run",
            receiverLoader = { throw corruptReceiver },
        )

        assertTrue(request is ProbeStartRequest.InvalidReceiver)
        assertSame(corruptReceiver, (request as ProbeStartRequest.InvalidReceiver).error)
    }

    @Test
    fun builderAppliesEveryEntryAndAllowsOnlyTheMissingFamily() {
        val includes = listOf(Cidr.parse("0.0.0.0/0"), Cidr.parse("8.8.8.8/32"))
        val excludes = listOf(Cidr.parse("10.0.0.0/8"), Cidr.parse("192.168.0.0/16"))
        val plan = routePlan(
            scenario = ProbeScenario.FOREIGN_DIRECT_IPV4,
            includes = includes,
            excludes = excludes,
        )
        val builder = RecordingBuilder()

        applyRoutePlan(builder, plan, sdkInt = 35)

        assertEquals("AnyConnect Route Probe: FOREIGN_DIRECT_IPV4", builder.recordedSession)
        assertEquals(1500, builder.recordedMtu)
        assertTrue(builder.recordedBlocking)
        assertEquals(listOf("10.252.0.2/32"), builder.addresses)
        assertEquals(listOf(IpFamily.IPV6), builder.allowedFamilies)
        assertEquals(includes, builder.includes)
        assertEquals(excludes, builder.excludes)
    }

    @Test
    fun dualStackBuilderUsesExactlyOneAddressPerFamilyAndNoAllowFamily() {
        val plan = routePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_DUAL,
            includes = listOf(Cidr.parse("8.8.8.8/32"), Cidr.parse("2001:db8::1/128")),
        )
        val builder = RecordingBuilder()

        applyRoutePlan(builder, plan, sdkInt = 26)

        assertEquals(listOf("10.252.0.2/32", "fd00:252::2/128"), builder.addresses)
        assertEquals(emptyList<IpFamily>(), builder.allowedFamilies)
        assertEquals(plan.includes, builder.includes)
        assertEquals(emptyList<Cidr>(), builder.excludes)
    }

    @Test
    fun excludesBelowApi33FailClosedWithoutCallingEstablish() {
        val builder = RecordingBuilder()
        val plan = routePlan(
            scenario = ProbeScenario.FOREIGN_DIRECT_IPV4,
            includes = listOf(Cidr.parse("0.0.0.0/0")),
            excludes = listOf(Cidr.parse("10.0.0.0/8")),
        )

        assertThrows(IllegalArgumentException::class.java) {
            applyRoutePlan(builder, plan, sdkInt = 32)
        }
        assertEquals(0, builder.establishCount)
    }

    @Test
    fun normalizedPlanHashIsOrderIndependentButIncludesStrategyAndEntries() {
        val first = routePlan(
            scenario = ProbeScenario.FOREIGN_DIRECT_IPV4,
            includes = listOf(Cidr.parse("8.8.8.8/32"), Cidr.parse("0.0.0.0/0")),
            excludes = listOf(Cidr.parse("192.168.0.0/16"), Cidr.parse("10.0.0.0/8")),
        )
        val reordered = first.copy(
            includes = first.includes.reversed(),
            excludes = first.excludes.reversed(),
        )
        val changed = first.copy(excludes = listOf(Cidr.parse("10.0.0.0/8")))

        assertEquals(normalizedPlanSha256(first), normalizedPlanSha256(reordered))
        assertNotEquals(normalizedPlanSha256(first), normalizedPlanSha256(changed))
    }

    @Test
    fun checkpointsBracketSuccessfulEstablishAndUseElapsedRealtime() {
        val events = mutableListOf<String>()
        val checkpoints = mutableListOf<ProbeCheckpoint>()
        val recorder = establishRecorder(
            clockValues = listOf(90L, 100L, 125L),
            checkpoints = checkpoints,
            events = events,
        )

        val result = recorder.execute(establishMetadata()) {
            events += "establish"
            Any()
        }

        assertEquals(EstablishStatus.SUCCESS, result.status)
        assertEquals(25L, result.establishMs)
        assertEquals(listOf("BEFORE_ESTABLISH", "establish", "AFTER_ESTABLISH"), events)
        assertEquals(
            listOf(CheckpointPhase.BEFORE_ESTABLISH, CheckpointPhase.AFTER_ESTABLISH),
            checkpoints.map { it.phase },
        )
        assertEquals(listOf(90L, 125L), checkpoints.map { it.elapsedRealtime })
    }

    @Test
    fun metricsBeforeFailureDoesNotWriteCheckpointOrCallBuilder() {
        val checkpoints = mutableListOf<ProbeCheckpoint>()
        var clockCalls = 0
        var establishCalls = 0
        val original = IllegalStateException("metrics before failed")
        val recorder = ProbeEstablishRecorder(
            elapsedRealtime = {
                clockCalls++
                100L
            },
            processMetrics = { throw original },
            checkpointWriter = ProbeCheckpointWriter(checkpoints::add),
        )

        val thrown = assertThrows(IllegalStateException::class.java) {
            recorder.execute<Any>(establishMetadata()) {
                establishCalls++
                Any()
            }
        }

        assertSame(original, thrown)
        assertEquals(0, clockCalls)
        assertEquals(0, establishCalls)
        assertTrue(checkpoints.isEmpty())
    }

    @Test
    fun builderErrorRemainsPrimaryWhenAfterCheckpointAndMetricsAfterAlsoFail() {
        val attemptedPhases = mutableListOf<CheckpointPhase>()
        var metricsCalls = 0
        val builderError = SecurityException("builder failed")
        val afterCheckpointError = IllegalStateException("after checkpoint failed")
        val metricsAfterError = IllegalArgumentException("metrics after failed")
        val clock = ArrayDeque(listOf(100L, 110L, 120L))
        val recorder = ProbeEstablishRecorder(
            elapsedRealtime = { clock.removeFirst() },
            processMetrics = {
                metricsCalls++
                if (metricsCalls == 1) {
                    ProcessMetrics(10_000, 10_100, 20)
                } else {
                    throw metricsAfterError
                }
            },
            checkpointWriter = ProbeCheckpointWriter { checkpoint ->
                attemptedPhases += checkpoint.phase
                if (checkpoint.phase == CheckpointPhase.AFTER_ESTABLISH) {
                    throw afterCheckpointError
                }
            },
        )

        val thrown = assertThrows(SecurityException::class.java) {
            recorder.execute<Any>(establishMetadata()) { throw builderError }
        }

        assertSame(builderError, thrown)
        assertEquals("builder failed", thrown.message)
        assertEquals(
            listOf(afterCheckpointError, metricsAfterError),
            thrown.suppressed.toList(),
        )
        assertEquals(
            listOf(CheckpointPhase.BEFORE_ESTABLISH, CheckpointPhase.AFTER_ESTABLISH),
            attemptedPhases,
        )
        assertEquals(2, metricsCalls)
    }

    @Test
    fun firstPostEstablishErrorIsPrimaryAndLaterCleanupErrorIsSuppressed() {
        var metricsCalls = 0
        val afterCheckpointError = IllegalStateException("after checkpoint failed")
        val metricsAfterError = IllegalArgumentException("metrics after failed")
        val clock = ArrayDeque(listOf(200L, 210L, 220L))
        val recorder = ProbeEstablishRecorder(
            elapsedRealtime = { clock.removeFirst() },
            processMetrics = {
                metricsCalls++
                if (metricsCalls == 1) {
                    ProcessMetrics(10_000, 10_100, 20)
                } else {
                    throw metricsAfterError
                }
            },
            checkpointWriter = ProbeCheckpointWriter { checkpoint ->
                if (checkpoint.phase == CheckpointPhase.AFTER_ESTABLISH) {
                    throw afterCheckpointError
                }
            },
        )

        val thrown = assertThrows(IllegalStateException::class.java) {
            recorder.execute(establishMetadata()) { Any() }
        }

        assertSame(afterCheckpointError, thrown)
        assertEquals(listOf(metricsAfterError), thrown.suppressed.toList())
        assertEquals(2, metricsCalls)
    }

    @Test
    fun exceptionStillWritesAfterCheckpointAndPreservesOriginalError() {
        val events = mutableListOf<String>()
        val checkpoints = mutableListOf<ProbeCheckpoint>()
        val recorder = establishRecorder(
            clockValues = listOf(190L, 200L, 212L),
            checkpoints = checkpoints,
            events = events,
        )
        val original = SecurityException("VPN permission revoked")

        val result = recorder.execute<Any>(establishMetadata()) {
            events += "establish"
            throw original
        }

        assertEquals(EstablishStatus.EXCEPTION, result.status)
        assertSame(original, result.error)
        assertEquals("java.lang.SecurityException", result.errorClass)
        assertEquals("VPN permission revoked", result.errorMessage)
        assertEquals(listOf("BEFORE_ESTABLISH", "establish", "AFTER_ESTABLISH"), events)
    }

    @Test
    fun nullStillWritesAfterCheckpointAndIsNotRetried() {
        val checkpoints = mutableListOf<ProbeCheckpoint>()
        val recorder = establishRecorder(
            clockValues = listOf(290L, 300L, 301L),
            checkpoints = checkpoints,
            events = mutableListOf(),
        )

        val result = recorder.execute<Any>(establishMetadata()) { null }

        assertEquals(EstablishStatus.NULL, result.status)
        assertNull(result.value)
        assertNull(result.error)
        assertEquals(2, checkpoints.size)
    }

    @Test
    fun bypassProofChecksTheDnsWireNonceRatherThanTheRawNonce() {
        val capturedNonce = ByteArray(16) { 1 }
        val directRawNonce = ByteArray(16) { 2 }
        val dnsWireNonce = "probe-wire-nonce".toByteArray()
        val capture = RecordingCapture()
        val sent = mutableListOf<Pair<InetAddress, ByteArray>>()
        val directInputs = mutableListOf<Pair<InetAddress, ByteArray>>()
        val nonces = ArrayDeque(listOf(capturedNonce, directRawNonce))
        val verifier = ProbePathVerifier(
            nonceSource = { nonces.removeFirst() },
            capturedSender = CapturedDatagramSender { address, nonce ->
                sent += address to nonce
                CapturedProbeSendObservation.UNAVAILABLE
            },
            directDnsVerifier = DirectDnsVerifier { address, nonce ->
                directInputs += address to nonce
                DirectProbeResult(
                    status = DirectResponseStatus.PASS,
                    wireNonce = dnsWireNonce,
                    detail = "matching DNS response",
                )
            },
        )

        val result = verifier.verify(
            plan = routePlan(
                scenario = ProbeScenario.DOMESTIC_DIRECT_IPV4,
                includes = listOf(Cidr.parse("8.8.8.8/32")),
            ),
            capture = capture,
        )

        assertTrue(result.capturedChecksPassed)
        assertTrue(result.bypassChecksPassed)
        assertEquals(DirectPathStatus.PASS, result.directResponseStatus)
        assertEquals(capturedNonce.toList(), sent.single().second.toList())
        assertEquals(directRawNonce.toList(), directInputs.single().second.toList())
        assertEquals(dnsWireNonce.toList(), capture.awaitedNonces.last().toList())
        assertEquals(0, capture.capturedCallCount)
    }

    @Test
    fun missingDnsResponseIsInconclusiveAndNeverPassesBypassProof() {
        val capture = RecordingCapture()
        val nonces = ArrayDeque(listOf(ByteArray(16) { 3 }, ByteArray(16) { 4 }))
        val verifier = ProbePathVerifier(
            nonceSource = { nonces.removeFirst() },
            capturedSender = CapturedDatagramSender { _, _ ->
                CapturedProbeSendObservation.UNAVAILABLE
            },
            directDnsVerifier = DirectDnsVerifier { _, _ ->
                DirectProbeResult(
                    status = DirectResponseStatus.INCONCLUSIVE,
                    wireNonce = "wire".toByteArray(),
                    detail = "timeout",
                )
            },
        )

        val result = verifier.verify(
            plan = routePlan(
                scenario = ProbeScenario.DOMESTIC_DIRECT_IPV4,
                includes = listOf(Cidr.parse("8.8.8.8/32")),
            ),
            capture = capture,
        )

        assertEquals(DirectPathStatus.INCONCLUSIVE, result.directResponseStatus)
        assertFalse(result.bypassChecksPassed)
        assertFalse(result.bypassPathCaptured)
    }

    @Test
    fun dnsTimeoutWithWireNonceCapturedInTunIsBlockedAsARouteFailure() {
        val nonces = ArrayDeque(listOf(ByteArray(16) { 5 }, ByteArray(16) { 6 }))
        val capture = object : ProbeCaptureSession {
            override fun await(
                destination: InetAddress,
                nonce: ByteArray,
                timeoutMs: Long,
            ): Boolean = true

            override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = true
            override fun close() = Unit
        }
        val verifier = ProbePathVerifier(
            nonceSource = { nonces.removeFirst() },
            capturedSender = CapturedDatagramSender { _, _ ->
                CapturedProbeSendObservation.UNAVAILABLE
            },
            directDnsVerifier = DirectDnsVerifier { _, _ ->
                DirectProbeResult(
                    status = DirectResponseStatus.INCONCLUSIVE,
                    wireNonce = "captured-wire".toByteArray(),
                    detail = "timeout",
                )
            },
        )
        val verification = verifier.verify(
            plan = routePlan(
                scenario = ProbeScenario.DOMESTIC_DIRECT_IPV4,
                includes = listOf(Cidr.parse("8.8.8.8/32")),
            ),
            capture = capture,
        )
        val decision = ProbeExecutionController(
            outcomeProvider = ProbeAttemptOutcomeProvider {
                passingOutcome(1).copy(
                    bypassChecksPassed = verification.bypassChecksPassed,
                    directResponseStatus = verification.directResponseStatus,
                    bypassPathCaptured = verification.bypassPathCaptured,
                )
            },
        ).execute(physicalDualStackAvailable = true)

        assertTrue(verification.bypassPathCaptured)
        assertEquals(GateDecision.BLOCKED, decision.decision)
    }

    private fun passingOutcome(establishMs: Long) = ProbeAttemptOutcome(
        establishStatus = EstablishStatus.SUCCESS,
        establishMs = establishMs,
        capturedChecksPassed = true,
        bypassChecksPassed = true,
        directResponseStatus = DirectPathStatus.PASS,
    )

    private class CountingCloseable : Closeable {
        var closeCount: Int = 0
            private set

        override fun close() {
            closeCount++
        }
    }

    private class ThrowingDescriptor(
        private val closeError: Exception? = null,
    ) : Closeable {
        var closeCount: Int = 0
            private set

        override fun close() {
            closeCount++
            closeError?.let { throw it }
        }
    }

    private class ThrowingCapture(
        private val closeError: Exception? = null,
    ) : ProbeCaptureSession {
        var closeCount: Int = 0
            private set

        override fun await(destination: InetAddress, nonce: ByteArray, timeoutMs: Long): Boolean =
            false

        override fun captured(destination: InetAddress, nonce: ByteArray): Boolean = false

        override fun close() {
            closeCount++
            closeError?.let { throw it }
        }
    }

    private class RecordingBuilder : ProbeBuilderFacade {
        var recordedSession: String? = null
        var recordedMtu: Int? = null
        var recordedBlocking: Boolean = false
        val addresses = mutableListOf<String>()
        val allowedFamilies = mutableListOf<IpFamily>()
        val includes = mutableListOf<Cidr>()
        val excludes = mutableListOf<Cidr>()
        var establishCount = 0

        override fun setSession(value: String) {
            recordedSession = value
        }

        override fun setMtu(value: Int) {
            recordedMtu = value
        }

        override fun setBlocking(value: Boolean) {
            recordedBlocking = value
        }

        override fun addAddress(address: String, prefixLength: Int) {
            addresses += "$address/$prefixLength"
        }

        override fun allowFamily(family: IpFamily) {
            allowedFamilies += family
        }

        override fun addRoute(cidr: Cidr) {
            includes += cidr
        }

        override fun excludeRoute(cidr: Cidr) {
            excludes += cidr
        }

        override fun establish(): ParcelFileDescriptor? {
            establishCount++
            return null
        }
    }

    private class RecordingCapture : ProbeCaptureSession {
        val awaitedNonces = mutableListOf<ByteArray>()
        var capturedCallCount = 0

        override fun await(destination: InetAddress, nonce: ByteArray, timeoutMs: Long): Boolean {
            awaitedNonces += nonce
            return destination.hostAddress == "8.8.8.8"
        }

        override fun captured(destination: InetAddress, nonce: ByteArray): Boolean {
            capturedCallCount++
            return false
        }

        override fun close() = Unit
    }

    private fun routePlan(
        scenario: ProbeScenario,
        includes: List<Cidr>,
        excludes: List<Cidr> = emptyList(),
    ): RoutePlan = RoutePlan(
        scenario = scenario,
        strategy = if (excludes.isEmpty()) {
            RouteStrategy.INCLUDES_ONLY
        } else {
            RouteStrategy.DEFAULT_WITH_EXCLUDES
        },
        includes = includes,
        excludes = excludes,
        directSet = excludes,
        syntheticMappings = emptyList(),
        expectedCaptured = listOf(InetAddress.getByName("8.8.8.8")),
        expectedBypass = listOf(InetAddress.getByName("114.114.114.114")),
    )

    private fun establishMetadata(): ProbeEstablishMetadata {
        val plan = routePlan(
            scenario = ProbeScenario.DOMESTIC_DIRECT_IPV4,
            includes = listOf(Cidr.parse("8.8.8.8/32")),
        )
        return ProbeEstablishMetadata(
            runId = "run-1",
            request = ProbeAttemptRequest(1, plan.scenario, false),
            plan = plan,
            planSha256 = normalizedPlanSha256(plan),
        )
    }

    private fun establishRecorder(
        clockValues: List<Long>,
        checkpoints: MutableList<ProbeCheckpoint>,
        events: MutableList<String>,
    ): ProbeEstablishRecorder {
        val clock = ArrayDeque(clockValues)
        val metrics = ArrayDeque(
            listOf(
                ProcessMetrics(10_000, 10_100, 20),
                ProcessMetrics(10_050, 10_200, 21),
            ),
        )
        return ProbeEstablishRecorder(
            elapsedRealtime = { clock.removeFirst() },
            processMetrics = { metrics.removeFirst() },
            checkpointWriter = ProbeCheckpointWriter { checkpoint ->
                checkpoints += checkpoint
                events += checkpoint.phase.name
            },
        )
    }

    private fun <T : Closeable> attemptProvider(
        establish: (RoutePlan) -> T?,
        captureFactory: (T) -> ProbeCaptureSession,
        captureOwner: HandoverOwner<ProbeCaptureSession> = HandoverOwner(),
        readiness: ProbeAttemptReadiness = object : ProbeAttemptReadiness {
            override fun beforeEstablish(plan: RoutePlan): ProbeReadinessBaseline =
                ProbeReadinessBaseline(null, false)

            override fun await(
                plan: RoutePlan,
                capture: ProbeCaptureSession,
                baseline: ProbeReadinessBaseline,
            ) = Unit
        },
        planFactory: (ProbeScenario) -> RoutePlan = { scenario ->
            routePlan(
                scenario = scenario,
                includes = listOf(Cidr.parse("8.8.8.8/32")),
            )
        },
        pathVerifier: ProbePathVerifier = ProbePathVerifier(
            nonceSource = { ByteArray(16) },
            capturedSender = CapturedDatagramSender { _, _ ->
                CapturedProbeSendObservation.UNAVAILABLE
            },
            directDnsVerifier = DirectDnsVerifier { _, _ ->
                DirectProbeResult(
                    status = DirectResponseStatus.PASS,
                    wireNonce = "wire".toByteArray(),
                    detail = "matching DNS response",
                )
            },
        ),
    ): AndroidAttemptOutcomeProvider<T> {
        var elapsed = 0L
        return AndroidAttemptOutcomeProvider(
            planFactory = planFactory,
            runId = "run-provider-test",
            establish = establish,
            recorder = ProbeEstablishRecorder(
                elapsedRealtime = { ++elapsed },
                processMetrics = { ProcessMetrics(10_000, 10_100, 20) },
                checkpointWriter = ProbeCheckpointWriter {},
            ),
            captureFactory = captureFactory,
            captureOwner = captureOwner,
            readiness = readiness,
            pathVerifier = pathVerifier,
        )
    }

    private fun pathVerifierThatThrows(error: Exception): ProbePathVerifier = ProbePathVerifier(
        nonceSource = { ByteArray(16) },
        capturedSender = CapturedDatagramSender { _, _ -> throw error },
        directDnsVerifier = DirectDnsVerifier { _, _ ->
            throw AssertionError("direct DNS verification must not run")
        },
    )
}
