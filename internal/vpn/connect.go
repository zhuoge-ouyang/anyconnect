package vpn

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/user/anyconnect-split/internal/monitor"
)

// killConflictingProcesses 杀掉可能冲突的 AnyConnect 进程
// vpnui.exe 运行时会阻止 vpncli -s 建立新连接
// 残留的 vpncli.exe 也会导致 "Connect not available" 错误
func killConflictingProcesses() {
	for _, proc := range []string{"vpnui.exe", "vpncli.exe"} {
		cmd := exec.Command("taskkill", "/F", "/IM", proc)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
		if err := cmd.Run(); err == nil {
			log.Printf("Killed conflicting process: %s", proc)
		}
	}
	// 等待进程完全退出
	time.Sleep(2 * time.Second)
}

func runHiddenWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("%s timed out", name)
	}
	return output, err
}

func ensureAgentService() error {
	// 快速检查：服务已运行则立即返回（绝大多数正常场景走这里，<1秒）
	if output, err := runHiddenWithTimeout(3*time.Second, "sc", "query", "vpnagent"); err == nil {
		if strings.Contains(strings.ToUpper(string(output)), "RUNNING") {
			return nil
		}
	}

	// 服务未运行，尝试启动
	log.Println("vpnagent service not running, attempting to start...")
	runHiddenWithTimeout(5*time.Second, "net", "start", "vpnagent")

	// 轮询等待服务启动，最多15秒
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		if output, err := runHiddenWithTimeout(3*time.Second, "sc", "query", "vpnagent"); err == nil {
			if strings.Contains(strings.ToUpper(string(output)), "RUNNING") {
				log.Println("vpnagent service started successfully")
				return nil
			}
		}
	}
	return fmt.Errorf("vpnagent service failed to start within 15 seconds")
}

func isVPNAdapterUp() bool {
	return monitor.HasActiveCiscoAdapter()
}

func hasVPNNetworkEvidence() bool {
	return monitor.HasActiveCiscoAdapter() || monitor.HasCiscoDefaultRoute()
}

// PrepareForNewConnection clears a previous Cisco UI/CLI session before this
// app records the local gateway and starts its own VPN connection.
func PrepareForNewConnection(cliPath string) {
	if err := ensureAgentService(); err != nil {
		log.Printf("Warning: could not ensure vpnagent service: %v", err)
	}
	if cliPath != "" {
		if err := Disconnect(cliPath); err != nil {
			log.Printf("Pre-connect disconnect ignored: %v", err)
		}
	}
	killConflictingProcesses()
}

// statusCheckWithTimeout 在独立 goroutine 中运行状态检查，确保不会阻塞调用方
func statusCheckWithTimeout(cliPath string, timeout time.Duration) (bool, error) {
	connected, output, err := statusWithOutput(cliPath, timeout)
	if connected {
		return true, nil
	}
	if hasVPNNetworkEvidence() {
		log.Printf("vpncli status unavailable, but VPN adapter is up: %s", strings.TrimSpace(output))
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func Connect(cliPath, server, username, password string) error {
	if err := ensureAgentService(); err != nil {
		return fmt.Errorf("Cisco VPN 后台服务 vpnagent 未运行：%w", err)
	}
	// 先杀掉 vpnui.exe，避免 "Another AnyConnect application is running" 错误
	killConflictingProcesses()

	cmd := exec.Command(cliPath, "-s")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start vpncli: %w", err)
	}

	// 发送连接命令
	commands := fmt.Sprintf("connect %s\n%s\n%s\ny\n", server, username, password)
	stdin.Write([]byte(commands))
	stdin.Close()

	// 不等待 cmd.Wait()，改为轮询 VPN 状态
	// vpncli -s 连接成功后不会退出，会保持会话
	// 我们通过独立调用 vpncli status 来检查是否连接成功

	// 先等几秒让连接建立
	time.Sleep(5 * time.Second)

	// 使用硬性截止时间，确保无论状态检查是否阻塞都能超时退出
	deadline := time.Now().Add(90 * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			log.Println("VPN connection timed out after 90s")
			return fmt.Errorf("VPN 连接超时（90秒），请检查网络或换个节点重试")
		}

		<-ticker.C
		// 使用带超时的状态检查，防止 vpncli status 挂起阻塞整个循环
		connected, err := statusCheckWithTimeout(cliPath, 8*time.Second)
		if err != nil {
			log.Printf("Status check error (will retry): %v", err)
			continue
		}
		if connected {
			// 连接成功，让 vpncli -s 在后台继续运行
			// 不要 Kill 它，否则 VPN 会断开
			go cmd.Wait() // 在后台回收进程
			return nil
		}
	}
}

