package com.msitools.anyconnectmobile.routeprobesender

import android.os.Bundle
import java.net.InetAddress

internal data class SenderRequest(
    val destination: InetAddress,
    val nonce: ByteArray,
) {
    init {
        require(nonce.size == NONCE_BYTES) { "Probe nonce must be exactly 16 bytes" }
    }

    companion object {
        fun parse(extras: Bundle?): Result<SenderRequest> = parse(
            destination = extras?.getString(ExternalProbeContract.KEY_DESTINATION),
            nonce = extras?.getByteArray(ExternalProbeContract.KEY_NONCE),
        )

        fun parse(destination: String?, nonce: ByteArray?): Result<SenderRequest> =
            runCatching {
                val numericDestination = requireNumericIp(destination)
                SenderRequest(
                    destination = InetAddress.getByName(numericDestination),
                    nonce = requireNotNull(nonce) { "Probe nonce is required" },
                )
            }

        private const val NONCE_BYTES = 16
        private val NUMERIC_IP = Regex("^[0-9A-Fa-f:.]+$")

        private fun requireNumericIp(value: String?): String {
            val destination = requireNotNull(value) { "Probe destination is required" }
            require(destination.contains('.') || destination.contains(':')) {
                "Probe destination must be a numeric IP literal"
            }
            require(NUMERIC_IP.matches(destination)) {
                "Probe destination must be a numeric IP literal"
            }
            return destination
        }
    }
}
