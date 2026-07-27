# Split Mode 与国外白名单重构设计

## 背景

当前 `anyconnect` 采用“默认走 VPN，国内 IP/域名例外直连”的分流策略。随着使用场景变化，需要支持相反的“默认国内直连，仅国外白名单走 VPN”模式，并保留原模式作为可切换选项。

## 目标

1. 新增 `split_mode` 模式开关：
   - `domestic_direct`（默认）：所有流量直连，仅国外白名单域名/IP 走 VPN。
   - `foreign_direct`：所有流量走 VPN，仅国内白名单域名/CIDR 直连（兼容现有行为）。
2. 提供国外白名单默认集合，覆盖 Pornhub、YouTube、Google、Gemini、OpenAI/Codex、Claude、GitHub/Copilot、GitLab 等站点及其常见 CDN。
3. 提供三处“快速添加国外站点”入口：托盘菜单、Dashboard、配置文件。
4. 保证 Codex（OpenAI Codex / ChatGPT / API）在国内直连模式下可正常连通。

## 配置变更

### 新增字段

```yaml
# 分流模式
split_mode: "domestic_direct"   # domestic_direct | foreign_direct

# 国内直连模式下的国外白名单
foreign_domain_exceptions:
  - "pornhub.com"
  - "youtube.com"
  - "ytimg.com"
  - "google.com"
  - "googleapis.com"
  - "googlevideo.com"
  - "gstatic.com"
  - "ggpht.com"
  - "gemini.google.com"
  - "aistudio.google.com"
  - "generativelanguage.googleapis.com"
  - "openai.com"
  - "chatgpt.com"
  - "api.openai.com"
  - "auth.openai.com"
  - "platform.openai.com"
  - "cdn.openai.com"
  - "files.oaiusercontent.com"
  - "chat.com"
  - "oaistatic.com"
  - "oaiusercontent.com"
  - "anthropic.com"
  - "claude.ai"
  - "api.anthropic.com"
  - "github.com"
  - "githubusercontent.com"
  - "githubassets.com"
  - "githubcopilot.com"
  - "copilot.microsoft.com"
  - "gitlab.com"
  - "gitlab-static.net"

foreign_ip_exceptions: []       # 国内直连模式下走 VPN 的国外 IP/CIDR
```

### 保留字段

```yaml
domestic_domain_exceptions: [...]   # 国外直连模式下的国内直连域名
ipv6_split_enabled: false
update_interval_days: 7
```

### 兼容性

- 老配置缺少 `split_mode` 时，初始化为 `foreign_direct`，保持原行为不变。
- 新安装或全新配置默认使用 `domestic_direct`。
- 当 `split_mode == domestic_direct` 且 `foreign_domain_exceptions` 为空时，记录 warning 日志提醒用户。

## 路由逻辑变更

### TUN / sing-box 模式

`internal/tun/session.go` 中的 `BuildSingBoxConfig()` 根据 `split_mode` 动态生成路由规则。

#### domestic_direct 模式

1. DNS 劫持规则（保持不变）。
2. 私网 IP 直连（保持不变）。
3. 受保护 CIDR 直连（VPN 服务器 IP 等，保持不变）。
4. **国外白名单域名/IP → 走 VPN outbound。**
5. 其余全部走 `direct-local`。

#### foreign_direct 模式

保持现有逻辑：
1. DNS 劫持规则。
2. 私网 IP 直连。
3. 受保护 CIDR 直连。
4. 国内 CIDR 直连。
5. 国内域名直连。
6. 其余走 VPN。

### Cisco 静态路由回退

`internal/route/manager.go` 根据模式决定写入 Windows 路由表的条目：

- `domestic_direct`：仅将 `foreign_domain_exceptions` 解析后的 IP/CIDR 与 `foreign_ip_exceptions` 加入 VPN 路由表。
- `foreign_direct`：保持现有逻辑，加入国内 CIDR 与 `domestic_domain_exceptions` 解析后的 IP。

## UI / 入口

### 托盘菜单

在 `internal/tray/tray.go` 新增：

