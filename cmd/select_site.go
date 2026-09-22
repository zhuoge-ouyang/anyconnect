package main

import (
	"fmt"

	"github.com/user/anyconnect-split/internal/ui"
)

// Resolve names against trusted configuration; never accept a server address from UI commands.
func selectConfiguredSite(sites []ui.Site, name string) (ui.Site, error) {
	for _, site := range sites {
		if site.Name == name && name != "" && site.Server != "" {
			return site, nil
		}
	}
	return ui.Site{}, fmt.Errorf("站点不在当前配置中，请重新选择")
}

// The UI has confirmed the change. Validation must still finish before disconnecting.
func switchSelectedSite(sites []ui.Site, name, current, username, password string, connected bool,
	disconnect func() error, connect func(ui.Site, string, string) error) error {
	site, err := selectConfiguredSite(sites, name)
	if err != nil {
		return err
	}
	if connected && site.Name == current {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("当前会话没有可用凭据，请使用重新连接登录")
	}
	if err := disconnect(); err != nil {
		return fmt.Errorf("未能完成断开，已停止切换：%w", err)
	}
	return connect(site, username, password)
}
