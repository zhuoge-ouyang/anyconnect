# 双模式分流与国外白名单 — 手动验证清单

> 适用版本：`split_mode` 双模式 + 国外白名单快速添加入口改造完成后。
> 自动化验证已通过：`go build` / `go vet` / `go test ./...` 全绿，dist 配置解析校验通过，domestic_direct sing-box JSON 结构校验通过（`route.final=direct-local`，国外白名单 → `vpn-direct`）。

下面是需要真实 Windows + VPN 环境人工执行的验证项。每项请勾选并记录结果。

---

## 0. 构建与部署

- [ ] 在 Windows 上执行 `go build -o bin/anyconnect-split.exe ./cmd`，生成唯一官方可执行文件且无报错。
- [ ] 确认 `openconnect.exe`、`sing-box.exe`、`vpncli.exe`（Cisco 客户端）已在可检测路径或 `configs/config.yaml` 中正确配置。
- [ ] 首次启动后，确认 `configs/config.yaml` 被生成，且包含 `split_mode: "domestic_direct"` 与 `foreign_domain_exceptions` 列表。

---

## 1. 配置兼容性（旧用户不破坏）

- [ ] **旧配置回退**：用一个不含 `split_mode` 键的旧 `config.yaml` 启动，确认日志/行为显示为 `foreign_direct`（默认全部走 VPN，国内白名单直连），与改造前一致。
- [ ] **新安装默认**：删除 `configs/config.yaml` 后启动，确认生成的配置 `split_mode` 为 `domestic_direct`。
- [ ] **显式 foreign_direct**：手动写 `split_mode: "foreign_direct"`，确认行为同旧版（全 VPN + 国内白名单直连）。

---

## 2. TUN 模式 — 国内直连模式（默认，核心场景）

前提：`traffic_backend: auto` 且 TUN 工具可用，`split_mode: domestic_direct`，`split_tunnel_enabled: true`。

- [ ] 连接 VPN 后，托盘显示「TUN 分流已启用」。
- [ ] **默认直连**：访问 `https://www.baidu.com`、`https://www.qq.com`，确认走本地直连（可对比 IP 归属或延迟，应非 VPN 出口）。
- [ ] **国外白名单走 VPN**：访问 `https://chatgpt.com`、`https://github.com`、`https://claude.ai`、`https://gemini.google.com`，确认走 VPN 出口（IP 归属为海外节点）。
- [ ] **YouTube/Google**：访问 `https://www.youtube.com`、`https://www.google.com`，确认可正常打开且走 VPN。
- [ ] **DNS 正确**：`nslookup chatgpt.com` 返回海外解析（经 `8.8.8.8`）；`nslookup baidu.com` 返回国内解析（经 `114.114.114.114`）。可检查 `data/sing-box-tun.json` 中 `dns.final=dns-local`、国外域名规则 → `dns-vpn`。
- [ ] **Pornhub**：访问 `https://www.pornhub.com`，确认走 VPN 且可打开。
- [ ] 打开 `data/sing-box-tun.json`，确认 `route.final` 为 `direct-local`，国外 CIDR/domain 规则 `outbound` 为 `vpn-direct`。

---

## 3. TUN 模式 — 国外直连模式（兼容旧行为）

前提：`split_mode: foreign_direct`，`split_tunnel_enabled: true`。

- [ ] 连接 VPN 后访问 `https://www.baidu.com` 走直连，访问 `https://www.google.com` 走 VPN。
- [ ] `data/sing-box-tun.json` 中 `route.final` 为 `vpn-direct`，国内 CIDR/domain 规则 `outbound` 为 `direct-local`，`dns.final=dns-vpn`。

---

## 4. Cisco 静态路由回退 — 国内直连模式

前提：TUN 工具不可用，回退到 `cisco_static`；`split_mode: domestic_direct`。

- [ ] 连接后，托盘显示「已回退静态路由（N 条路由）」。
- [ ] **默认直连**：访问 `https://www.baidu.com` 走本地直连。
- [ ] **国外白名单走 VPN**：访问 `https://chatgpt.com`、`https://github.com` 走 VPN。
- [ ] 在管理员 PowerShell 执行 `route print -4`，确认存在 `0.0.0.0/1`、`128.0.0.0/1` 指向本地网关（metric 5），以及国外白名单 IP 的 `/32` 指向 VPN 网关。
- [ ] 断开 VPN 后，确认 `0.0.0.0/1`、`128.0.0.0/1` 与 VPN `/32` 路由均被清理（`route print` 无残留）。
- [ ] VPN 服务器 IP 仍走本地直连（隧道本身不中断）。

