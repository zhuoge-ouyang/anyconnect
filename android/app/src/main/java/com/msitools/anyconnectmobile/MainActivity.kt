package com.msitools.anyconnectmobile

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.app.Dialog
import android.content.BroadcastReceiver
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.text.method.PasswordTransformationMethod
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.Window
import android.widget.AdapterView
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.Spinner
import android.widget.TextView
import android.widget.Toast
import com.msitools.anyconnectmobile.core.ClientPreferences
import com.msitools.anyconnectmobile.core.BuiltInWhitelist
import com.msitools.anyconnectmobile.core.ConnectionPageController
import com.msitools.anyconnectmobile.core.ConnectionMode
import com.msitools.anyconnectmobile.core.SmartCandidatePlanner
import com.msitools.anyconnectmobile.core.SmartEndpointProbe
import com.msitools.anyconnectmobile.core.SmartSelectionPolicy
import com.msitools.anyconnectmobile.core.SmartSelectionTransaction
import com.msitools.anyconnectmobile.core.VpnSite
import com.msitools.anyconnectmobile.core.VpnSiteCatalog
import com.msitools.anyconnectmobile.core.WhitelistRuleExpander
import com.msitools.anyconnectmobile.core.contactAuthorPresentation
import com.msitools.anyconnectmobile.service.AnyConnectVpnService
import com.msitools.anyconnectmobile.service.OpenConnectInfo
import com.msitools.anyconnectmobile.service.VpnStatusHub
import com.msitools.anyconnectmobile.service.VpnStatusUpdate
import kotlin.concurrent.thread
import java.net.InetSocketAddress
import java.net.URI
import java.net.Socket
import java.util.concurrent.Executors

