package com.msitools.anyconnectmobile.routeprobe

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.ComponentName
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.FileObserver
import android.os.Handler
import android.os.Looper
import android.os.ResultReceiver
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import com.msitools.anyconnectmobile.routeprobe.service.RouteProbeVpnService
import java.io.File
import java.io.InputStreamReader
import java.lang.ref.WeakReference
import java.nio.charset.CodingErrorAction
import java.util.ArrayDeque
import org.json.JSONObject
import org.json.JSONTokener

internal fun shouldRestoreLatestReport(
    flowActive: Boolean,
    flowStartedAtEpochMs: Long?,
    latestModifiedEpochMs: Long,
    displayedLatestModifiedEpochMs: Long?,
): Boolean {
    if (displayedLatestModifiedEpochMs != null &&
        latestModifiedEpochMs <= displayedLatestModifiedEpochMs
    ) {
        return false
    }
    if (!flowActive) return true
    val startedAt = flowStartedAtEpochMs ?: return false
    return latestModifiedEpochMs >= startedAt
}

class ProbeActivity : Activity() {
    private lateinit var startButton: Button
    private lateinit var copyButton: Button
    private lateinit var reportScroll: ScrollView
    private lateinit var reportView: TextView

    private var permissionFlowActive = false
    private var flowStartedAtEpochMs: Long? = null
    private var reportJson: String? = null
    private var displayedLatestModifiedEpochMs: Long? = null
    private var latestReportObserver: FileObserver? = null
    private var latestReportReadFailed = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(createContentView())