---

## 5. 快速添加入口 — 托盘菜单

前提：VPN 已连接，`domestic_direct` 模式。

- [ ] 托盘右键 →「添加国外域名…」→ 输入 `anthropic.com` → 确认弹窗「已加入白名单并保存」。
- [ ] 立即访问 `https://www.anthropic.com`，确认已走 VPN（热刷新无需断开重连）。
- [ ] 托盘右键 →「添加国外 IP/CIDR…」→ 输入 `104.18.0.0/16` → 确认弹窗。
- [ ] **去重**：再次添加 `anthropic.com`，确认提示「已存在，未做更改」。
- [ ] **格式校验**：输入 `invalid`（无点）或 `not_a_cidr`，确认提示「格式无效」。
- [ ] 重启程序后，确认 `configs/config.yaml` 中新增条目仍在。

---

## 6. 快速添加入口 — Dashboard 面板

- [ ] 打开 Dashboard，确认「国外白名单」区有两个输入框 + 「添加域名」「添加 IP/CIDR」按钮。
- [ ] 在域名框输入 `gitlab.com` → 点「添加域名」，确认「已发送」提示。
- [ ] 访问 `https://gitlab.com` 确认走 VPN。
- [ ] 在 IP 框输入 `1.2.3.0/24` → 点「添加 IP/CIDR」，确认生效。
- [ ] 输入框为空时点按钮无反应（前端空值拦截）。

---

## 7. 快速添加入口 — 配置文件直编

- [ ] 手动编辑 `configs/config.yaml`，在 `foreign_domain_exceptions` 下加 `- "example.com"`。
- [ ] 重启程序并连接，访问该域名确认走 VPN。

---

## 8. 持久化与热刷新

- [ ] 任何一处添加后，`configs/config.yaml` 立即包含新条目（TUN 模式通过 `tunSession.SetForeignWhitelist` + `cfg.Save`）。
- [ ] TUN 模式添加域名后无需重连，sing-box 配置热重载生效（日志可见 `OpenConnect TUN routes refreshed`）。
- [ ] 添加 IP/CIDR 在 Cisco 静态模式下增量写入路由表（`route print` 可见新 `/32`）。

---

## 9. Codex 连通性专项（核心需求）

- [ ] `domestic_direct` + TUN：访问 `https://chatgpt.com`、`https://api.openai.com`、`https://platform.openai.com` 均走 VPN 且可正常使用 Codex/ChatGPT。
- [ ] `domestic_direct` + TUN：GitHub Copilot 相关（`github.com`、`githubcopilot.com`、`copilot.microsoft.com`）走 VPN。
- [ ] `domestic_direct` + Cisco 静态：OpenAI/Codex 站点走 VPN。
- [ ] 如某 Codex 子域名不通，用「添加国外域名」加入后立即恢复。

---

## 10. 断开/重连/退出清理

- [ ] 断开 VPN：TUN 模式 sing-box 进程退出，`data/sing-box-tun.json` 不再接管路由。
- [ ] 断开 VPN：Cisco 静态模式下 `0.0.0.0/1`、`128.0.0.0/1` 与 VPN `/32` 路由全部清理。
- [ ] 异常退出后重启：`CleanupStaleRoutes` 清理 `applied_routes.json` 与 `applied_vpn_routes.json` 中的残留路由。
- [ ] 重连后分流规则重新应用。

---

## 11. 模式切换稳定性（可选）

- [ ] 在双击托盘图标打开的管理台中切换“国内直连优先 / 国外 VPN 优先”，确认配置、界面和实际路由同步变化。
- [ ] 在右键托盘的“分流模式”子菜单中切换两种模式，确认当前模式勾选正确，点击已选模式不会重复刷新。
- [ ] VPN 已连接时切换模式，确认 OpenConnect 登录保持连接，仅 TUN/静态路由规则刷新；未连接时确认下次连接采用已保存模式。
- [ ] `foreign_direct` 下 `foreign_domain_exceptions` 不参与路由（仅 `domestic_direct` 生效）。

---

## 12. 回滚

- [ ] 如需回退旧行为：设置 `split_mode: "foreign_direct"` 即可，无需改代码。
- [ ] 国外白名单字段保留不影响 `foreign_direct` 模式运行。
