package tray

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/user/anyconnect-split/internal/config"
)

type Actions struct {
	OnDisconnect       func() error
	OnReconnect        func()
	OnCodexMode        func()
	OnRestoreNormal    func()
	OnSetSplitMode     func(mode string)
	OnUpdateIPDB       func()
	OnAddForeignDomain func(domain string) bool
	OnAddForeignCIDR   func(cidr string) bool
	OnViewLog          func()
	OnToggleAuto       func(enabled bool) error
	OnOpenDashboard    func()
	OnContactAuthor    func()
	OnQuit             func()
}

type Status struct {
	StatusText      string
	CurrentSite     string
	RouteCount      int
	SplitEnabled    bool
	SplitMode       string
	AutoStart       bool
	CodexModeActive bool
	LastError       string
}

type Tray struct {
	actionsMu         sync.RWMutex
	actions           Actions
	statusMu          sync.RWMutex
	status            Status
	statusListener    func(Status)
	menuStatus        *systray.MenuItem
	menuSite          *systray.MenuItem
	menuDisconnect    *systray.MenuItem
	menuReconnect     *systray.MenuItem
	menuCodex         *systray.MenuItem
	menuRestore       *systray.MenuItem
	menuSplitMode     *systray.MenuItem
	menuDomesticMode  *systray.MenuItem
	menuForeignMode   *systray.MenuItem
	menuUpdate        *systray.MenuItem
	menuAddDomain     *systray.MenuItem
	menuAddIP         *systray.MenuItem
	menuLog           *systray.MenuItem
	menuAutoStart     *systray.MenuItem
	menuContactAuthor *systray.MenuItem
	menuQuit          *systray.MenuItem
	iconAnimator      *trayIconAnimator
	splitEnabled      bool
	splitMode         string
	autoStart         bool
	codexModeActive   bool
	currentSite       string
	initialTooltip    string
	ready             chan struct{}
	quitOnce          sync.Once
}

func New(actions Actions, autoStart bool) *Tray {
	return &Tray{
		actions:      actions,
		splitEnabled: true,
		splitMode:    config.SplitModeDomesticDirect,
		autoStart:    autoStart,
		status: Status{
			StatusText:      "状态：VPN 未连接",
			SplitEnabled:    true,
			SplitMode:       config.SplitModeDomesticDirect,
			AutoStart:       autoStart,
			CodexModeActive: false,
		},
		iconAnimator: newTrayIconAnimator(nil),
		ready:        make(chan struct{}),
	}
}

// SetInitialTooltip sets the tooltip shown when the tray first appears (before VPN connects).
func (t *Tray) SetInitialTooltip(tooltip string) {
	t.initialTooltip = tooltip
}

// SetActions updates the action handlers (thread-safe).
func (t *Tray) SetActions(actions Actions) {
	t.actionsMu.Lock()
	t.actions = actions
	t.actionsMu.Unlock()
}

func (t *Tray) getActions() Actions {
	t.actionsMu.RLock()
	defer t.actionsMu.RUnlock()
	return t.actions
}

func (t *Tray) SetStatusListener(listener func(Status)) {
	t.statusMu.Lock()
	t.statusListener = listener
	status := t.status
	t.statusMu.Unlock()
	if listener != nil {
		listener(status)
	}
}

func (t *Tray) SetStatusFields(text string, routeCount int, lastError string) {
	t.statusMu.Lock()
	t.status.StatusText = text
	t.status.CurrentSite = t.currentSite
	t.status.RouteCount = routeCount
	t.status.SplitEnabled = t.splitEnabled
	t.status.SplitMode = t.splitMode
	t.status.AutoStart = t.autoStart
	t.status.CodexModeActive = t.codexModeActive
	t.status.LastError = lastError
	listener := t.statusListener
	status := t.status
	t.statusMu.Unlock()
	if listener != nil {
		listener(status)
	}
}

