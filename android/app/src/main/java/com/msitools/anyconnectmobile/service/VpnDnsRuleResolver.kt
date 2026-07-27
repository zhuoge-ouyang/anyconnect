package com.msitools.anyconnectmobile.service

import android.annotation.TargetApi
import android.net.DnsResolver
import android.net.Network
import android.os.Build
import android.os.CancellationSignal
import com.msitools.anyconnectmobile.core.IpCidr
import com.msitools.anyconnectmobile.core.IpFamily
import java.io.Closeable
import java.net.InetAddress
import java.util.concurrent.CompletableFuture
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

internal data class DnsRouteSnapshot(
    val routeTtlSeconds: Map<IpCidr, Long>,
    val minimumTtlSeconds: Long,
) {
    val routes: List<IpCidr>
        get() = routeTtlSeconds.keys.toList()
}

internal class DnsRuleRefreshException(
    message: String,
    cause: Throwable? = null,
) : IllegalStateException(message, cause)

@TargetApi(Build.VERSION_CODES.Q)
internal class VpnDnsRuleResolver(
    private val network: Network,
    private val dnsServer: InetAddress,
) : Closeable {
    private val cancelled = AtomicBoolean(false)
    private val queryIds = AtomicInteger((System.nanoTime() and 0xffff).toInt())
    private val activeQueries = ConcurrentHashMap.newKeySet<CancellationSignal>()
    private val resolver = DnsResolver.getInstance()
    @Volatile
    private var executor = Executors.newFixedThreadPool(MAX_PARALLEL_QUERIES)

    fun resolve(
        hosts: List<String>,
        families: Set<IpFamily>,
    ): DnsRouteSnapshot {
        check(!cancelled.get()) { "DNS resolver is cancelled" }
        val queries = hosts.distinct().flatMap { host ->
            buildList {
                if (IpFamily.IPV4 in families) add(DnsQuery(host, DnsRecordType.A))
                if (IpFamily.IPV6 in families) add(DnsQuery(host, DnsRecordType.AAAA))
            }
        }
        if (queries.isEmpty()) {
            return DnsRouteSnapshot(emptyMap(), DEFAULT_EMPTY_REFRESH_SECONDS)
        }

        val futures = queries.map { query ->
            executor.submit<DnsWireResponse> { execute(query) }
        }
        return try {
            val responses = futures.mapIndexed { index, future ->
                future.await(queries[index])
            }
            val failedResponse = responses.firstOrNull {
                it.responseCode != DNS_RCODE_SUCCESS && it.responseCode != DNS_RCODE_NAME_ERROR
            }
            check(failedResponse == null) {
                "VPN DNS returned rcode=${failedResponse?.responseCode}"
            }
            val ttlByRoute = buildMap {
                responses.flatMap(DnsWireResponse::records).forEach { record ->
                    val route = IpCidr.from(record.address)
                    put(route, maxOf(get(route) ?: 0L, record.ttlSeconds))
                }
            }
            DnsRouteSnapshot(
                routeTtlSeconds = ttlByRoute,
                minimumTtlSeconds = responses.minOfOrNull(DnsWireResponse::minimumTtlSeconds)
                    ?: DEFAULT_EMPTY_REFRESH_SECONDS,
            )
        } catch (failure: Throwable) {
            futures.forEach { it.cancel(true) }
            throw DnsRuleRefreshException(
                "VPN DNS ${dnsServer.hostAddress} failed while resolving route mappings",
                failure,
            )
        }
    }

    private fun Future<DnsWireResponse>.await(query: DnsQuery): DnsWireResponse =
        try {
            get(QUERY_DEADLINE_SECONDS, TimeUnit.SECONDS)
        } catch (failure: Throwable) {
            throw DnsRuleRefreshException(
                "VPN DNS query failed for ${query.host} ${query.type.name}",
                failure,
            )
        }

    private fun execute(query: DnsQuery): DnsWireResponse {
        check(!cancelled.get()) { "DNS resolver is cancelled" }
        val id = queryIds.getAndIncrement() and 0xffff
        val request = DnsPacketCodec.buildQuery(id, query.host, query.type)
        val cancellation = CancellationSignal()
        val completion = CompletableFuture<RawDnsAnswer>()
        check(activeQueries.add(cancellation)) { "DNS query was registered twice" }
        try {
            resolver.rawQuery(
                network,
                request,
                DnsResolver.FLAG_EMPTY,
                DIRECT_EXECUTOR,
                cancellation,
                object : DnsResolver.Callback<ByteArray> {
                    override fun onAnswer(answer: ByteArray, rcode: Int) {
                        completion.complete(RawDnsAnswer(answer, rcode))
                    }

                    override fun onError(error: DnsResolver.DnsException) {
                        completion.completeExceptionally(error)
                    }
                },
            )
            val answer = completion.get(QUERY_DEADLINE_SECONDS, TimeUnit.SECONDS)
            return DnsPacketCodec.parseResponse(answer.packet, id, query.type).also { parsed ->
                check(!parsed.truncated) {
                    "Android DNS resolver returned a truncated response for ${query.host}"
                }
                check(parsed.responseCode == answer.responseCode) {
                    "Android DNS resolver rcode ${answer.responseCode} did not match packet " +
                        "rcode ${parsed.responseCode}"
                }
            }
        } finally {
            activeQueries.remove(cancellation)
            if (cancelled.get() || !completion.isDone) cancellation.cancel()
        }
    }

    override fun close() {
        if (!cancelled.compareAndSet(false, true)) return
        activeQueries.toList().forEach(CancellationSignal::cancel)
        executor.shutdownNow()
    }

    private data class RawDnsAnswer(
        val packet: ByteArray,
        val responseCode: Int,
    )

    private data class DnsQuery(
        val host: String,
        val type: DnsRecordType,
    )

    companion object {
        private const val DNS_RCODE_SUCCESS = 0
        private const val DNS_RCODE_NAME_ERROR = 3
        private const val QUERY_DEADLINE_SECONDS = 8L
        private const val MAX_PARALLEL_QUERIES = 8
        private const val DEFAULT_EMPTY_REFRESH_SECONDS = 60L
        private val DIRECT_EXECUTOR = Executor(Runnable::run)
    }
}