func Disconnect(cliPath string) error {
	if cliPath == "" {
		return nil
	}
	if err := ensureAgentService(); err != nil {
		log.Printf("Warning: disconnect without running vpnagent service: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliPath, "disconnect")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		killConflictingProcesses()
		return fmt.Errorf("disconnect timed out")
	}
	if err != nil {
		return fmt.Errorf("disconnect failed: %s - %w", string(output), err)
	}
	return nil
}

func Status(cliPath string) (bool, error) {
	connected, _, err := statusWithOutput(cliPath, 10*time.Second)
	if connected {
		return true, nil
	}
	if hasVPNNetworkEvidence() {
		return true, nil
	}
	return false, err
}

func IsConnected(cliPath string, timeout time.Duration) bool {
	return ConnectionPresence(cliPath, timeout) == monitor.PresenceConnected
}

func ConnectionPresence(cliPath string, timeout time.Duration) monitor.VPNPresence {
	connected, output, err := statusWithOutput(cliPath, timeout)
	if connected {
		return monitor.PresenceConnected
	}
	if presence, ok := statusPresenceFromOutput(output); ok {
		if presence == monitor.PresenceDisconnected && hasVPNNetworkEvidence() {
			log.Printf("vpncli reports disconnected, but VPN network evidence is still present; preserving routes")
			return monitor.PresenceUnknown
		}
		return presence
	}
	if hasVPNNetworkEvidence() {
		if err != nil {
			log.Printf("vpncli status unavailable during monitor check, but VPN adapter is up: %v", err)
		}
		return monitor.PresenceConnected
	}
	if err != nil {
		log.Printf("VPN monitor status check failed: %v; output=%s", err, strings.TrimSpace(output))
		return monitor.PresenceUnknown
	}
	return monitor.PresenceDisconnected
}

func statusPresenceFromOutput(output string) (monitor.VPNPresence, bool) {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		idx := strings.Index(lower, "state:")
		if idx < 0 {
			continue
		}
		stateText := strings.TrimSpace(lower[idx+len("state:"):])
		fields := strings.Fields(stateText)
		if len(fields) == 0 {
			return monitor.PresenceUnknown, true
		}
		state := strings.Trim(fields[0], ".")
		switch state {
		case "connected":
			return monitor.PresenceConnected, true
		case "reconnecting", "connecting":
			return monitor.PresenceConnected, true
		case "disconnecting":
			return monitor.PresenceUnknown, true
		case "disconnected":
			return monitor.PresenceDisconnected, true
		default:
			return monitor.PresenceUnknown, true
		}
	}
	return monitor.PresenceUnknown, false
}

// statusWithOutput 返回连接状态和原始输出（用于诊断）
func statusWithOutput(cliPath string, timeout time.Duration) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliPath, "status")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if ctx.Err() == context.DeadlineExceeded {
		return false, outStr, fmt.Errorf("status check timed out")
	}
	if err != nil {
		return false, outStr, fmt.Errorf("status check failed: %w", err)
	}
	return strings.Contains(strings.ToLower(outStr), "state: connected"), outStr, nil
}