func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	log.Println("[tray] onReady: entered")
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in tray onReady: %v", r)
		}
		// 确保 ready channel 一定会被关闭，防止 WaitReady 死锁
		select {
		case <-t.ready:
		default:
			close(t.ready)
		}
		log.Println("[tray] onReady: defer complete, ready channel closed")
	}()

	log.Println("Tray onReady called, initializing...")

	log.Println("[tray] onReady: calling setTrayIconMode(idle)...")
	t.setTrayIconMode(trayIconModeIdle)
	log.Println("[tray] onReady: setTrayIconMode(idle) returned")
	systray.SetTitle("AnyConnect Split")
	if t.initialTooltip != "" {
		systray.SetTooltip(t.initialTooltip)
	} else {
		systray.SetTooltip("AnyConnect 分流 - VPN 未连接")
	}

	initStatus := "状态：VPN 未连接"
	if t.initialTooltip != "" {
		initStatus = "状态：初始化中..."
	}
	t.menuStatus = systray.AddMenuItem(initStatus, "")
	t.menuStatus.Disable()
	t.menuSite = systray.AddMenuItem(t.currentSiteTitle(), "")
	t.menuSite.Disable()
	systray.AddSeparator()

	t.menuDisconnect = systray.AddMenuItem("断开 VPN", "断开当前 VPN 连接")
	t.menuReconnect = systray.AddMenuItem("重新连接", "使用新凭据重新连接")
	t.menuCodex = systray.AddMenuItem("ChatGPT/Codex 智能选线", "最多 60 秒，严格验证后由你确认或倒计时采用")
	t.menuRestore = systray.AddMenuItem("恢复常用线路", "切回配置中的常用 VPN 节点")
	systray.AddSeparator()

	domesticChecked, foreignChecked := splitModeChecks(t.splitMode)
	t.menuSplitMode = systray.AddMenuItem("分流模式", "选择默认流量路径")
	t.menuDomesticMode = t.menuSplitMode.AddSubMenuItemCheckbox(
		"国内直连优先",
		"默认使用本地网络，仅国外白名单走 VPN",
		domesticChecked,
	)
	t.menuForeignMode = t.menuSplitMode.AddSubMenuItemCheckbox(
		"国外 VPN 优先",
		"默认使用 VPN，仅国内白名单直连",
		foreignChecked,
	)
	t.menuUpdate = systray.AddMenuItem("更新 IP 数据库", "下载最新国内 IP 列表")
	t.menuAddDomain = systray.AddMenuItem("添加国外域名…", "快速添加一个国外域名到 VPN 白名单")
	t.menuAddIP = systray.AddMenuItem("添加国外 IP/CIDR…", "快速添加一个国外 IP 或 CIDR 到 VPN 白名单")
	t.menuLog = systray.AddMenuItem("查看日志", "打开日志文件")
	systray.AddSeparator()

	t.menuAutoStart = systray.AddMenuItemCheckbox("开机自启", "登录时自动启动", t.autoStart)
	systray.AddSeparator()
	t.menuContactAuthor = systray.AddMenuItem("联系作者", "联系作者获取帮助")
	t.menuQuit = systray.AddMenuItem("退出", "退出应用程序")

	log.Println("Tray initialized successfully")
	t.refreshSiteDisplay()

	if a := t.getActions(); a.OnOpenDashboard != nil {
		systray.SetIconClickHandler(func(left bool) {
			button := iconButtonFromClick(left)
			switch ResolveIconClickAction(button, true) {
			case IconClickOpenDashboard:
				a := t.getActions()
				if a.OnOpenDashboard != nil {
					go a.OnOpenDashboard()
				}
			}
		})
	}

	go t.handleClicks()
	go t.handleContactAuthorClicks()
	go t.handleQuitClicks()
}

func (t *Tray) onExit() {
	t.stopTrayIconAnimation()
}

