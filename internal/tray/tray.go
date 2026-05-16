package tray

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/getlantern/systray"
)

type Actions struct {
	OnDisconnect    func() error
	OnReconnect     func()
	OnCodexMode     func()
	OnRestoreNormal func()
	OnToggleSplit   func(enabled bool)
	OnUpdateIPDB    func()
	OnViewLog       func()
	OnToggleAuto    func(enabled bool) error
	OnQuit          func()
}

type Tray struct {
	actionsMu         sync.RWMutex
	actions           Actions
	menuStatus        *systray.MenuItem
	menuSite          *systray.MenuItem
	menuDisconnect    *systray.MenuItem
	menuReconnect     *systray.MenuItem
	menuCodex         *systray.MenuItem
	menuRestore       *systray.MenuItem
	menuToggle        *systray.MenuItem
	menuUpdate        *systray.MenuItem
	menuLog           *systray.MenuItem
	menuAutoStart     *systray.MenuItem
	menuContactAuthor *systray.MenuItem
	menuQuit          *systray.MenuItem
	splitEnabled      bool
	autoStart         bool
	currentSite       string
	initialTooltip    string
	ready             chan struct{}
	quitOnce          sync.Once
}

func New(actions Actions, splitEnabled bool, autoStart bool) *Tray {
	return &Tray{
		actions:      actions,
		splitEnabled: splitEnabled,
		autoStart:    autoStart,
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

func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
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
	}()

	log.Println("Tray onReady called, initializing...")

	systray.SetIcon(iconIdle)
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
	t.menuCodex = systray.AddMenuItem("切到 Codex 稳定线路", "自动选择对 Codex 不返回 403 的线路")
	t.menuRestore = systray.AddMenuItem("恢复常用线路", "切回配置中的常用 VPN 节点")
	systray.AddSeparator()

	t.menuToggle = systray.AddMenuItemCheckbox("启用分流", "切换分流模式", t.splitEnabled)
	t.menuUpdate = systray.AddMenuItem("更新 IP 数据库", "下载最新国内 IP 列表")
	t.menuLog = systray.AddMenuItem("查看日志", "打开日志文件")
	systray.AddSeparator()

	t.menuAutoStart = systray.AddMenuItemCheckbox("开机自启", "登录时自动启动", t.autoStart)
	systray.AddSeparator()
	t.menuContactAuthor = systray.AddMenuItem("联系作者", "联系作者获取帮助")
	t.menuQuit = systray.AddMenuItem("退出", "退出应用程序")

	log.Println("Tray initialized successfully")
	t.refreshSiteDisplay()

	go t.handleClicks()
	go t.handleContactAuthorClicks()
	go t.handleQuitClicks()
}

func (t *Tray) onExit() {}

// WaitReady blocks until the tray is fully initialized.
func (t *Tray) WaitReady() {
	<-t.ready
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
		case <-t.menuToggle.ClickedCh:
			t.splitEnabled = !t.splitEnabled
			if t.splitEnabled {
				t.menuToggle.Check()
			} else {
				t.menuToggle.Uncheck()
			}
			a := t.getActions()
			if a.OnToggleSplit != nil {
				a.OnToggleSplit(t.splitEnabled)
			}
		case <-t.menuUpdate.ClickedCh:
			a := t.getActions()
			if a.OnUpdateIPDB != nil {
				a.OnUpdateIPDB()
			}
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
		script := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show("作者：卓哥` + "`n" + `微信号：ai_creater99` + "`n`n" + `有任何优化或定制需求，欢迎找卓哥~", "联系作者", [System.Windows.Forms.MessageBoxButtons]::OK, [System.Windows.Forms.MessageBoxIcon]::Information)`
		exec.Command("powershell", "-NoProfile", "-Command", script).Start()
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
}

func (t *Tray) SetCurrentSite(site string) {
	t.currentSite = strings.TrimSpace(site)
	t.refreshSiteDisplay()
}

func (t *Tray) ClearCurrentSite() {
	t.SetCurrentSite("")
}

func (t *Tray) SetStatusIdle() {
	systray.SetIcon(iconIdle)
	systray.SetTooltip(t.tooltip("VPN 未连接"))
	t.menuStatus.SetTitle("状态：VPN 未连接")
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusActive(routeCount int) {
	systray.SetIcon(iconActive)
	systray.SetTooltip(t.tooltip("VPN 已连接"))
	t.menuStatus.SetTitle(fmt.Sprintf("状态：分流已启用（%d 条路由）", routeCount))
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusTunActive() {
	systray.SetIcon(iconActive)
	systray.SetTooltip(t.tooltip("TUN 分流已启用"))
	t.menuStatus.SetTitle("状态：TUN 分流已启用")
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusTunFullTunnel() {
	systray.SetIcon(iconActive)
	systray.SetTooltip(t.tooltip("TUN 全隧道已启用"))
	t.menuStatus.SetTitle("状态：TUN 全隧道已启用")
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusStaticFallback(routeCount int) {
	systray.SetIcon(iconActive)
	systray.SetTooltip(t.tooltip("已回退静态路由"))
	t.menuStatus.SetTitle(fmt.Sprintf("状态：已回退静态路由（%d 条路由）", routeCount))
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusSplitDisabled() {
	systray.SetIcon(iconIdle)
	systray.SetTooltip(t.tooltip("分流未启用"))
	t.menuStatus.SetTitle("状态：VPN 已连接，分流未启用")
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusBusy(msg string) {
	systray.SetIcon(iconBusy)
	systray.SetTooltip(t.tooltip("处理中..."))
	t.menuStatus.SetTitle(fmt.Sprintf("状态：%s", msg))
	t.refreshSiteDisplay()
}

func (t *Tray) SetStatusError(msg string) {
	systray.SetIcon(iconError)
	systray.SetTooltip(t.tooltip("出错"))
	t.menuStatus.SetTitle(fmt.Sprintf("错误：%s", msg))
	t.refreshSiteDisplay()
}
