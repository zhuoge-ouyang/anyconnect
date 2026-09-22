package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// Site 表示一个 VPN 站点
type Site struct {
	Name   string
	Server string
}

type LoginResult struct {
	SiteName string
	Server   string
	Username string
	Password string
	Remember bool
	OK       bool
}

type loginDialogGate struct {
	mu     sync.Mutex
	active bool
}

func (g *loginDialogGate) run(show func() LoginResult) LoginResult {
	g.mu.Lock()
	if g.active {
		g.mu.Unlock()
		return LoginResult{}
	}
	g.active = true
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		g.active = false
		g.mu.Unlock()
	}()
	return show()
}

var sharedLoginDialogGate loginDialogGate

func loginDefaultSite(sites []Site, preferredSite string) string {
	preferredSite = strings.TrimSpace(preferredSite)
	if preferredSite != "" {
		for _, site := range sites {
			if site.Name == preferredSite || strings.Contains(site.Name, preferredSite) {
				return site.Name
			}
		}
		return preferredSite
	}
	for _, site := range sites {
		if strings.Contains(site.Name, "深圳") {
			return site.Name
		}
	}
	for _, hint := range []string{"澳大利亚", "日本", "韩国", "泰国", "英国", "美国", "加拿大", "香港", "台湾"} {
		for _, site := range sites {
			if strings.Contains(site.Name, hint) {
				return site.Name
			}
		}
	}
	if len(sites) > 0 {
		return sites[0].Name
	}
	return ""
}

// ShowLoginDialog runs a compiled WinForms host. Credentials travel only through inherited pipes.
func ShowLoginDialog(sites []Site, preferredSite, savedUsername string, rememberDefault bool) LoginResult {
	return sharedLoginDialogGate.run(func() LoginResult {
		result, err := nativeLogin(sites, preferredSite, savedUsername, rememberDefault)
		if err != nil {
			hostError(err)
		}
		return result
	})
}
func nativeLogin(sites []Site, preferredSite, savedUsername string, rememberDefault bool) (LoginResult, error) {
	cmd, err := hostCommand("login", map[string]any{"sites": sites, "preferred_site": loginDefaultSite(sites, preferredSite), "saved_username": savedUsername, "remember": rememberDefault})
	if err != nil {
		return LoginResult{}, err
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	// Do not combine stderr with stdout: stdout contains the password.
	if err := cmd.Run(); err != nil {
		return LoginResult{}, err
	}
	var payload struct {
		SiteName string `json:"site_name"`
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
		OK       bool   `json:"ok"`
	}
	defer func() { clear(output.Bytes()) }()
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		return LoginResult{}, errors.New("原生登录窗口返回格式无效")
	}
	if !payload.OK {
		return LoginResult{}, nil
	}
	validSite := false
	for _, site := range sites {
		if site.Name == payload.SiteName && site.Server == payload.Server {
			validSite = true
			break
		}
	}
	if !validSite || strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.Password) == "" {
		return LoginResult{}, errors.New("原生登录窗口返回内容无效")
	}
	return LoginResult{SiteName: payload.SiteName, Server: payload.Server, Username: strings.TrimSpace(payload.Username), Password: payload.Password, Remember: payload.Remember, OK: true}, nil
}
