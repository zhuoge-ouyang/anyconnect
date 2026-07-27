package com.msitools.anyconnectmobile.routeprobe.data

import android.content.Context
import com.msitools.anyconnectmobile.routeprobe.BuildConfig
import com.msitools.anyconnectmobile.routeprobe.net.Cidr
import java.security.MessageDigest

data class ProbeIpDb(
    val cidrs: List<Cidr>,
    val sha256: String,
    val rawRecordCount: Int,
) {
    companion object {
        fun load(context: Context): ProbeIpDb {
            val bytes = context.assets.open("china_ip_list.txt").use { it.readBytes() }
            val sha = MessageDigest.getInstance("SHA-256")
                .digest(bytes)
                .joinToString("") { "%02X".format(it.toInt() and 0xff) }
            require(sha == BuildConfig.IPDB_SHA256) {
                "Gate 0 IPDB SHA-256 changed: expected=${BuildConfig.IPDB_SHA256} actual=$sha"
            }
            val lines = bytes.toString(Charsets.UTF_8)
                .lineSequence()
                .map(String::trim)
                .filter { it.isNotEmpty() && !it.startsWith("#") }
                .toList()
            require(lines.size == BuildConfig.IPDB_RECORD_COUNT) {
                "Gate 0 IPDB record count changed: " +
                    "expected=${BuildConfig.IPDB_RECORD_COUNT} actual=${lines.size}"
            }
            val cidrs = lines.map(Cidr::parse)
            return ProbeIpDb(cidrs = cidrs, sha256 = sha, rawRecordCount = lines.size)
        }
    }
}