- `模式` 子菜单：
  - `国内直连模式`
  - `国外直连模式`
  - 当前模式前显示勾选标记。
- `快速添加国外域名...`
- `快速添加国外 IP/CIDR...`

点击后弹出输入框（使用 Windows `InputBox` 或 PowerShell 弹窗），校验后写入配置并刷新路由。

### Dashboard

在 `internal/ui/dashboard.go` 与 `internal/dashboard/controller.go` 新增：

- 模式切换下拉框（`国内直连 / 国外直连`）。
- 输入框 + `添加国外域名` 按钮。
- 输入框 + `添加国外 IP/CIDR` 按钮。
- 当前白名单滚动列表（只读展示）。

对应新增 dashboard command：`toggle_mode`、`add_foreign_domain`、`add_foreign_ip`。

### 配置文件入口

用户可直接编辑 `configs/config.yaml` 中的 `foreign_domain_exceptions` / `foreign_ip_exceptions` 列表，重启或手动刷新后生效。

## 快速添加流程

1. 用户输入域名或 IP/CIDR。
2. 格式校验：
   - 域名：去除协议头、路径、端口号；至少包含一个点。
   - IP/CIDR：支持 IPv4、IPv6 单地址及 CIDR。
3. 去重：若已存在则跳过并提示。
4. 写入对应配置项，调用 `config.Save()` 持久化。
5. 若 VPN 已连接：
   - TUN 模式：调用 `tunSession.Refresh()` 重新生成并热重载 sing-box 配置。
   - Cisco 模式：调用 route manager 增量添加路由。
6. 更新 Dashboard 状态快照。

## 默认国外白名单

覆盖范围：

| 类别 | 域名 |
|------|------|
| 视频 | `pornhub.com`、`youtube.com`、`ytimg.com` |
| Google | `google.com`、`googleapis.com`、`googlevideo.com`、`gstatic.com`、`ggpht.com` |
| Gemini | `gemini.google.com`、`aistudio.google.com`、`generativelanguage.googleapis.com` |
| OpenAI / Codex / ChatGPT | `openai.com`、`chatgpt.com`、`api.openai.com`、`auth.openai.com`、`platform.openai.com`、`cdn.openai.com`、`files.oaiusercontent.com`、`chat.com`、`oaistatic.com`、`oaiusercontent.com` |
| Claude / Anthropic | `anthropic.com`、`claude.ai`、`api.anthropic.com` |
| GitHub / Copilot | `github.com`、`githubusercontent.com`、`githubassets.com`、`githubcopilot.com`、`copilot.microsoft.com` |
| GitLab | `gitlab.com`、`gitlab-static.net` |

> 注：默认采用域名后缀匹配（sing-box `domain_suffix`），可覆盖主域及子域。

## 测试与验证

1. 配置加载：验证 `split_mode` 缺失时正确回退为 `foreign_direct`。
2. TUN 配置生成：两种模式下 sing-box JSON 路由规则顺序正确。
3. Cisco 静态路由：两种模式下写入 Windows 路由表的条目符合预期。
4. 快速添加：
   - 域名校验与去重。
   - IP/CIDR 校验与去重。
   - 添加后立即刷新路由。
5. 连通性：
   - `domestic_direct` 模式下，访问 `chatgpt.com`、`github.com`、`claude.ai` 等站点走 VPN。
   - 访问国内站点（如 `qq.com`、`baidu.com`）不走 VPN。

## 文件影响

- `internal/config/config.go`：新增字段、默认值、迁移逻辑。
- `configs/config.dist.yaml`：新增默认配置模板。
- `configs/config.yaml`：运行时配置更新。
- `internal/tun/session.go`：`BuildSingBoxConfig()` 按模式生成规则。
- `internal/route/manager.go`：按模式选择写入路由表的条目。
- `internal/tray/tray.go`：新增菜单项与处理逻辑。
- `cmd/main.go`：新增托盘/Dashboard 动作处理函数。
- `internal/ui/dashboard.go`、`internal/dashboard/controller.go`：新增 UI 控件与命令类型。