class MainActivity : Activity() {
    private val sites = VpnSiteCatalog.defaultSites()
    private lateinit var prefs: ClientPreferences
    private lateinit var siteSpinner: Spinner
    private lateinit var modeSpinner: Spinner
    private lateinit var usernameInput: EditText
    private lateinit var passwordInput: EditText
    private lateinit var statusView: TextView
    private lateinit var loginProgress: ProgressBar
    private lateinit var progressLabel: TextView
    private lateinit var formCardView: View
    private lateinit var connectedCardView: View
    private lateinit var loginContactView: View
    private lateinit var mainContactButton: View
    private lateinit var connectedNodeView: TextView
    private lateinit var connectedModeView: TextView
    private lateinit var downloadSpeedView: TextView
    private lateinit var uploadSpeedView: TextView
    private lateinit var smartSelectButton: Button
    private lateinit var restoreNormalButton: Button
    private lateinit var rulesTitleView: TextView
    private lateinit var rulesSummaryView: TextView
    private var domesticDirectRules: String = ""
    private var foreignDirectRules: String = ""
    private var currentNodeName: String = ""
    private var currentMode: ConnectionMode = ConnectionMode.DOMESTIC_DIRECT
    private var isConnectedUi: Boolean = false
    private var isRestoringSettings: Boolean = false
    private var smartRun: SmartRun? = null
    private var smartDecisionDialog: Dialog? = null
    private val connectionPageController = ConnectionPageController()
    private val uiHandler = Handler(Looper.getMainLooper())
    private val statusPoller = object : Runnable {
        override fun run() {
            refreshStatusFromStorage()
            uiHandler.postDelayed(this, 1000)
        }
    }
    private val statusHubListener: (VpnStatusUpdate) -> Unit = ::handleStatusUpdate
    private val vpnStatusReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action != AnyConnectVpnService.ACTION_STATUS) return
            handleStatusUpdate(
                VpnStatusUpdate(
                    state = intent.getStringExtra(AnyConnectVpnService.EXTRA_STATE).orEmpty(),
                    message = intent.getStringExtra(AnyConnectVpnService.EXTRA_MESSAGE).orEmpty(),
                    nodeName = intent.getStringExtra(AnyConnectVpnService.EXTRA_SITE_NAME).orEmpty(),
                    modeName = intent.getStringExtra(AnyConnectVpnService.EXTRA_MODE).orEmpty(),
                    rxBps = intent.getLongExtra(AnyConnectVpnService.EXTRA_RX_BPS, 0L),
                    txBps = intent.getLongExtra(AnyConnectVpnService.EXTRA_TX_BPS, 0L),
                ),
            )
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = ClientPreferences(this)
        setContentView(createView())
        restoreSavedSettings()
        recoverIncompleteSmartSelection()
    }

    override fun onStart() {
        super.onStart()
        VpnStatusHub.addListener(statusHubListener)
        if (Build.VERSION.SDK_INT >= 33) {
            registerReceiver(
                vpnStatusReceiver,
                IntentFilter(AnyConnectVpnService.ACTION_STATUS),
                RECEIVER_NOT_EXPORTED,
            )
        } else {
            @Suppress("DEPRECATION", "UnspecifiedRegisterReceiverFlag")
            registerReceiver(vpnStatusReceiver, IntentFilter(AnyConnectVpnService.ACTION_STATUS))
        }
        uiHandler.post(statusPoller)
    }

    override fun onStop() {
        uiHandler.removeCallbacks(statusPoller)
        VpnStatusHub.removeListener(statusHubListener)
        unregisterReceiver(vpnStatusReceiver)
        super.onStop()
    }

    @Deprecated("VpnService.prepare uses request-code activity result on this minSdk")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST_VPN && resultCode == RESULT_OK) {
            setProgressStep(62, "VPN 已授权，正在打开森林隧道…")
            startVpnService()
        } else if (requestCode == REQUEST_VPN) {
            showIdleProgress()
            statusView.text = getString(R.string.vpn_permission_denied)
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQUEST_NOTIFICATIONS) {
            requestPermissionsThenVpn()
        }
    }

    private fun createView(): View {
        val density = resources.displayMetrics.density
        val root = FrameLayout(this).apply {
            setBackgroundColor(0xFFFFF7E6.toInt())
        }
        root.addView(ImageView(this).apply {
            setImageResource(R.drawable.splash_healing)
            scaleType = ImageView.ScaleType.CENTER_CROP
            alpha = 0.92f
        }, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT,
        ))

        val scroll = ScrollView(this).apply {
            isFillViewport = true
        }
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(18), dp(26), dp(18), dp(104))
        }
        scroll.addView(content)
        root.addView(scroll)

        content.addView(heroCard(), matchWidth())
        formCardView = formCard()
        connectedCardView = connectedCard().apply { visibility = View.GONE }
        content.addView(formCardView, marginTop(18))
        content.addView(connectedCardView, marginTop(18))
        content.addView(rulesCard(), marginTop(14))
        content.addView(statusCard(), marginTop(14))
        loginContactView = loginContactCard()
        content.addView(loginContactView, marginTop(14))

        mainContactButton = Button(this).apply {
            text = getString(R.string.contact_link)
            textSize = 14f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF4F9F61.toInt(), dp(18).toFloat(), 0x00000000)
            elevation = dp(10).toFloat()
            visibility = View.GONE
            setOnClickListener { showContactDialog() }
        }
        root.addView(mainContactButton, FrameLayout.LayoutParams(dp(132), dp(52)).apply {
            gravity = Gravity.END or Gravity.BOTTOM
            marginEnd = dp(18)
            bottomMargin = dp(18)
        })

        return root
    }

    private fun heroCard(): View {
        val row = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(18), dp(16), dp(18), dp(16))
            background = rounded(0xF5FFF8DF.toInt(), dp(26).toFloat(), 0xAAFFFFFF.toInt())
            elevation = dp(6).toFloat()
        }
        row.addView(ImageView(this).apply {
            setImageResource(R.drawable.brand_forest_guardian)
            scaleType = ImageView.ScaleType.CENTER_CROP
            background = rounded(0xCCFFFFFF.toInt(), dp(22).toFloat(), 0x88D6E8BE.toInt())
            clipToOutline = false
        }, LinearLayout.LayoutParams(dp(78), dp(78)))

        val copy = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(14), 0, 0, 0)
        }
        copy.addView(TextView(this).apply {
            text = getString(R.string.title)
            textSize = 25f
            typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
            setTextColor(0xFF365C34.toInt())
        })
        copy.addView(TextView(this).apply {
            text = getString(R.string.subtitle)
            textSize = 14f
            setTextColor(0xFF6F8059.toInt())
            setPadding(0, dp(5), 0, 0)
        })
        row.addView(copy, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        return row
    }

    private fun formCard(): View {
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(18), dp(18), dp(18), dp(18))
            background = rounded(0xF2FFFDF4.toInt(), dp(28).toFloat(), 0xCCFFFFFF.toInt())
            elevation = dp(8).toFloat()
        }
        card.addView(label(getString(R.string.site_hint)))
        siteSpinner = Spinner(this).apply {
            adapter = ArrayAdapter(
                this@MainActivity,
                android.R.layout.simple_spinner_dropdown_item,
                sites.map { "☁  ${it.name}" },
            )
            setSelection(sites.indexOfFirst { it.name == VpnSiteCatalog.defaultSite().name }.coerceAtLeast(0))
            background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
            setPadding(dp(12), 0, dp(12), 0)
        }
        card.addView(siteSpinner, fieldLayout())
        card.addView(label("连接模式"))
        modeSpinner = Spinner(this).apply {
            adapter = ArrayAdapter(
                this@MainActivity,
                android.R.layout.simple_spinner_dropdown_item,
                ConnectionMode.entriesForUi.map { it.label },
            )
            setSelection(0)
            background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
            setPadding(dp(12), 0, dp(12), 0)
            onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) {
                    currentMode = selectedMode()
                    refreshRulesCardForMode(currentMode)
                }

                override fun onNothingSelected(parent: AdapterView<*>?) = Unit
            }
        }
        card.addView(modeSpinner, fieldLayout())
        usernameInput = EditText(this).apply {
            hint = getString(R.string.username_hint)
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT
            setFieldStyle()
        }
        card.addView(usernameInput, fieldLayout())
        passwordInput = EditText(this).apply {
            hint = getString(R.string.password_hint)
            setSingleLine(true)
            setFieldStyle()
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
            transformationMethod = PasswordTransformationMethod.getInstance()
        }
        card.addView(passwordInput, fieldLayout())

        loginProgress = ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal).apply {
            max = 100
            progress = 0
            progressTintList = ColorStateList.valueOf(0xFF73B56E.toInt())
            progressBackgroundTintList = ColorStateList.valueOf(0xFFE9F0D8.toInt())
            visibility = View.GONE
        }
        card.addView(loginProgress, LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            dp(10),
        ).apply {
            topMargin = dp(12)
        })
        progressLabel = TextView(this).apply {
            textSize = 13f
            setTextColor(0xFF6B7F55.toInt())
            visibility = View.GONE
            setPadding(0, dp(8), 0, 0)
        }
        card.addView(progressLabel, matchWidth())

        val buttons = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(0, dp(16), 0, 0)
        }
        buttons.addView(Button(this).apply {
            text = getString(R.string.connect)
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF5FAF6C.toInt(), dp(20).toFloat(), 0x00000000)
            setOnClickListener { beginConnect() }
        }, LinearLayout.LayoutParams(0, dp(52), 1f))
        buttons.addView(Button(this).apply {
            text = getString(R.string.disconnect)
            setTextColor(0xFF4C6A3E.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(20).toFloat(), 0xFFE2C579.toInt())
            setOnClickListener { stopVpnService() }
        }, LinearLayout.LayoutParams(0, dp(52), 1f).apply {
            leftMargin = dp(10)
        })
        card.addView(buttons, matchWidth())
        return card
    }

    private fun connectedCard(): View {
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(18), dp(18), dp(18), dp(18))
            background = rounded(0xF2E9FFE8.toInt(), dp(30).toFloat(), 0xDDFFFFFF.toInt())
            elevation = dp(8).toFloat()
        }
        card.addView(TextView(this).apply {
            text = "森林隧道已连通"
            textSize = 24f
            typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
            setTextColor(0xFF315F3B.toInt())
        }, matchWidth())
        card.addView(TextView(this).apply {
            text = "小精灵正在守护当前网络小路"
            textSize = 14f
            setTextColor(0xFF6D8059.toInt())
            setPadding(0, dp(4), 0, dp(12))
        }, matchWidth())

        val speedRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
        }
        downloadSpeedView = metricCard("当前下载", "0 B/s")
        uploadSpeedView = metricCard("当前上传", "0 B/s")
        speedRow.addView(downloadSpeedView, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        speedRow.addView(uploadSpeedView, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f).apply {
            leftMargin = dp(10)
        })
        card.addView(speedRow, matchWidth())

        connectedNodeView = infoLine("当前节点", "未连接")
        connectedModeView = infoLine("当前模式", ConnectionMode.DOMESTIC_DIRECT.label)
        card.addView(connectedNodeView, marginTop(12))
        card.addView(connectedModeView, marginTop(8))

        val firstRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(0, dp(16), 0, 0)
        }
        firstRow.addView(Button(this).apply {
            text = "断开连接"
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF4F9C62.toInt(), dp(20).toFloat(), 0x00000000)
            setOnClickListener { stopVpnService() }
        }, LinearLayout.LayoutParams(0, dp(52), 1f))
        firstRow.addView(Button(this).apply {
            text = "切换节点"
            setTextColor(0xFF4C6A3E.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(20).toFloat(), 0xFFE2C579.toInt())
            setOnClickListener { openSiteSelection() }
        }, LinearLayout.LayoutParams(0, dp(52), 1f).apply {
            leftMargin = dp(10)
        })
        card.addView(firstRow, matchWidth())

        card.addView(Button(this).apply {
            text = "切换模式"
            setTextColor(0xFF315F3B.toInt())
            background = rounded(0xFFE3F4D8.toInt(), dp(20).toFloat(), 0xFFA8D39A.toInt())
            setOnClickListener { toggleModeAndReconnect() }
        }, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(52)).apply {
            topMargin = dp(10)
        })
        smartSelectButton = Button(this).apply {
            text = "ChatGPT/Codex 智能选线"
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF238EA8.toInt(), dp(20).toFloat(), 0x00000000)
            setOnClickListener { onSmartSelectionPressed() }
        }
        card.addView(smartSelectButton, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(52)).apply {
            topMargin = dp(10)
        })
        restoreNormalButton = Button(this).apply {
            text = "恢复常用线路"
            setTextColor(0xFF315F3B.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(20).toFloat(), 0xFFE2C579.toInt())
            visibility = View.GONE
            setOnClickListener { restoreNormalLine() }
        }
        card.addView(restoreNormalButton, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(52)).apply {
            topMargin = dp(10)
        })
        return card
    }

    private fun rulesCard(): View {
        val openRules = View.OnClickListener { showRulesDialog(currentMode) }
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(14), dp(16), dp(14))
            background = rounded(0xECFFFDF8.toInt(), dp(22).toFloat(), 0xAAFFFFFF.toInt())
            isClickable = true
            isFocusable = true
            contentDescription = "查看完整白名单"
            setOnClickListener(openRules)
        }
        rulesTitleView = label(ConnectionMode.DOMESTIC_DIRECT.ruleTitle).apply {
            isClickable = true
            setOnClickListener(openRules)
        }
        card.addView(rulesTitleView, matchWidth())
        rulesSummaryView = TextView(this).apply {
            textSize = 14f
            setTextColor(0xFF586C45.toInt())
            setPadding(dp(14), dp(12), dp(14), dp(12))
            background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
            text = "还没有添加白名单。点击下方按钮添加。"
            isClickable = true
            isFocusable = true
            contentDescription = "查看完整白名单"
            setOnClickListener(openRules)
        }
        card.addView(rulesSummaryView, LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT,
        ).apply {
            topMargin = dp(8)
        })
        card.addView(Button(this).apply {
            text = "查看 / 添加白名单"
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF6AAE72.toInt(), dp(18).toFloat(), 0x00000000)
            setOnClickListener(openRules)
        }, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(48)).apply {
            topMargin = dp(10)
        })
        return card
    }

    private fun statusCard(): View {
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(14), dp(16), dp(14))
            background = rounded(0xEFFFFDF8.toInt(), dp(22).toFloat(), 0xAAFFFFFF.toInt())
        }
        statusView = TextView(this).apply {
            text = getString(R.string.initial_status) + "\n" + OpenConnectInfo.versionSummary()
            textSize = 13f
            setTextColor(0xFF52604A.toInt())
            setTextIsSelectable(true)
        }
        card.addView(statusView, matchWidth())
        return card
    }

    private fun loginContactCard(): View {
        val qrSize = minOf(resources.displayMetrics.widthPixels - dp(96), dp(300))
        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(dp(18), dp(18), dp(18), dp(20))
            background = rounded(0xEFFFFDF2.toInt(), dp(24).toFloat(), 0xAAFFFFFF.toInt())
            elevation = dp(4).toFloat()
            addView(TextView(this@MainActivity).apply {
                text = getString(R.string.contact_link)
                textSize = 19f
                gravity = Gravity.CENTER
                typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
                setTextColor(0xFF315F3B.toInt())
            }, matchWidth())
            addView(TextView(this@MainActivity).apply {
                text = getString(R.string.contact_scan_hint)
                textSize = 14f
                gravity = Gravity.CENTER
                setTextColor(0xFF6B7F55.toInt())
                setPadding(0, dp(6), 0, dp(14))
            }, matchWidth())
            addView(ImageView(this@MainActivity).apply {
                setImageResource(R.drawable.wechat_contact_qr)
                scaleType = ImageView.ScaleType.CENTER_INSIDE
                adjustViewBounds = true
                background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
                setPadding(dp(8), dp(8), dp(8), dp(8))
            }, LinearLayout.LayoutParams(qrSize, qrSize))
            addView(TextView(this@MainActivity).apply {
                text = getString(R.string.contact_value)
                textSize = 16f
                gravity = Gravity.CENTER
                typeface = Typeface.MONOSPACE
                setTextColor(0xFF4F6D42.toInt())
                setPadding(0, dp(14), 0, 0)
            }, matchWidth())
        }
    }

    private fun beginConnect() {
        if (smartRun != null) {
            Toast.makeText(this, "智能选线进行中，请先取消", Toast.LENGTH_SHORT).show()
            return
        }
        if (usernameInput.text.isNullOrBlank()) {
            usernameInput.error = "请先输入用户名"
            return
        }
        if (passwordInput.text.isNullOrEmpty()) {
            passwordInput.error = "请先输入密码"
            return
        }
        connectionPageController.finishSiteSelection()
        setProgressStep(18, "正在检查通知与 VPN 权限…")
        requestPermissionsThenVpn()
    }

    private fun requestPermissionsThenVpn() {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            setProgressStep(28, "等待通知权限确认…")
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFICATIONS)
            return
        }
        val intent = VpnService.prepare(this)
        if (intent != null) {
            setProgressStep(42, "等待 Android VPN 授权…")
            startActivityForResult(intent, REQUEST_VPN)
        } else {
            setProgressStep(55, "VPN 已授权，正在准备节点…")
            startVpnService()
        }
    }

    private fun startVpnService() {
        val site = sites[siteSpinner.selectedItemPosition]
        currentNodeName = site.name
        currentMode = selectedMode()
        persistSettings()
        connectSite(site, currentMode, "已发送连接请求，正在认证：${site.name}")
    }

    private fun connectSite(site: VpnSite, mode: ConnectionMode, progress: String) {
        val intent = Intent(this, AnyConnectVpnService::class.java).apply {
            action = AnyConnectVpnService.ACTION_CONNECT
            putExtra(AnyConnectVpnService.EXTRA_SERVER, site.server)
            putExtra(AnyConnectVpnService.EXTRA_SITE_NAME, site.name)
            putExtra(AnyConnectVpnService.EXTRA_USERNAME, usernameInput.text.toString())
            putExtra(AnyConnectVpnService.EXTRA_PASSWORD, passwordInput.text.toString())
            putExtra(AnyConnectVpnService.EXTRA_MODE, mode.name)
            putExtra(
                AnyConnectVpnService.EXTRA_RULES,
                BuiltInWhitelist.effectiveRules(mode, rulesFor(mode)),
            )
        }
        if (Build.VERSION.SDK_INT >= 26) {
            startForegroundService(intent)
        } else {
            startService(intent)
        }
        setProgressStep(78, progress)
        statusView.text = "正在连接：${site.name}\n${OpenConnectInfo.versionSummary()}"
    }

    private fun onSmartSelectionPressed() {
        if (smartRun != null) {
            restoreSmartOriginal("用户取消智能选线")
            return
        }
        if (!isConnectedUi || !isVpnActive()) {
            Toast.makeText(this, "请先连接 VPN，再开始智能选线", Toast.LENGTH_SHORT).show()
            return
        }
        AlertDialog.Builder(this)
            .setTitle("开始智能选线")
            .setMessage("诊断最多 60 秒。检测期间 ChatGPT 和国外连接可能短暂中断，国内直连应用通常不受影响。")
            .setNegativeButton("取消", null)
            .setPositiveButton("开始") { _, _ -> beginSmartSelection() }
            .show()
    }

    private fun beginSmartSelection() {
        val settings = prefs.load()
        val original = sites.firstOrNull { it.name == currentNodeName }
            ?: sites.firstOrNull { it.name == settings.siteName }
            ?: return
        val run = SmartRun(
            originalSite = original,
            originalMode = currentMode,
            deadlineAtMs = System.currentTimeMillis() + SmartSelectionPolicy.DEADLINE_MS,
        )
        prefs.saveSmartTransaction(
            SmartSelectionTransaction(original, currentMode, System.currentTimeMillis()),
        )
        smartRun = run
        updateSmartControls(true)
        statusView.text = "智能选线：复测当前线路 0/${SmartSelectionPolicy.CURRENT_ROUNDS}"
        uiHandler.postDelayed({
            if (smartRun === run && run.phase !in setOf(SmartPhase.RECOMMENDATION, SmartPhase.RESTORING)) {
                if (run.phase == SmartPhase.CURRENT_DECISION) {
                    finishSmartKeepOriginal("已到 60 秒，当前线路健康并保持不变")
                } else {
                    restoreSmartOriginal("检测达到 60 秒，正在恢复原线路")
                }
            }
        }, SmartSelectionPolicy.DEADLINE_MS)
        probeSite(run, original, SmartSelectionPolicy.CURRENT_ROUNDS) { metrics ->
            run.currentMetrics = metrics
            if (SmartSelectionPolicy.currentHealthy(metrics)) {
                run.phase = SmartPhase.CURRENT_DECISION
                showCurrentHealthyDecision(run, metrics)
            } else {
                startDeepSmartScan(run)
            }
        }
    }

    private fun probeSite(
        run: SmartRun,
        site: VpnSite,
        rounds: Int,
        done: (SmartSelectionPolicy.Metrics) -> Unit,
    ) {
        run.phase = SmartPhase.PROBING
        thread(name = "smart-probe-${site.name}") {
            val probe = SmartEndpointProbe(::isVpnActive)
            val results = mutableListOf<SmartSelectionPolicy.Round>()
            repeat(rounds) { index ->
                if (smartRun !== run || System.currentTimeMillis() >= run.deadlineAtMs) return@thread
                results += probe.runRound(run.deadlineAtMs)
                uiHandler.post {
                    if (smartRun === run) {
                        statusView.text = "智能选线：${site.name} ${index + 1}/$rounds"
                    }
                }
            }
            val metrics = SmartSelectionPolicy.evaluate(results)
            uiHandler.post { if (smartRun === run) done(metrics) }
        }
    }

    private fun showCurrentHealthyDecision(run: SmartRun, metrics: SmartSelectionPolicy.Metrics) {
        smartDecisionDialog = AlertDialog.Builder(this)
            .setTitle("当前线路健康")
            .setMessage(
                "ChatGPT/OpenAI：${metrics.successes}/${metrics.attempts}\n" +
                    "中位耗时：${metrics.medianMs} ms，最慢：${metrics.slowestMs} ms\n\n" +
                    "是否继续深度检测其他线路？",
            )
            .setCancelable(false)
            .setNegativeButton("保持当前线路") { _, _ ->
                if (smartRun === run) finishSmartKeepOriginal("当前线路健康，已保持")
            }
            .setPositiveButton("继续深度检测") { _, _ ->
                if (smartRun === run) startDeepSmartScan(run)
            }
            .show()
    }

    private fun startDeepSmartScan(run: SmartRun) {
        if (System.currentTimeMillis() >= run.deadlineAtMs) {
            restoreSmartOriginal("检测达到 60 秒，正在恢复原线路")
            return
        }
        val history = prefs.loadSmartHistory()
        val configured = VpnSiteCatalog.smartCandidates()
        if (history.isEmpty()) {
            statusView.text = "智能选线：正在预筛选候选节点"
            thread(name = "smart-candidate-prefilter") {
                val ordered = prefilterSmartCandidates(configured, run.deadlineAtMs)
                uiHandler.post {
                    if (smartRun === run) prepareSmartCandidates(run, ordered, history)
                }
            }
        } else {
            prepareSmartCandidates(run, configured, history)
        }
    }

    private fun prepareSmartCandidates(
        run: SmartRun,
        configured: List<VpnSite>,
        history: Map<String, com.msitools.anyconnectmobile.core.SmartHealthRecord>,
    ) {
        run.candidates = SmartCandidatePlanner.rank(
            sites = configured,
            current = run.originalSite,
            history = history,
            nowMs = System.currentTimeMillis(),
        )
        run.candidateIndex = -1
        tryNextSmartCandidate(run)
    }

    private fun prefilterSmartCandidates(candidates: List<VpnSite>, deadlineAtMs: Long): List<VpnSite> {
        data class Reachability(val site: VpnSite, val reachable: Boolean, val latencyMs: Long, val index: Int)
        val pool = Executors.newFixedThreadPool(candidates.size.coerceAtLeast(1))
        return try {
            candidates.mapIndexed { index, site ->
                pool.submit<Reachability> {
                    val started = System.currentTimeMillis()
                    val reachable = runCatching {
                        val uri = URI(site.server)
                        val timeout = (deadlineAtMs - System.currentTimeMillis()).coerceIn(1L, 2_000L).toInt()
                        Socket().use { it.connect(InetSocketAddress(uri.host, if (uri.port > 0) uri.port else 443), timeout) }
                    }.isSuccess
                    Reachability(site, reachable, System.currentTimeMillis() - started, index)
                }
            }.map { it.get() }
                .sortedWith(
                    compareByDescending<Reachability> { it.reachable }
                        .thenBy { if (it.reachable) it.latencyMs else Long.MAX_VALUE }
                        .thenBy { it.index },
                )
                .map { it.site }
        } finally {
            pool.shutdownNow()
        }
    }

    private fun tryNextSmartCandidate(run: SmartRun) {
        if (smartRun !== run) return
        run.candidateIndex++
        val candidate = run.candidates.getOrNull(run.candidateIndex)
        if (candidate == null || System.currentTimeMillis() >= run.deadlineAtMs) {
            restoreSmartOriginal("没有可信推荐，正在恢复原线路")
            return
        }
        run.activeCandidate = candidate
        run.probeStarted = false
        run.phase = SmartPhase.CONNECTING_CANDIDATE
        statusView.text = "智能选线：连接候选 ${run.candidateIndex + 1}/${run.candidates.size}：${candidate.name}"
        connectSite(candidate, run.originalMode, "智能选线：正在连接 ${candidate.name}")
    }

    private fun handleSmartStatus(update: VpnStatusUpdate) {
        val run = smartRun ?: return
        when (run.phase) {
            SmartPhase.CONNECTING_CANDIDATE -> {
                val candidate = run.activeCandidate ?: return
                if (update.state in setOf(AnyConnectVpnService.STATE_CONNECTED, AnyConnectVpnService.STATE_STATS) &&
                    update.nodeName == candidate.name && !run.probeStarted
                ) {
                    run.probeStarted = true
                    probeSite(run, candidate, SmartSelectionPolicy.CANDIDATE_ROUNDS) { metrics ->
                        val changedExit = run.currentMetrics?.exitIp.isNullOrBlank() ||
                            metrics.exitIp != run.currentMetrics?.exitIp
                        val qualified = SmartSelectionPolicy.candidateQualified(metrics) && changedExit
                        prefs.recordSmartResult(candidate.name, metrics, qualified)
                        if (!qualified) {
                            tryNextSmartCandidate(run)
                        } else if (run.currentMetrics?.let { SmartSelectionPolicy.currentHealthy(it) } == true &&
                            !SmartSelectionPolicy.materiallyBetter(run.currentMetrics!!, metrics)
                        ) {
                            restoreSmartOriginal("候选线路提升不足，正在恢复原线路")
                        } else {
                            run.recommendedMetrics = metrics
                            showSmartRecommendation(run, candidate, metrics)
                        }
                    }
                } else if (update.state == AnyConnectVpnService.STATE_FAILED && update.nodeName == candidate.name) {
                    prefs.recordSmartResult(candidate.name, SmartSelectionPolicy.evaluate(emptyList()), false)
                    tryNextSmartCandidate(run)
                }
            }
            SmartPhase.RESTORING -> {
                if (update.state in setOf(AnyConnectVpnService.STATE_CONNECTED, AnyConnectVpnService.STATE_STATS) &&
                    update.nodeName == run.originalSite.name
                ) {
                    prefs.clearSmartTransaction()
                    finishSmartRun(run.restoreMessage.ifBlank { "已恢复原线路" })
                } else if (update.state == AnyConnectVpnService.STATE_FAILED && update.nodeName == run.originalSite.name) {
                    finishSmartRun("恢复原线路失败；恢复点已保留，下次打开会继续恢复", clearTransaction = false)
                }
            }
            else -> Unit
        }
    }

    private fun showSmartRecommendation(
        run: SmartRun,
        candidate: VpnSite,
        metrics: SmartSelectionPolicy.Metrics,
    ) {
        run.phase = SmartPhase.RECOMMENDATION
        val dialog = Dialog(this)
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE)
        dialog.setCancelable(false)
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(22), dp(22), dp(22), dp(18))
            background = rounded(0xFFFFFDF2.toInt(), dp(28).toFloat(), 0xFFFFFFFF.toInt())
        }
        card.addView(TextView(this).apply {
            text = "推荐：${candidate.name}"
            textSize = 22f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF315F3B.toInt())
        }, matchWidth())
        card.addView(TextView(this).apply {
            text = "ChatGPT/OpenAI：${metrics.successes}/${metrics.attempts}\n" +
                "中位：${metrics.medianMs} ms　最慢：${metrics.slowestMs} ms\n" +
                "出口：${metrics.exitIp} / ${metrics.exitRegion}"
            textSize = 15f
            setTextColor(0xFF4D6940.toInt())
            setPadding(0, dp(12), 0, dp(12))
        }, matchWidth())
        val countdown = TextView(this).apply {
            textSize = 14f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFFB88430.toInt())
        }
        card.addView(countdown, matchWidth())
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        row.addView(Button(this).apply {
            text = "立即采用"
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF238EA8.toInt(), dp(18).toFloat(), 0x00000000)
            setOnClickListener { acceptSmartRecommendation(run, candidate) }
        }, LinearLayout.LayoutParams(0, dp(50), 1f))
        row.addView(Button(this).apply {
            text = "恢复原线路"
            setTextColor(0xFF4C6A3E.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(18).toFloat(), 0xFFE2C579.toInt())
            setOnClickListener { restoreSmartOriginal("用户选择恢复原线路") }
        }, LinearLayout.LayoutParams(0, dp(50), 1f).apply { leftMargin = dp(10) })
        card.addView(row, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
            topMargin = dp(14)
        })
        dialog.setContentView(card)
        dialog.window?.setBackgroundDrawable(ColorDrawable(0x00000000))
        dialog.show()
        dialog.window?.setLayout((resources.displayMetrics.widthPixels * 0.9f).toInt(), ViewGroup.LayoutParams.WRAP_CONTENT)
        smartDecisionDialog = dialog
        val started = System.currentTimeMillis()
        val tick = object : Runnable {
            override fun run() {
                if (smartRun !== run || run.phase != SmartPhase.RECOMMENDATION) return
                val remaining = (SmartSelectionPolicy.DECISION_COUNTDOWN_MS -
                    (System.currentTimeMillis() - started)).coerceAtLeast(0L)
                countdown.text = "${(remaining + 999L) / 1000L} 秒后自动采用推荐线路"
                if (remaining == 0L) acceptSmartRecommendation(run, candidate) else uiHandler.postDelayed(this, 1_000L)
            }
        }
        tick.run()
    }

    private fun acceptSmartRecommendation(run: SmartRun, candidate: VpnSite) {
        if (smartRun !== run) return
        prefs.acceptSmartSelection(run.originalSite, candidate)
        prefs.markSmartSelected(candidate.name)
        prefs.clearSmartTransaction()
        sites.indexOfFirst { it.name == candidate.name }.takeIf { it >= 0 }?.let(siteSpinner::setSelection)
        currentNodeName = candidate.name
        finishSmartRun("已采用推荐线路：${candidate.name}")
    }

    private fun restoreSmartOriginal(message: String) {
        val run = smartRun ?: return
        smartDecisionDialog?.dismiss()
        run.phase = SmartPhase.RESTORING
        run.restoreMessage = message
        statusView.text = "$message：${run.originalSite.name}"
        connectSite(run.originalSite, run.originalMode, "正在恢复原线路：${run.originalSite.name}")
    }

    private fun finishSmartKeepOriginal(message: String) {
        prefs.clearSmartTransaction()
        finishSmartRun(message)
    }

    private fun finishSmartRun(message: String, clearTransaction: Boolean = true) {
        if (clearTransaction) prefs.clearSmartTransaction()
        smartDecisionDialog?.dismiss()
        smartDecisionDialog = null
        smartRun = null
        updateSmartControls(false)
        statusView.text = "$message\n${OpenConnectInfo.versionSummary()}"
        Toast.makeText(this, message, Toast.LENGTH_LONG).show()
    }

    private fun updateSmartControls(running: Boolean) {
        if (::smartSelectButton.isInitialized) {
            smartSelectButton.text = if (running) "取消智能选线" else "ChatGPT/Codex 智能选线"
        }
        if (::restoreNormalButton.isInitialized) {
            restoreNormalButton.isEnabled = !running
            restoreNormalButton.visibility = if (!running && prefs.load().smartModeActive) View.VISIBLE else View.GONE
        }
    }

    private fun restoreNormalLine() {
        if (smartRun != null) return
        val settings = prefs.load()
        val normal = VpnSite(settings.normalSiteName, settings.normalSiteServer)
        if (!settings.smartModeActive || normal.name.isBlank() || normal.server.isBlank()) return
        AlertDialog.Builder(this)
            .setTitle("恢复常用线路")
            .setMessage("将从智能线路切回：${normal.name}")
            .setNegativeButton("取消", null)
            .setPositiveButton("恢复") { _, _ ->
                prefs.clearSmartMode(normal)
                sites.indexOfFirst { it.name == normal.name }.takeIf { it >= 0 }?.let(siteSpinner::setSelection)
                currentNodeName = normal.name
                connectSite(normal, currentMode, "正在恢复常用线路：${normal.name}")
                updateSmartControls(false)
            }
            .show()
    }

    private fun recoverIncompleteSmartSelection() {
        val transaction = prefs.loadSmartTransaction() ?: return
        if (usernameInput.text.isNullOrBlank() || passwordInput.text.isNullOrEmpty()) {
            statusView.text = "检测到未完成的智能选线，但缺少凭据，连接时将使用原线路：${transaction.originalSite.name}"
            return
        }
        if (VpnService.prepare(this) != null) {
            statusView.text = "检测到未完成的智能选线；请重新授权 VPN 后恢复：${transaction.originalSite.name}"
            return
        }
        val run = SmartRun(
            originalSite = transaction.originalSite,
            originalMode = transaction.originalMode,
            deadlineAtMs = Long.MAX_VALUE,
            phase = SmartPhase.RESTORING,
            restoreMessage = "启动时已恢复检测前线路",
        )
        smartRun = run
        updateSmartControls(true)
        connectSite(transaction.originalSite, transaction.originalMode, "正在恢复检测前线路")
    }

    private fun stopVpnService() {
        if (smartRun != null) {
            Toast.makeText(this, "智能选线进行中，请先取消", Toast.LENGTH_SHORT).show()
            return
        }
        connectionPageController.finishSiteSelection()
        startService(Intent(this, AnyConnectVpnService::class.java).apply {
            action = AnyConnectVpnService.ACTION_DISCONNECT
        })
        showIdleProgress()
        statusView.text = "已请求断开\n${OpenConnectInfo.versionSummary()}"
    }

    private fun restoreSavedSettings() {
        isRestoringSettings = true
        val settings = prefs.load()
        domesticDirectRules = settings.domesticDirectRules
        foreignDirectRules = settings.foreignDirectRules
        usernameInput.setText(settings.username)
        passwordInput.setText(settings.password)
        sites.indexOfFirst { it.name == settings.siteName }
            .takeIf { it >= 0 }
            ?.let { siteSpinner.setSelection(it) }
        ConnectionMode.entriesForUi.indexOf(settings.mode)
            .takeIf { it >= 0 }
            ?.let { modeSpinner.setSelection(it) }
        currentMode = settings.mode
        refreshRulesCardForMode(currentMode)
        isRestoringSettings = false
        if (settings.isConnectionFresh()) {
            showConnectedPage(
                nodeName = settings.lastNodeName.ifBlank { settings.siteName },
                mode = settings.lastMode,
                message = settings.lastMessage.ifBlank { "VPN 已连接。" },
            )
            updateSpeed(settings.rxBps, settings.txBps)
        } else {
            showFormPage()
        }
    }

    private fun handleStatusUpdate(update: VpnStatusUpdate) {
        handleSmartStatus(update)
        if (smartRun != null && update.state in setOf(
                AnyConnectVpnService.STATE_FAILED,
                AnyConnectVpnService.STATE_DISCONNECTED,
            )
        ) {
            statusView.text = "${update.message.ifBlank { "候选线路连接失败，正在继续检测…" }}\n${OpenConnectInfo.versionSummary()}"
            return
        }
        val mode = ConnectionMode.fromStored(update.modeName)
        when (update.state) {
            AnyConnectVpnService.STATE_CONNECTING -> {
                setProgressStep(82, update.message.ifBlank { "正在认证…" })
            }
            AnyConnectVpnService.STATE_CONNECTED -> {
                if (connectionPageController.shouldShowConnected(connectionActive = true)) {
                    showConnectedPage(
                        nodeName = update.nodeName.ifBlank { currentNodeName },
                        mode = mode,
                        message = update.message.ifBlank { "VPN 已连接。" },
                    )
                }
            }
            AnyConnectVpnService.STATE_STATS -> {
                if (connectionPageController.shouldShowConnected(connectionActive = true)) {
                    showConnectedPage(
                        nodeName = update.nodeName.ifBlank { currentNodeName },
                        mode = mode,
                        message = update.message.ifBlank { "VPN 已连接。" },
                    )
                }
                updateSpeed(update.rxBps, update.txBps)
            }
            AnyConnectVpnService.STATE_FAILED -> {
                connectionPageController.finishSiteSelection()
                setProgressStep(100, update.message.ifBlank { "连接失败。" })
                showFormPage()
            }
            AnyConnectVpnService.STATE_DISCONNECTED -> {
                connectionPageController.finishSiteSelection()
                showIdleProgress()
                showFormPage()
            }
        }
        statusView.text = "${update.message.ifBlank { getString(R.string.initial_status) }}\n${OpenConnectInfo.versionSummary()}"
    }

    private fun refreshStatusFromStorage() {
        val settings = prefs.load()
        if (connectionPageController.shouldShowConnected(settings.isConnectionFresh())) {
            showConnectedPage(
                nodeName = settings.lastNodeName.ifBlank { settings.siteName },
                mode = settings.lastMode,
                message = settings.lastMessage.ifBlank { "VPN 已连接。" },
            )
            updateSpeed(settings.rxBps, settings.txBps)
            return
        }
        if (isConnectedUi && !isVpnActive()) {
            showFormPage()
            showIdleProgress()
            statusView.text = "${getString(R.string.status_disconnected)}\n${OpenConnectInfo.versionSummary()}"
        }
    }

    private fun persistSettings() {
        val site = sites[siteSpinner.selectedItemPosition]
        prefs.saveSettings(
            username = usernameInput.text.toString(),
            password = passwordInput.text.toString(),
            site = site,
            mode = currentMode,
            domesticDirectRules = domesticDirectRules,
            foreignDirectRules = foreignDirectRules,
        )
    }

    private fun isVpnActive(): Boolean {
        val connectivityManager = getSystemService(ConnectivityManager::class.java)
        return connectivityManager.allNetworks.any { network ->
            connectivityManager.getNetworkCapabilities(network)
                ?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
        }
    }

    private fun com.msitools.anyconnectmobile.core.ClientSettings.isConnectionFresh(): Boolean {
        val connectedState = lastState == AnyConnectVpnService.STATE_CONNECTED ||
            lastState == AnyConnectVpnService.STATE_STATS
        val fresh = System.currentTimeMillis() - lastUpdatedAtMs <= STATUS_FRESH_MS
        return connectedState && (fresh || isVpnActive())
    }

    private fun selectedMode(): ConnectionMode =
        ConnectionMode.entriesForUi.getOrElse(modeSpinner.selectedItemPosition) {
            ConnectionMode.DOMESTIC_DIRECT
        }

    private fun refreshRulesCardForMode(mode: ConnectionMode) {
        if (!::rulesSummaryView.isInitialized || !::rulesTitleView.isInitialized) return
        rulesTitleView.text = mode.ruleTitle
        rulesSummaryView.text = summarizeRules(mode)
    }

    private fun rulesFor(mode: ConnectionMode): String = when (mode) {
        ConnectionMode.DOMESTIC_DIRECT -> domesticDirectRules
        ConnectionMode.FOREIGN_DIRECT -> foreignDirectRules
    }

    private fun setRulesFor(mode: ConnectionMode, rules: String) {
        when (mode) {
            ConnectionMode.DOMESTIC_DIRECT -> domesticDirectRules = rules
            ConnectionMode.FOREIGN_DIRECT -> foreignDirectRules = rules
        }
    }

    private fun summarizeRules(mode: ConnectionMode): String {
        val builtIn = BuiltInWhitelist.domainsFor(mode)
        val custom = rulesFor(mode).lineSequence()
            .map { it.trim() }
            .filter { it.isNotEmpty() }
            .toList()
        val preview = (builtIn + custom).distinct().take(5).joinToString("\n")
        val customText = if (custom.isEmpty()) "暂无自定义" else "自定义 ${custom.size} 条"
        return "$preview\n… 内置 ${builtIn.size} 条，$customText"
    }

    private fun showConnectedPage(nodeName: String, mode: ConnectionMode, message: String) {
        isConnectedUi = true
        currentNodeName = nodeName
        currentMode = mode
        formCardView.visibility = View.GONE
        connectedCardView.visibility = View.VISIBLE
        connectedNodeView.text = "当前节点：$nodeName"
        connectedModeView.text = "当前模式：${mode.label}"
        applyContactAuthorPresentation(isConnected = true)
        updateSmartControls(smartRun != null)
        refreshRulesCardForMode(mode)
        setProgressStep(100, message)
        statusView.text = "$message\n${OpenConnectInfo.versionSummary()}"
    }

    private fun showFormPage() {
        isConnectedUi = false
        if (::formCardView.isInitialized) formCardView.visibility = View.VISIBLE
        if (::connectedCardView.isInitialized) connectedCardView.visibility = View.GONE
        applyContactAuthorPresentation(isConnected = false)
    }

    private fun openSiteSelection() {
        if (smartRun != null) {
            Toast.makeText(this, "智能选线进行中，请先取消", Toast.LENGTH_SHORT).show()
            return
        }
        connectionPageController.requestSiteSelection()
        showFormPage()
    }

    private fun applyContactAuthorPresentation(isConnected: Boolean) {
        val presentation = contactAuthorPresentation(isConnected)
        if (::loginContactView.isInitialized) {
            loginContactView.visibility = if (presentation.showInlineQr) View.VISIBLE else View.GONE
        }
        if (::mainContactButton.isInitialized) {
            mainContactButton.visibility = if (presentation.showMainButton) View.VISIBLE else View.GONE
        }
    }

    private fun updateSpeed(rxBps: Long, txBps: Long) {
        if (!::downloadSpeedView.isInitialized || !::uploadSpeedView.isInitialized) return
        downloadSpeedView.text = "当前下载\n${formatSpeed(rxBps)}"
        uploadSpeedView.text = "当前上传\n${formatSpeed(txBps)}"
    }

    private fun toggleModeAndReconnect() {
        if (smartRun != null) {
            Toast.makeText(this, "智能选线进行中，请先取消", Toast.LENGTH_SHORT).show()
            return
        }
        val next = if (selectedMode() == ConnectionMode.DOMESTIC_DIRECT) {
            ConnectionMode.FOREIGN_DIRECT
        } else {
            ConnectionMode.DOMESTIC_DIRECT
        }
        modeSpinner.setSelection(ConnectionMode.entriesForUi.indexOf(next))
        currentMode = next
        refreshRulesCardForMode(next)
        persistSettings()
        if (isConnectedUi) {
            setProgressStep(36, "正在切换为${next.label}并重新连接…")
            startVpnService()
        }
    }

    private fun showRulesDialog(mode: ConnectionMode) {
        val dialog = Dialog(this)
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE)
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(22), dp(20), dp(22), dp(18))
            background = rounded(0xFFFFFDF2.toInt(), dp(28).toFloat(), 0xFFFFFFFF.toInt())
        }
        card.addView(TextView(this).apply {
            text = mode.ruleTitle
            textSize = 22f
            typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
            setTextColor(0xFF315F3B.toInt())
        }, matchWidth())
        card.addView(TextView(this).apply {
            text = "${mode.ruleHint}\n内置白名单始终生效且不可删除；自定义条目可在下方编辑。"
            textSize = 13f
            setTextColor(0xFF6D8059.toInt())
            setPadding(0, dp(6), 0, dp(12))
        }, matchWidth())

        card.addView(TextView(this).apply {
            text = "内置白名单（${BuiltInWhitelist.domainsFor(mode).size} 条，只读）"
            textSize = 14f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF4C6A3E.toInt())
            setPadding(0, 0, 0, dp(6))
        }, matchWidth())
        val builtInList = TextView(this).apply {
            text = BuiltInWhitelist.displayText(mode)
            textSize = 13f
            setTextColor(0xFF52604A.toInt())
            setTextIsSelectable(true)
            setPadding(dp(14), dp(12), dp(14), dp(12))
        }
        card.addView(ScrollView(this).apply {
            background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
            isFillViewport = true
            addView(builtInList, matchWidth())
        }, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(180)))

        card.addView(TextView(this).apply {
            text = "自定义白名单"
            textSize = 14f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF4C6A3E.toInt())
            setPadding(0, dp(10), 0, dp(6))
        }, matchWidth())

        val currentRulesInput = EditText(this).apply {
            minLines = 2
            maxLines = 4
            gravity = Gravity.TOP
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
            setText(rulesFor(mode))
            hint = "暂无自定义白名单"
            setFieldStyle()
            setPadding(dp(14), dp(12), dp(14), dp(12))
        }
        card.addView(currentRulesInput, LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            dp(96),
        ))

        val newRulesInput = EditText(this).apply {
            minLines = 1
            maxLines = 3
            gravity = Gravity.TOP
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
            hint = "新增内容，例如：google"
            setFieldStyle()
            setPadding(dp(14), dp(12), dp(14), dp(12))
        }
        card.addView(newRulesInput, LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            dp(76),
        ).apply {
            topMargin = dp(10)
        })

        val row = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(0, dp(14), 0, 0)
        }
        row.addView(Button(this).apply {
            text = "取消"
            setTextColor(0xFF4C6A3E.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(18).toFloat(), 0xFFE2C579.toInt())
            setOnClickListener { dialog.dismiss() }
        }, LinearLayout.LayoutParams(0, dp(50), 1f))
        row.addView(Button(this).apply {
            text = "解析并保存"
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF5FAF6C.toInt(), dp(18).toFloat(), 0x00000000)
            setOnClickListener {
                val result = WhitelistRuleExpander.mergeAndExpand(
                    currentRulesInput.text.toString(),
                    newRulesInput.text.toString(),
                )
                setRulesFor(mode, result.rulesText)
                currentMode = mode
                persistSettings()
                refreshRulesCardForMode(mode)
                val message = if (result.addedCount > 0) {
                    "已解析并新增 ${result.addedCount} 条，重新连接后生效"
                } else {
                    "白名单已保存，重新连接后生效"
                }
                Toast.makeText(this@MainActivity, message, Toast.LENGTH_SHORT).show()
                dialog.dismiss()
            }
        }, LinearLayout.LayoutParams(0, dp(50), 1f).apply {
            leftMargin = dp(10)
        })
        card.addView(row, matchWidth())

        dialog.setContentView(card)
        dialog.window?.setBackgroundDrawable(ColorDrawable(0x00000000))
        dialog.window?.setDimAmount(0.28f)
        dialog.show()
        dialog.window?.setLayout(
            (resources.displayMetrics.widthPixels * 0.92f).toInt(),
            ViewGroup.LayoutParams.WRAP_CONTENT,
        )
    }

    private fun metricCard(title: String, value: String): TextView = TextView(this).apply {
        text = "$title\n$value"
        textSize = 16f
        typeface = Typeface.DEFAULT_BOLD
        gravity = Gravity.CENTER
        setTextColor(0xFF315F3B.toInt())
        setPadding(dp(8), dp(14), dp(8), dp(14))
        background = rounded(0xFFFFFFFF.toInt(), dp(22).toFloat(), 0xFFD7E9BF.toInt())
    }

    private fun infoLine(title: String, value: String): TextView = TextView(this).apply {
        text = "$title：$value"
        textSize = 15f
        setTextColor(0xFF4D6940.toInt())
        setPadding(dp(12), dp(10), dp(12), dp(10))
        background = rounded(0xDFFFFFF7.toInt(), dp(18).toFloat(), 0x88D7E9BF.toInt())
    }

    private fun formatSpeed(bytesPerSecond: Long): String {
        val value = bytesPerSecond.coerceAtLeast(0L).toDouble()
        return when {
            value >= 1024 * 1024 -> String.format("%.2f MB/s", value / 1024.0 / 1024.0)
            value >= 1024 -> String.format("%.1f KB/s", value / 1024.0)
            else -> "${value.toLong()} B/s"
        }
    }

    private fun showContactDialog() {
        val dialog = Dialog(this)
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE)
        val qrSize = minOf(resources.displayMetrics.widthPixels - dp(96), dp(320))
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(dp(24), dp(22), dp(24), dp(20))
            background = rounded(0xFFFFFDF2.toInt(), dp(28).toFloat(), 0xFFFFFFFF.toInt())
        }
        card.addView(TextView(this).apply {
            text = getString(R.string.contact_title)
            textSize = 24f
            typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
            gravity = Gravity.CENTER
            setTextColor(0xFF315F3B.toInt())
        }, matchWidth())
        card.addView(TextView(this).apply {
            text = getString(R.string.contact_scan_hint)
            textSize = 14f
            gravity = Gravity.CENTER
            setTextColor(0xFF6B7F55.toInt())
            setPadding(0, dp(6), 0, dp(14))
        }, matchWidth())
        card.addView(ImageView(this).apply {
            setImageResource(R.drawable.wechat_contact_qr)
            scaleType = ImageView.ScaleType.CENTER_INSIDE
            adjustViewBounds = true
            background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
            setPadding(dp(8), dp(8), dp(8), dp(8))
        }, LinearLayout.LayoutParams(qrSize, qrSize))
        card.addView(TextView(this).apply {
            text = getString(R.string.contact_value)
            textSize = 18f
            gravity = Gravity.CENTER
            typeface = Typeface.MONOSPACE
            setTextColor(0xFF5C7A3D.toInt())
            setPadding(0, dp(12), 0, dp(14))
        }, matchWidth())
        val buttons = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
        }
        buttons.addView(Button(this).apply {
            text = getString(R.string.copy_contact)
            setTextColor(0xFFFFFFFF.toInt())
            background = rounded(0xFF5FAF6C.toInt(), dp(18).toFloat(), 0x00000000)
            setOnClickListener {
                val clipboard = getSystemService(ClipboardManager::class.java)
                clipboard.setPrimaryClip(ClipData.newPlainText("wechat", getString(R.string.contact_value)))
                Toast.makeText(this@MainActivity, R.string.contact_copied, Toast.LENGTH_SHORT).show()
            }
        }, LinearLayout.LayoutParams(0, dp(50), 1f))
        buttons.addView(Button(this).apply {
            text = getString(R.string.close)
            setTextColor(0xFF4C6A3E.toInt())
            background = rounded(0xFFFFF1C8.toInt(), dp(18).toFloat(), 0xFFE2C579.toInt())
            setOnClickListener { dialog.dismiss() }
        }, LinearLayout.LayoutParams(0, dp(50), 1f).apply {
            marginStart = dp(10)
        })
        card.addView(buttons, matchWidth())

        dialog.setContentView(card)
        dialog.window?.setBackgroundDrawable(ColorDrawable(0x00000000))
        dialog.window?.setDimAmount(0.28f)
        dialog.show()
        dialog.window?.setLayout(
            (resources.displayMetrics.widthPixels * 0.88f).toInt(),
            ViewGroup.LayoutParams.WRAP_CONTENT,
        )
    }

    private fun setProgressStep(value: Int, label: String) {
        loginProgress.visibility = View.VISIBLE
        progressLabel.visibility = View.VISIBLE
        loginProgress.progress = value
        progressLabel.text = label
    }

    private fun showIdleProgress() {
        loginProgress.progress = 0
        loginProgress.visibility = View.GONE
        progressLabel.visibility = View.GONE
    }

    private fun TextView.setFieldStyle() {
        textSize = 16f
        setTextColor(0xFF3F5634.toInt())
        setHintTextColor(0xFF9CA985.toInt())
        background = rounded(0xFFFFFFFF.toInt(), dp(18).toFloat(), 0xFFDDE8C3.toInt())
        setPadding(dp(14), 0, dp(14), 0)
    }

    private fun label(textValue: String): TextView = TextView(this).apply {
        text = textValue
        textSize = 14f
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(0xFF5D7046.toInt())
        setPadding(dp(2), 0, 0, dp(8))
    }

    private fun fieldLayout(): LinearLayout.LayoutParams =
        LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(52)).apply {
            topMargin = dp(10)
        }

    private fun marginTop(top: Int): LinearLayout.LayoutParams =
        LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
            topMargin = dp(top)
        }

    private fun matchWidth(): LinearLayout.LayoutParams = LinearLayout.LayoutParams(
        ViewGroup.LayoutParams.MATCH_PARENT,
        ViewGroup.LayoutParams.WRAP_CONTENT,
    )

    private fun dp(value: Int): Int = (resources.displayMetrics.density * value).toInt()

    private fun rounded(color: Int, radius: Float, stroke: Int): GradientDrawable =
        GradientDrawable().apply {
            setColor(color)
            cornerRadius = radius
            if (stroke != 0) setStroke(2, stroke)
        }

    private enum class SmartPhase {
        PROBING,
        CURRENT_DECISION,
        CONNECTING_CANDIDATE,
        RECOMMENDATION,
        RESTORING,
    }

    private data class SmartRun(
        val originalSite: VpnSite,
        val originalMode: ConnectionMode,
        val deadlineAtMs: Long,
        var phase: SmartPhase = SmartPhase.PROBING,
        var currentMetrics: SmartSelectionPolicy.Metrics? = null,
        var candidates: List<VpnSite> = emptyList(),
        var candidateIndex: Int = -1,
        var activeCandidate: VpnSite? = null,
        var recommendedMetrics: SmartSelectionPolicy.Metrics? = null,
        var probeStarted: Boolean = false,
        var restoreMessage: String = "",
    )

    companion object {
        private const val REQUEST_VPN = 100
        private const val REQUEST_NOTIFICATIONS = 101
        private const val STATUS_FRESH_MS = 8_000L
    }
}
