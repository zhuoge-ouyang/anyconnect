package com.msitools.anyconnectmobile.core

import android.content.Context

data class ClientSettings(
    val username: String,
    val password: String,
    val siteName: String,
    val siteServer: String,
    val mode: ConnectionMode,
    val domesticDirectRules: String,
    val foreignDirectRules: String,
    val lastState: String,
    val lastMessage: String,
    val lastNodeName: String,
    val lastMode: ConnectionMode,
    val rxBps: Long,
    val txBps: Long,
    val lastUpdatedAtMs: Long,
    val smartModeActive: Boolean,
    val normalSiteName: String,
    val normalSiteServer: String,
) {
    fun rulesFor(mode: ConnectionMode): String = BuiltInWhitelist.effectiveRules(
        mode,
        when (mode) {
            ConnectionMode.DOMESTIC_DIRECT -> domesticDirectRules
            ConnectionMode.FOREIGN_DIRECT -> foreignDirectRules
        },
    )
}

class ClientPreferences(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun load(): ClientSettings = ClientSettings(
        username = prefs.getString(KEY_USERNAME, "").orEmpty(),
        password = prefs.getString(KEY_PASSWORD, "").orEmpty(),
        siteName = prefs.getString(KEY_SITE_NAME, DEFAULT_SITE_NAME).orEmpty(),
        siteServer = prefs.getString(KEY_SITE_SERVER, "").orEmpty(),
        mode = ConnectionMode.fromStored(prefs.getString(KEY_MODE, null)),
        domesticDirectRules = prefs.getString(KEY_DOMESTIC_DIRECT_RULES, "").orEmpty(),
        foreignDirectRules = prefs.getString(KEY_FOREIGN_DIRECT_RULES, "").orEmpty(),
        lastState = prefs.getString(KEY_LAST_STATE, "").orEmpty(),
        lastMessage = prefs.getString(KEY_LAST_MESSAGE, "").orEmpty(),
        lastNodeName = prefs.getString(KEY_LAST_NODE_NAME, "").orEmpty(),
        lastMode = ConnectionMode.fromStored(prefs.getString(KEY_LAST_MODE, null)),
        rxBps = prefs.getLong(KEY_RX_BPS, 0L),
        txBps = prefs.getLong(KEY_TX_BPS, 0L),
        lastUpdatedAtMs = prefs.getLong(KEY_LAST_UPDATED_AT_MS, 0L),
        smartModeActive = prefs.getBoolean(KEY_SMART_MODE_ACTIVE, false),
        normalSiteName = prefs.getString(KEY_NORMAL_SITE_NAME, "").orEmpty(),
        normalSiteServer = prefs.getString(KEY_NORMAL_SITE_SERVER, "").orEmpty(),
    )

    fun saveSettings(
        username: String,
        password: String,
        site: VpnSite,
        mode: ConnectionMode,
        domesticDirectRules: String,
        foreignDirectRules: String,
    ) {
        prefs.edit()
            .putString(KEY_USERNAME, username)
            .putString(KEY_PASSWORD, password)
            .putString(KEY_SITE_NAME, site.name)
            .putString(KEY_SITE_SERVER, site.server)
            .putString(KEY_MODE, mode.name)
            .putString(KEY_DOMESTIC_DIRECT_RULES, domesticDirectRules)
            .putString(KEY_FOREIGN_DIRECT_RULES, foreignDirectRules)
            .putBoolean(KEY_SMART_MODE_ACTIVE, false)
            .remove(KEY_NORMAL_SITE_NAME)
            .remove(KEY_NORMAL_SITE_SERVER)
            .apply()
    }

    fun saveRules(domesticDirectRules: String, foreignDirectRules: String, mode: ConnectionMode) {
        prefs.edit()
            .putString(KEY_DOMESTIC_DIRECT_RULES, domesticDirectRules)
            .putString(KEY_FOREIGN_DIRECT_RULES, foreignDirectRules)
            .putString(KEY_MODE, mode.name)
            .apply()
    }

    fun saveSmartTransaction(transaction: SmartSelectionTransaction) {
        prefs.edit()
            .putBoolean(KEY_SMART_TX_PENDING, true)
            .putString(KEY_SMART_TX_SITE_NAME, transaction.originalSite.name)
            .putString(KEY_SMART_TX_SITE_SERVER, transaction.originalSite.server)
            .putString(KEY_SMART_TX_MODE, transaction.originalMode.name)
            .putLong(KEY_SMART_TX_STARTED_AT, transaction.startedAtMs)
            .commit()
    }

    fun loadSmartTransaction(): SmartSelectionTransaction? {
        if (!prefs.getBoolean(KEY_SMART_TX_PENDING, false)) return null
        val name = prefs.getString(KEY_SMART_TX_SITE_NAME, "").orEmpty()
        val server = prefs.getString(KEY_SMART_TX_SITE_SERVER, "").orEmpty()
        if (name.isBlank() || server.isBlank()) return null
        return SmartSelectionTransaction(
            originalSite = VpnSite(name, server),
            originalMode = ConnectionMode.fromStored(prefs.getString(KEY_SMART_TX_MODE, null)),
            startedAtMs = prefs.getLong(KEY_SMART_TX_STARTED_AT, 0L),
        )
    }

    fun clearSmartTransaction() {
        prefs.edit().putBoolean(KEY_SMART_TX_PENDING, false).commit()
    }

    fun acceptSmartSelection(original: VpnSite, recommended: VpnSite) {
        val current = load()
        val normal = if (current.smartModeActive && current.normalSiteName.isNotBlank()) {
            VpnSite(current.normalSiteName, current.normalSiteServer)
        } else {
            original
        }
        prefs.edit()
            .putBoolean(KEY_SMART_MODE_ACTIVE, true)
            .putString(KEY_NORMAL_SITE_NAME, normal.name)
            .putString(KEY_NORMAL_SITE_SERVER, normal.server)
            .putString(KEY_SITE_NAME, recommended.name)
            .putString(KEY_SITE_SERVER, recommended.server)
            .commit()
    }

    fun clearSmartMode(normal: VpnSite) {
        prefs.edit()
            .putBoolean(KEY_SMART_MODE_ACTIVE, false)
            .putString(KEY_SITE_NAME, normal.name)
            .putString(KEY_SITE_SERVER, normal.server)
            .commit()
    }

    fun loadSmartHistory(): Map<String, SmartHealthRecord> =
        prefs.getStringSet(KEY_SMART_HISTORY_SITES, emptySet()).orEmpty().associateWith(::loadHealthRecord)

    fun recordSmartResult(site: String, metrics: SmartSelectionPolicy.Metrics, success: Boolean) {
        val previous = loadHealthRecord(site)
        val now = System.currentTimeMillis()
        val next = if (success) {
            previous.copy(
                successes = previous.successes + 1,
                consecutiveFailures = 0,
                lastSuccessAtMs = now,
                medianMs = metrics.medianMs,
                slowestMs = metrics.slowestMs,
            )
        } else {
            previous.copy(
                failures = previous.failures + 1,
                consecutiveFailures = previous.consecutiveFailures + 1,
                lastFailureAtMs = now,
            )
        }
        val sites = prefs.getStringSet(KEY_SMART_HISTORY_SITES, emptySet()).orEmpty().toMutableSet().apply { add(site) }
        val key = healthKey(site)
        prefs.edit()
            .putStringSet(KEY_SMART_HISTORY_SITES, sites)
            .putInt("${key}_successes", next.successes)
            .putInt("${key}_failures", next.failures)
            .putInt("${key}_consecutive", next.consecutiveFailures)
            .putLong("${key}_last_success", next.lastSuccessAtMs)
            .putLong("${key}_last_failure", next.lastFailureAtMs)
            .putLong("${key}_median", next.medianMs)
            .putLong("${key}_slowest", next.slowestMs)
            .commit()
    }

    fun markSmartSelected(site: String) {
        prefs.edit().putLong("${healthKey(site)}_last_selected", System.currentTimeMillis()).commit()
    }

    private fun loadHealthRecord(site: String): SmartHealthRecord {
        val key = healthKey(site)
        return SmartHealthRecord(
            siteName = site,
            successes = prefs.getInt("${key}_successes", 0),
            failures = prefs.getInt("${key}_failures", 0),
            consecutiveFailures = prefs.getInt("${key}_consecutive", 0),
            lastSuccessAtMs = prefs.getLong("${key}_last_success", 0L),
            lastFailureAtMs = prefs.getLong("${key}_last_failure", 0L),
            medianMs = prefs.getLong("${key}_median", 0L),
            slowestMs = prefs.getLong("${key}_slowest", 0L),
            lastSelectedAtMs = prefs.getLong("${key}_last_selected", 0L),
        )
    }

    private fun healthKey(site: String): String = "smart_health_${Integer.toHexString(site.hashCode())}"

    fun saveConnectionStatus(
        state: String,
        message: String,
        nodeName: String,
        mode: ConnectionMode,
        rxBps: Long,
        txBps: Long,
    ) {
        prefs.edit()
            .putString(KEY_LAST_STATE, state)
            .putString(KEY_LAST_MESSAGE, message)
            .putString(KEY_LAST_NODE_NAME, nodeName)
            .putString(KEY_LAST_MODE, mode.name)
            .putLong(KEY_RX_BPS, rxBps)
            .putLong(KEY_TX_BPS, txBps)
            .putLong(KEY_LAST_UPDATED_AT_MS, System.currentTimeMillis())
            .commit()
    }

    companion object {
        private const val PREFS_NAME = "anyconnect_mobile_settings"
        private const val DEFAULT_SITE_NAME = "22.日本"

        private const val KEY_USERNAME = "username"
        private const val KEY_PASSWORD = "password"
        private const val KEY_SITE_NAME = "site_name"
        private const val KEY_SITE_SERVER = "site_server"
        private const val KEY_MODE = "mode"
        private const val KEY_DOMESTIC_DIRECT_RULES = "domestic_direct_rules"
        private const val KEY_FOREIGN_DIRECT_RULES = "foreign_direct_rules"
        private const val KEY_LAST_STATE = "last_state"
        private const val KEY_LAST_MESSAGE = "last_message"
        private const val KEY_LAST_NODE_NAME = "last_node_name"
        private const val KEY_LAST_MODE = "last_mode"
        private const val KEY_RX_BPS = "rx_bps"
        private const val KEY_TX_BPS = "tx_bps"
        private const val KEY_LAST_UPDATED_AT_MS = "last_updated_at_ms"
        private const val KEY_SMART_MODE_ACTIVE = "smart_mode_active"
        private const val KEY_NORMAL_SITE_NAME = "normal_site_name"
        private const val KEY_NORMAL_SITE_SERVER = "normal_site_server"
        private const val KEY_SMART_TX_PENDING = "smart_tx_pending"
        private const val KEY_SMART_TX_SITE_NAME = "smart_tx_site_name"
        private const val KEY_SMART_TX_SITE_SERVER = "smart_tx_site_server"
        private const val KEY_SMART_TX_MODE = "smart_tx_mode"
        private const val KEY_SMART_TX_STARTED_AT = "smart_tx_started_at"
        private const val KEY_SMART_HISTORY_SITES = "smart_history_sites"
    }
}

data class SmartSelectionTransaction(
    val originalSite: VpnSite,
    val originalMode: ConnectionMode,
    val startedAtMs: Long,
)