// WaitReady blocks until the tray is fully initialized, with a 30-second timeout.
// If the timeout expires, a warning is logged but execution continues.
func (t *Tray) WaitReady() {
	select {
	case <-t.ready:
		log.Println("[tray] WaitReady: ready signal received")
	case <-time.After(30 * time.Second):
		log.Println("WARNING: [tray] WaitReady: timed out after 30s, onReady may not have been called. Continuing anyway...")
	}
}

// ReadyChan returns the ready channel so callers can implement their own timeout logic.
func (t *Tray) ReadyChan() <-chan struct{} {
	return t.ready
}

func (t *Tray) handleClicks() {
	for {
		select {
		case <-t.menuDisconnect.ClickedCh:
			a := t.getActions()
			if a.OnDisconnect != nil {
				a.OnDisconnect()
			}
		case <-t.menuReconnect.ClickedCh:
			a := t.getActions()
			if a.OnReconnect != nil {
				a.OnReconnect()
			}
		case <-t.menuCodex.ClickedCh:
			a := t.getActions()
			if a.OnCodexMode != nil {
				a.OnCodexMode()
			}
		case <-t.menuRestore.ClickedCh:
			a := t.getActions()
			if a.OnRestoreNormal != nil {
				a.OnRestoreNormal()
			}
		case <-t.menuDomesticMode.ClickedCh:
			t.requestSplitMode(config.SplitModeDomesticDirect)
		case <-t.menuForeignMode.ClickedCh:
			t.requestSplitMode(config.SplitModeForeignDirect)
		case <-t.menuUpdate.ClickedCh:
			a := t.getActions()
			if a.OnUpdateIPDB != nil {
				a.OnUpdateIPDB()
			}
		case <-t.menuAddDomain.ClickedCh:
			t.promptAddForeignDomain()
		case <-t.menuAddIP.ClickedCh:
			t.promptAddForeignCIDR()
		case <-t.menuLog.ClickedCh:
			a := t.getActions()
			if a.OnViewLog != nil {
				a.OnViewLog()
			}
		case <-t.menuAutoStart.ClickedCh:
			previous := t.autoStart
			t.autoStart = !t.autoStart
			if t.autoStart {
				t.menuAutoStart.Check()
			} else {
				t.menuAutoStart.Uncheck()
			}
			a := t.getActions()
			if a.OnToggleAuto != nil {
				if err := a.OnToggleAuto(t.autoStart); err != nil {
					log.Printf("Failed to toggle autostart: %v", err)
					t.autoStart = previous
					if t.autoStart {
						t.menuAutoStart.Check()
					} else {
						t.menuAutoStart.Uncheck()
					}
				}
			}
		}
	}
}

func (t *Tray) handleContactAuthorClicks() {
	for range t.menuContactAuthor.ClickedCh {
		a := t.getActions()
		if a.OnContactAuthor != nil {
			a.OnContactAuthor()
			continue
		}
		ShowContactAuthor()
	}
}

func (t *Tray) handleQuitClicks() {
	<-t.menuQuit.ClickedCh
	t.requestQuit()
}

func (t *Tray) requestQuit() {
	t.quitOnce.Do(func() {
		log.Println("Tray quit clicked")
		t.menuQuit.Disable()
		t.SetStatusBusy("正在退出...")
		a := t.getActions()
		if a.OnQuit != nil {
			a.OnQuit()
			return
		}
		systray.Quit()
	})
}

func (t *Tray) promptAddForeignDomain() {
	value, ok := PromptInput("添加国外域名", "请输入国外域名（如 openai.com）：")
	if !ok {
		return
	}
	a := t.getActions()
	if a.OnAddForeignDomain == nil {
		return
	}
	if a.OnAddForeignDomain(value) {
		notifyInfo("已添加国外域名", "已加入白名单并保存，已连接时将自动刷新路由。")
	} else {
		notifyInfo("添加国外域名", "域名格式无效或已存在，未做更改。")
	}
}