        permissionFlowActive = savedInstanceState?.getBoolean(STATE_PERMISSION_FLOW) ?: false
        flowStartedAtEpochMs = savedInstanceState
            ?.takeIf { it.containsKey(STATE_FLOW_STARTED_AT) }
            ?.getLong(STATE_FLOW_STARTED_AT)
        reportJson = savedInstanceState?.getString(STATE_REPORT_JSON)
        displayedLatestModifiedEpochMs = savedInstanceState
            ?.takeIf { it.containsKey(STATE_DISPLAYED_LATEST_MODIFIED) }
            ?.getLong(STATE_DISPLAYED_LATEST_MODIFIED)
        reportView.text = savedInstanceState?.getCharSequence(STATE_REPORT_TEXT)
            ?: getString(R.string.initial_status)
        updateButtons()
    }

    override fun onResume() {
        super.onResume()
        startLatestReportObserver()
        RESULT_RECEIVER.attach(this)
    }

    override fun onPause() {
        stopLatestReportObserver()
        RESULT_RECEIVER.detach(this)
        super.onPause()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        RESULT_RECEIVER.detach(this)
        outState.putBoolean(STATE_PERMISSION_FLOW, permissionFlowActive)
        flowStartedAtEpochMs?.let { outState.putLong(STATE_FLOW_STARTED_AT, it) }
        outState.putString(STATE_REPORT_JSON, reportJson)
        displayedLatestModifiedEpochMs?.let {
            outState.putLong(STATE_DISPLAYED_LATEST_MODIFIED, it)
        }
        outState.putCharSequence(STATE_REPORT_TEXT, reportView.text)
        super.onSaveInstanceState(outState)
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != NOTIFICATION_PERMISSION_REQUEST || !permissionFlowActive) return

        if (grantResults.singleOrNull() == PackageManager.PERMISSION_GRANTED) {
            warmUpHelperPackage()
            requestVpnPermission()
        } else {
            stopPermissionFlow(getString(R.string.notification_permission_denied))
        }
    }

    @Deprecated("VpnService.prepare uses the request-code activity result contract")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != VPN_PERMISSION_REQUEST || !permissionFlowActive) return

        if (resultCode == RESULT_OK) {
            startProbeService()
        } else {
            stopPermissionFlow(getString(R.string.vpn_permission_denied))
        }
    }

    private fun createContentView(): View {
        val spacing = (resources.displayMetrics.density * CONTENT_SPACING_DP).toInt()
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(spacing, spacing, spacing, spacing)
        }
        root.addView(
            TextView(this).apply {
                text = getString(R.string.probe_title)
                textSize = TITLE_TEXT_SIZE_SP
            },
            matchWidthWrapHeight(),
        )
        root.addView(
            TextView(this).apply {
                text = getString(R.string.probe_warning)
                textSize = BODY_TEXT_SIZE_SP
                setPadding(0, spacing / 2, 0, spacing / 2)
            },
            matchWidthWrapHeight(),
        )
        root.addView(
            TextView(this).apply {
                text = getString(R.string.device_summary, Build.MODEL, Build.VERSION.SDK_INT)
                textSize = BODY_TEXT_SIZE_SP
            },
            matchWidthWrapHeight(),
        )

        startButton = Button(this).apply {
            text = getString(R.string.start_full_probe)
            setOnClickListener { beginPermissionFlow() }
        }
        root.addView(startButton, matchWidthWrapHeight())

        reportView = TextView(this).apply {
            textSize = REPORT_TEXT_SIZE_SP
            setTextIsSelectable(true)
            setPadding(spacing / 2, spacing / 2, spacing / 2, spacing / 2)
        }
        reportScroll = ScrollView(this).apply {
            isFillViewport = true
            addView(reportView, matchWidthWrapHeight())
        }
        root.addView(
            reportScroll,
            LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                0,
                1f,
            ),
        )

        copyButton = Button(this).apply {
            text = getString(R.string.copy_report)
            setOnClickListener { copyReport() }
        }
        root.addView(copyButton, matchWidthWrapHeight())
        root.addView(
            Button(this).apply {
                text = getString(R.string.stop_and_close_vpn)
                setOnClickListener { stopProbeService() }
            },
            matchWidthWrapHeight(),
        )
        return root
    }

    private fun beginPermissionFlow() {
        if (permissionFlowActive) return

        permissionFlowActive = true
        flowStartedAtEpochMs = System.currentTimeMillis()
        reportJson = null
        latestReportReadFailed = false
        RESULT_RECEIVER.clearPending()
        reportView.text = getString(R.string.checking_permissions)
        updateButtons()

        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            appendLine(getString(R.string.requesting_notification_permission))
            requestPermissions(
                arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                NOTIFICATION_PERMISSION_REQUEST,
            )
            return
        }
        warmUpHelperPackage()
        requestVpnPermission()
    }

    private fun warmUpHelperPackage() {
        val intent = Intent().apply {
            component = ComponentName(HELPER_PACKAGE, HELPER_WARMUP_ACTIVITY)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            addFlags(Intent.FLAG_ACTIVITY_NO_ANIMATION)
            addFlags(Intent.FLAG_ACTIVITY_EXCLUDE_FROM_RECENTS)
        }
        try {
            startActivity(intent)
            appendLine(getString(R.string.helper_warmup_requested))
        } catch (error: Exception) {
            appendLine(
                getString(
                    R.string.helper_warmup_failed,
                    error.javaClass.name + ":" + error.message.orEmpty(),
                ),
            )
        }
    }

    private fun requestVpnPermission() {
        val prepareIntent = try {
            VpnService.prepare(this)
        } catch (error: Exception) {
            showOriginalFailure(error)
            return
        }
        if (prepareIntent == null) {
            startProbeService()
            return
        }

        appendLine(getString(R.string.requesting_vpn_permission))
        try {
            @Suppress("DEPRECATION")
            startActivityForResult(prepareIntent, VPN_PERMISSION_REQUEST)
        } catch (error: Exception) {
            showOriginalFailure(error)
        }
    }

    private fun startProbeService() {
        val intent = Intent(this, RouteProbeVpnService::class.java).apply {
            action = ProbeContract.ACTION_RUN
            putExtra(ProbeContract.EXTRA_RECEIVER, RESULT_RECEIVER)
        }
        try {
            startForegroundService(intent)
            appendLine(getString(R.string.service_started))
        } catch (error: Exception) {
            showOriginalFailure(error)
        }
    }

    private fun stopProbeService() {
        val serviceIntent = Intent(this, RouteProbeVpnService::class.java)
        stopService(serviceIntent)
        permissionFlowActive = false
        flowStartedAtEpochMs = null
        RESULT_RECEIVER.clearPending()
        appendLine(getString(R.string.stop_requested))
        updateButtons()
    }

    private fun handleProbeResult(resultCode: Int, resultData: Bundle?) {
        when (resultCode) {
            ProbeContract.RESULT_PROGRESS -> {
                resultData?.getString(ProbeContract.KEY_MESSAGE)?.let(::appendLine)
            }

            ProbeContract.RESULT_COMPLETE -> {
                val completedJson = resultData?.getString(ProbeContract.KEY_REPORT_JSON)
                if (completedJson == null || completedJson != reportJson) {
                    appendLine(getString(R.string.probe_complete))
                    resultData?.getString(ProbeContract.KEY_MESSAGE)?.let(::appendLine)
                    resultData?.getString(ProbeContract.KEY_REPORT_PATH)?.let { path ->
                        appendLine(getString(R.string.report_file_path, path))
                    }
                    if (completedJson == null) {
                        appendLine(getString(R.string.missing_report_json))
                    } else {
                        reportJson = completedJson
                        appendLine(completedJson)
                    }
                }
                permissionFlowActive = false
                flowStartedAtEpochMs = null
                updateButtons()
            }

            ProbeContract.RESULT_FAILED -> {
                appendLine(getString(R.string.probe_failed))
                appendLine(
                    resultData?.getString(ProbeContract.KEY_MESSAGE)
                        ?: getString(R.string.missing_failure_message),
                )
                permissionFlowActive = false
                flowStartedAtEpochMs = null
                updateButtons()
            }
        }
    }

    private fun showOriginalFailure(error: Exception) {
        appendLine(getString(R.string.probe_failed))
        appendLine(error.javaClass.name + ": " + error.message)
        permissionFlowActive = false
        flowStartedAtEpochMs = null
        updateButtons()
    }

    private fun stopPermissionFlow(message: String) {
        appendLine(message)
        permissionFlowActive = false
        flowStartedAtEpochMs = null
        updateButtons()
    }

    private fun startLatestReportObserver() {
        check(latestReportObserver == null) { "Latest report observer is already active" }
        val reportsDirectory = File(filesDir, REPORTS_DIRECTORY)
        try {
            check(
                reportsDirectory.isDirectory ||
                    (!reportsDirectory.exists() && reportsDirectory.mkdirs()),
            ) {
                "Unable to create reports directory: ${reportsDirectory.absolutePath}"
            }
            @Suppress("DEPRECATION")
            val observer = object : FileObserver(
                reportsDirectory.absolutePath,
                LATEST_REPORT_EVENTS,
            ) {
                override fun onEvent(event: Int, path: String?) {
                    if (event and LATEST_REPORT_EVENTS == 0 || path != LATEST_REPORT_FILE) return
                    runOnUiThread {
                        if (latestReportObserver === this && !latestReportReadFailed) {
                            restoreLatestReportIfEligible(reportsDirectory)
                        }
                    }
                }
            }
            latestReportObserver = observer
            observer.startWatching()
            if (!latestReportReadFailed) restoreLatestReportIfEligible(reportsDirectory)
        } catch (error: Exception) {
            stopLatestReportObserver()
            latestReportReadFailed = true
            showOriginalFailure(error)
        }
    }

    private fun stopLatestReportObserver() {
        latestReportObserver?.stopWatching()
        latestReportObserver = null
    }

    private fun restoreLatestReportIfEligible(reportsDirectory: File) {
        val latestReport = File(reportsDirectory, LATEST_REPORT_FILE)
        if (!latestReport.exists()) return
        try {
            check(latestReport.isFile) {
                "Latest report path is not a file: ${latestReport.absolutePath}"
            }
            val latestModifiedEpochMs = latestReport.lastModified()
            if (!shouldRestoreLatestReport(
                    flowActive = permissionFlowActive,
                    flowStartedAtEpochMs = flowStartedAtEpochMs,
                    latestModifiedEpochMs = latestModifiedEpochMs,
                    displayedLatestModifiedEpochMs = displayedLatestModifiedEpochMs,
                )
            ) {
                return
            }

            val completedJson = latestReport.readStrictUtf8()
            check(completedJson.isNotEmpty()) {
                "Latest report is empty: ${latestReport.absolutePath}"
            }
            completedJson.requireCompleteJsonObject()
            if (completedJson == reportJson) {
                displayedLatestModifiedEpochMs = latestModifiedEpochMs
                return
            }

            appendLine(
                getString(
                    if (permissionFlowActive) {
                        R.string.active_report_restored
                    } else {
                        R.string.recent_report_restored
                    },
                ),
            )
            appendLine(getString(R.string.report_file_path, latestReport.absolutePath))
            reportJson = completedJson
            displayedLatestModifiedEpochMs = latestModifiedEpochMs
            appendLine(completedJson)
            permissionFlowActive = false
            flowStartedAtEpochMs = null
            updateButtons()
        } catch (error: Exception) {
            latestReportReadFailed = true
            showOriginalFailure(error)
        }
    }

    private fun updateButtons() {
        startButton.isEnabled = !permissionFlowActive
        copyButton.isEnabled = reportJson != null
    }

    private fun appendLine(line: String) {
        if (reportView.text.isNotEmpty()) reportView.append("\n")
        reportView.append(line)
        reportScroll.post { reportScroll.fullScroll(View.FOCUS_DOWN) }
    }

    private fun copyReport() {
        val completedJson = reportJson ?: return
        val clipboard = getSystemService(ClipboardManager::class.java)
        clipboard.setPrimaryClip(
            ClipData.newPlainText(getString(R.string.report_clip_label), completedJson),
        )
        Toast.makeText(this, R.string.report_copied, Toast.LENGTH_SHORT).show()
    }

    private fun matchWidthWrapHeight(): ViewGroup.LayoutParams = ViewGroup.LayoutParams(
        ViewGroup.LayoutParams.MATCH_PARENT,
        ViewGroup.LayoutParams.WRAP_CONTENT,
    )

    private class MainThreadProbeReceiver : ResultReceiver(Handler(Looper.getMainLooper())) {
        private var activityReference: WeakReference<ProbeActivity>? = null
        private val pending = ArrayDeque<PendingResult>()

        fun attach(activity: ProbeActivity) {
            activityReference = WeakReference(activity)
            while (pending.isNotEmpty()) {
                val result = pending.removeFirst()
                activity.handleProbeResult(result.code, result.data)
            }
        }

        fun detach(activity: ProbeActivity) {
            if (activityReference?.get() === activity) activityReference = null
        }

        fun clearPending() {
            pending.clear()
        }

        override fun onReceiveResult(resultCode: Int, resultData: Bundle?) {
            val activity = activityReference?.get()
            if (activity == null || activity.isFinishing || activity.isDestroyed) {
                pending.addLast(PendingResult(resultCode, resultData?.let(::Bundle)))
            } else {
                activity.handleProbeResult(resultCode, resultData)
            }
        }
    }

    private data class PendingResult(val code: Int, val data: Bundle?)

    private companion object {
        const val NOTIFICATION_PERMISSION_REQUEST = 2_001
        const val VPN_PERMISSION_REQUEST = 2_002
        const val CONTENT_SPACING_DP = 16
        const val TITLE_TEXT_SIZE_SP = 24f
        const val BODY_TEXT_SIZE_SP = 16f
        const val REPORT_TEXT_SIZE_SP = 13f
        const val STATE_PERMISSION_FLOW = "permission_flow"
        const val STATE_FLOW_STARTED_AT = "flow_started_at"
        const val STATE_REPORT_JSON = "report_json"
        const val STATE_DISPLAYED_LATEST_MODIFIED = "displayed_latest_modified"
        const val STATE_REPORT_TEXT = "report_text"
        const val REPORTS_DIRECTORY = "reports"
        const val LATEST_REPORT_FILE = "latest.json"
        const val LATEST_REPORT_EVENTS = FileObserver.CLOSE_WRITE or FileObserver.MOVED_TO
        const val HELPER_PACKAGE = "com.msitools.anyconnectmobile.routeprobesender"
        const val HELPER_WARMUP_ACTIVITY =
            "com.msitools.anyconnectmobile.routeprobesender.HelperWarmupActivity"

        val RESULT_RECEIVER = MainThreadProbeReceiver()
    }
}

private fun File.readStrictUtf8(): String {
    val decoder = Charsets.UTF_8.newDecoder()
        .onMalformedInput(CodingErrorAction.REPORT)
        .onUnmappableCharacter(CodingErrorAction.REPORT)
    return inputStream().use { stream ->
        InputStreamReader(stream, decoder).buffered().use { reader -> reader.readText() }
    }
}

private fun String.requireCompleteJsonObject() {
    val tokener = JSONTokener(this)
    check(tokener.nextValue() is JSONObject) { "Latest report must be a JSON object" }
    check(tokener.nextClean() == '\u0000') { "Latest report contains trailing content" }
}
