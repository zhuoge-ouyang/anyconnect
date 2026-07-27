package com.msitools.anyconnectmobile.routeprobesender

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri
import android.os.Bundle
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetSocketAddress

class ExternalProbeProvider : ContentProvider() {
    override fun onCreate(): Boolean = true

    override fun call(method: String, arg: String?, extras: Bundle?): Bundle {
        if (method != ExternalProbeContract.METHOD_SEND) {
            return errorBundle(IllegalArgumentException("Unsupported method: $method"))
        }
        return SenderRequest.parse(extras).fold(
            onSuccess = { request ->
                runCatching { sendUdpProbe(request) }
                    .fold(
                        onSuccess = { successBundle() },
                        onFailure = { errorBundle(it) },
                    )
            },
            onFailure = { errorBundle(it) },
        )
    }

    override fun query(
        uri: Uri,
        projection: Array<out String>?,
        selection: String?,
        selectionArgs: Array<out String>?,
        sortOrder: String?,
    ): Cursor? = null

    override fun getType(uri: Uri): String? = null

    override fun insert(uri: Uri, values: ContentValues?): Uri? = null

    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = 0

    override fun update(
        uri: Uri,
        values: ContentValues?,
        selection: String?,
        selectionArgs: Array<out String>?,
    ): Int = 0
}

internal fun sendUdpProbe(request: SenderRequest) {
    DatagramSocket().use { socket ->
        socket.connect(InetSocketAddress(request.destination, PROBE_UDP_PORT))
        socket.send(DatagramPacket(request.nonce, request.nonce.size))
    }
}

internal fun successBundle(): Bundle = Bundle().apply {
    putString(ExternalProbeContract.KEY_STATUS, ExternalProbeContract.STATUS_SUCCESS)
}

internal fun errorBundle(error: Throwable): Bundle = Bundle().apply {
    putString(ExternalProbeContract.KEY_STATUS, ExternalProbeContract.STATUS_FAILED)
    putString(ExternalProbeContract.KEY_ERROR_CLASS, error.javaClass.name)
    putString(ExternalProbeContract.KEY_ERROR_MESSAGE, error.message.orEmpty())
}

private const val PROBE_UDP_PORT = 9