func (t *Tray) requestSplitMode(mode string) {
	normalized, ok := config.NormalizeSplitMode(mode)
	if !ok || normalized == t.splitMode {
		return
	}
	a := t.getActions()
	if a.OnSetSplitMode != nil {
		a.OnSetSplitMode(normalized)
	}
}

func (t *Tray) promptAddForeignCIDR() {
	value, ok := PromptInput("添加国外 IP/CIDR", "请输入国外 IP 或 CIDR（如 1.2.3.0/24）：")
	if !ok {
		return
	}
	a := t.getActions()
	if a.OnAddForeignCIDR == nil {
		return
	}
	if a.OnAddForeignCIDR(value) {
		notifyInfo("已添加国外 IP/CIDR", "已加入白名单并保存，已连接时将自动刷新路由。")
	} else {
		notifyInfo("添加国外 IP/CIDR", "IP/CIDR 格式无效或已存在，未做更改。")
	}
}

func notifyInfo(title, message string) {
	script := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('` + escapePS(message) + `', '` + escapePS(title) + `', [System.Windows.Forms.MessageBoxButtons]::OK, [System.Windows.Forms.MessageBoxIcon]::Information)`
	exec.Command("powershell", "-NoProfile", "-Command", script).Start()
}

func (t *Tray) siteLabel() string {
	site := strings.TrimSpace(t.currentSite)
	if site == "" {
		return "未连接"
	}
	return site
}

func (t *Tray) currentSiteTitle() string {
	return "当前站点：" + t.siteLabel()
}

func (t *Tray) tooltip(state string) string {
	if strings.TrimSpace(t.currentSite) == "" {
		return "AnyConnect 分流 - " + state
	}
	return fmt.Sprintf("AnyConnect 分流 - %s - 当前站点：%s", state, t.siteLabel())
}

func (t *Tray) refreshSiteDisplay() {
	site := t.siteLabel()
	t.statusMu.Lock()
	t.status.CurrentSite = strings.TrimSpace(t.currentSite)
	t.status.SplitEnabled = t.splitEnabled
	t.status.SplitMode = t.splitMode
	t.status.AutoStart = t.autoStart
	t.status.CodexModeActive = t.codexModeActive
	listener := t.statusListener
	status := t.status
	t.statusMu.Unlock()
	if listener != nil {
		listener(status)
	}
	if t.menuSite != nil {
		t.menuSite.SetTitle("当前站点：" + site)
		t.menuSite.SetTooltip("当前 VPN 站点：" + site)
	}
	if t.menuStatus != nil {
		t.menuStatus.SetTooltip("当前 VPN 站点：" + site)
	}
	if t.menuDisconnect != nil {
		t.menuDisconnect.SetTooltip("断开当前 VPN 连接（当前站点：" + site + "）")
	}
	if t.menuReconnect != nil {
		t.menuReconnect.SetTooltip("使用新凭据重新连接（当前站点：" + site + "）")
	}
	if t.menuCodex != nil {
		t.menuCodex.SetTooltip("自动选择对 Codex 不返回 403 的线路（当前站点：" + site + "）")
	}
	if t.menuRestore != nil {
		t.menuRestore.SetTooltip("切回配置中的常用 VPN 节点（当前站点：" + site + "）")
	}
	t.refreshModeActionVisibility()
	t.refreshSplitModeChecks()
}

func (t *Tray) SetCurrentSite(site string) {
	t.currentSite = strings.TrimSpace(site)
	t.refreshSiteDisplay()
}

func (t *Tray) ClearCurrentSite() {
	t.SetCurrentSite("")
}

func (t *Tray) SetSplitMode(mode string) {
	normalized, ok := config.NormalizeSplitMode(mode)
	if !ok {
		return
	}
	t.splitMode = normalized
	t.refreshSiteDisplay()
}

func splitModeChecks(mode string) (domestic bool, foreign bool) {
	normalized, ok := config.NormalizeSplitMode(mode)
	if !ok {
		return false, false
	}
	return normalized == config.SplitModeDomesticDirect, normalized == config.SplitModeForeignDirect
}

func (t *Tray) refreshSplitModeChecks() {
	domestic, foreign := splitModeChecks(t.splitMode)
	if t.menuDomesticMode != nil {
		if domestic {
			t.menuDomesticMode.Check()
		} else {
			t.menuDomesticMode.Uncheck()
		}
	}
	if t.menuForeignMode != nil {
		if foreign {
			t.menuForeignMode.Check()
		} else {
			t.menuForeignMode.Uncheck()
		}
	}
}

func (t *Tray) SetAutoStartEnabled(enabled bool) {
	t.autoStart = enabled
	if t.menuAutoStart != nil {
		if enabled {
			t.menuAutoStart.Check()
		} else {
			t.menuAutoStart.Uncheck()
		}
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetCodexModeActive(active bool) {
	t.codexModeActive = active
	t.refreshSiteDisplay()
}

func modeActionVisibility(codexModeActive bool) (showCodex bool, showRestore bool) {
	return !codexModeActive, codexModeActive
}

func (t *Tray) refreshModeActionVisibility() {
	showCodex, showRestore := modeActionVisibility(t.codexModeActive)
	if t.menuCodex != nil {
		if showCodex {
			t.menuCodex.Show()
		} else {
			t.menuCodex.Hide()
		}
	}
	if t.menuRestore != nil {
		if showRestore {
			t.menuRestore.Show()
		} else {
			t.menuRestore.Hide()
		}
	}
}

func (t *Tray) SetStatusIdle() {
	title := "状态：VPN 未连接"
	t.SetStatusFields(title, 0, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeIdle)
		systray.SetTooltip(t.tooltip("VPN 未连接"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusActive(routeCount int) {
	title := fmt.Sprintf("状态：分流已启用（%d 条路由）", routeCount)
	t.SetStatusFields(title, routeCount, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeActive)
		systray.SetTooltip(t.tooltip("VPN 已连接"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusTunActive() {
	title := "状态：TUN 分流已启用"
	t.SetStatusFields(title, 0, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeActive)
		systray.SetTooltip(t.tooltip("TUN 分流已启用"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusTunFullTunnel() {
	title := "状态：TUN 全隧道已启用"
	t.SetStatusFields(title, 0, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeActive)
		systray.SetTooltip(t.tooltip("TUN 全隧道已启用"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusStaticFallback(routeCount int) {
	title := fmt.Sprintf("状态：已回退静态路由（%d 条路由）", routeCount)
	t.SetStatusFields(title, routeCount, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeActive)
		systray.SetTooltip(t.tooltip("已回退静态路由"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusSplitDisabled() {
	title := "状态：VPN 已连接，分流未启用"
	t.SetStatusFields(title, 0, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeIdle)
		systray.SetTooltip(t.tooltip("分流未启用"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusBusy(msg string) {
	title := fmt.Sprintf("状态：%s", msg)
	t.SetStatusFields(title, 0, "")
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeBusy)
		systray.SetTooltip(t.tooltip("处理中..."))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusError(msg string) {
	title := fmt.Sprintf("错误：%s", msg)
	t.SetStatusFields(title, 0, msg)
	if t.menuStatus != nil {
		t.setTrayIconMode(trayIconModeError)
		systray.SetTooltip(t.tooltip("出错"))
		t.menuStatus.SetTitle(title)
	}
	t.refreshSiteDisplay()
}

func ShowContactAuthor() {
	script := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show("作者：卓哥` + "`n" + `微信号：ai_creater99` + "`n`n" + `有任何优化或定制需求，欢迎找卓哥~", "联系作者", [System.Windows.Forms.MessageBoxButtons]::OK, [System.Windows.Forms.MessageBoxIcon]::Information)`
	exec.Command("powershell", "-NoProfile", "-Command", script).Start()
}
