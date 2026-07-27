package com.msitools.anyconnectmobile.routeprobe.service

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import java.net.InetAddress

internal class HelperProbeContentProviderSender(
    private val context: Context,
    private val availabilityChecker: HelperProbeAvailabilityChecker =
        AndroidHelperProbeAvailabilityChecker(context),
) : HelperProbeSender {
    override fun send(address: InetAddress, nonce: ByteArray): HelperProbeSendResult {
        val availability = availabilityChecker.check()
        if (!availability.providerResolved) {
            return HelperProbeSendResult.unavailable(availability.diagnostic())
        }
        val extras = Bundle().apply {
            putString(KEY_DESTINATION, requireNotNull(address.hostAddress))
            putByteArray(KEY_NONCE, nonce)
        }
        val result = try {
            context.contentResolver.call(
                Uri.parse("content://$AUTHORITY"),
                METHOD_SEND,
                null,
                extras,
            )
        } catch (error: Exception) {
            return HelperProbeSendResult.unavailable(
                availability.diagnostic() + " " +
                    error.javaClass.name + ":" + error.message.orEmpty(),
            )
        }
        return result.toHelperProbeSendResult()
    }
}

internal fun interface HelperProbeAvailabilityChecker {
    fun check(): HelperProbeAvailability
}

internal data class HelperProbeAvailability(
        val packageInstalled: Boolean,
        val providerResolved: Boolean,
        val broadcastReceiverResolved: Boolean,
        val lookupError: String?,
) {
    fun diagnostic(): String = buildString {
        append("helperPackageInstalled=").append(packageInstalled)
        append(" helperProviderResolved=").append(providerResolved)
        append(" helperBroadcastReceiverResolved=").append(broadcastReceiverResolved)
        lookupError?.let { append(" helperLookupError=").append(it.asSingleToken()) }
    }
}

private class AndroidHelperProbeAvailabilityChecker(
    private val context: Context,
) : HelperProbeAvailabilityChecker {
    override fun check(): HelperProbeAvailability {
        val packageInstalled = runCatching { context.packageManager.hasPackage(HELPER_PACKAGE) }
            .getOrElse {
                return HelperProbeAvailability(
                    packageInstalled = false,
                    providerResolved = false,
                    broadcastReceiverResolved = false,
                    lookupError = it.javaClass.name + ":" + it.message.orEmpty(),
                )
            }
        val providerResolved = runCatching {
            context.packageManager.resolveHelperProvider()
        }.getOrElse {
            return HelperProbeAvailability(
                packageInstalled = packageInstalled,
                providerResolved = false,
                broadcastReceiverResolved = false,
                lookupError = it.javaClass.name + ":" + it.message.orEmpty(),
            )
        }
        val receiverResolved = runCatching {
            context.packageManager.resolveHelperBroadcastReceiver()
        }.getOrElse {
            return HelperProbeAvailability(
                packageInstalled = packageInstalled,
                providerResolved = providerResolved,
                broadcastReceiverResolved = false,
                lookupError = it.javaClass.name + ":" + it.message.orEmpty(),
            )
        }
        return HelperProbeAvailability(
            packageInstalled = packageInstalled,
            providerResolved = providerResolved,
            broadcastReceiverResolved = receiverResolved,
            lookupError = null,
        )
    }
}

private fun PackageManager.hasPackage(packageName: String): Boolean =
    try {
        if (Build.VERSION.SDK_INT >= 33) {
            getPackageInfo(packageName, PackageManager.PackageInfoFlags.of(0))
        } else {
            @Suppress("DEPRECATION")
            getPackageInfo(packageName, 0)
        }
        true
    } catch (_: PackageManager.NameNotFoundException) {
        false
    }

private fun PackageManager.resolveHelperProvider(): Boolean =
    if (Build.VERSION.SDK_INT >= 33) {
        resolveContentProvider(
            AUTHORITY,
            PackageManager.ComponentInfoFlags.of(0),
        ) != null
    } else {
        @Suppress("DEPRECATION")
        resolveContentProvider(AUTHORITY, 0) != null
    }

private fun PackageManager.resolveHelperBroadcastReceiver(): Boolean {
    val intent = Intent(ACTION_SEND).apply {
        component = ComponentName(HELPER_PACKAGE, RECEIVER_CLASS_NAME)
    }
    return if (Build.VERSION.SDK_INT >= 33) {
        queryBroadcastReceivers(
            intent,
            PackageManager.ResolveInfoFlags.of(0),
        ).isNotEmpty()
    } else {
        @Suppress("DEPRECATION")
        queryBroadcastReceivers(intent, 0).isNotEmpty()
    }
}

private fun Bundle?.toHelperProbeSendResult(): HelperProbeSendResult {
    if (this == null) return HelperProbeSendResult.unavailable("helperProviderResult=null")
    return when (getString(KEY_STATUS)) {
        STATUS_SUCCESS -> HelperProbeSendResult.success()
        STATUS_FAILED -> HelperProbeSendResult(
            status = HelperProbeSendStatus.FAILED,
            detail = getString(KEY_ERROR_CLASS).orEmpty() +
                ":" + getString(KEY_ERROR_MESSAGE).orEmpty(),
        )
        null -> HelperProbeSendResult.unavailable("helperProviderResult=missing_status")
        else -> HelperProbeSendResult.unavailable("helperProviderResult=unknown_status")
    }
}

private fun String.asSingleToken(): String =
    replace(Regex("\\s+"), "_")

private const val AUTHORITY = "com.msitools.anyconnectmobile.routeprobesender.probe"
private const val HELPER_PACKAGE = "com.msitools.anyconnectmobile.routeprobesender"
private const val RECEIVER_CLASS_NAME =
    "com.msitools.anyconnectmobile.routeprobesender.ExternalProbeReceiver"
private const val ACTION_SEND =
    "com.msitools.anyconnectmobile.routeprobesender.SEND_UDP_PROBE"
private const val METHOD_SEND = "send_udp_probe"
private const val KEY_DESTINATION = "destination"
private const val KEY_NONCE = "nonce"
private const val KEY_STATUS = "status"
private const val KEY_ERROR_CLASS = "error_class"
private const val KEY_ERROR_MESSAGE = "error_message"
private const val STATUS_SUCCESS = "SUCCESS"
private const val STATUS_FAILED = "FAILED"
