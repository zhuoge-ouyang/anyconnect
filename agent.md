# Agent Guide

## 项目定位

这是一个 Windows 桌面端 VPN 分流工具，面向 Cisco AnyConnect / Cisco Secure Client 使用场景。程序以系统托盘应用运行，负责连接 VPN、维护分流状态，并让国内 IP / 指定国内域名走本地网关，其他流量继续走 VPN。

当前实现同时支持两类后端：

- `openconnect_tun`: 优先路径。使用 OpenConnect 建立 AnyConnect 会话，再用 sing-box TUN 做流量分流。
- `cisco_static`: 回退路径。使用 Cisco `vpncli.exe` 建立连接，并向 Windows 路由表批量写入国内 CIDR 路由。
- `auto`: 默认模式。优先尝试内置 OpenConnect + sing-box，必要时回退 Cisco 静态路由。

这是强 Windows 依赖项目，很多功能需要管理员权限、真实网络适配器、路由表、托盘环境和外部二进制配合。不要把纯单元测试能通过误判为完整运行验证完成。

## 技术栈

- Go module: `github.com/user/anyconnect-split`
- Go version: `1.26.3`
- GUI / tray: `github.com/getlantern/systray`，通过 `replace` 指向 `./third_party/systray`
- Windows API: `golang.org/x/sys/windows`
- 配置格式: YAML via `gopkg.in/yaml.v3`
- 打包资源: `go-winres` 生成 `.syso`
- 外部运行依赖: `openconnect.exe`、`sing-box.exe`、可选 Cisco `vpncli.exe`

## 常用命令

```powershell
go test ./internal/... ./cmd/... -v
```

```powershell
go build -ldflags="-s -w" -o bin/anyconnect-split.exe ./cmd/
```

```powershell
go run ./tools/icon_gen
cd cmd; go-winres make
cd ..\cmd\installer; go-winres make
```

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1
```

`Makefile` 也提供 `icons`、`build`、`run`、`test`、`package`，但在 Windows PowerShell 下优先使用上面的显式命令，便于看到失败位置。

## 目录导览

- `cmd/main.go`: 主程序入口；管理员提权、单实例、托盘启动、连接编排、路由刷新、ChatGPT/Codex 智能选线。
- `cmd/installer/main.go`: Windows 安装器；释放 payload、创建快捷方式、检测/安装 Cisco 客户端、启动主程序。
- `internal/config`: 配置结构、默认值、配置归一化、Cisco `vpncli.exe` 自动探测。
- `internal/vpn`: Cisco `vpncli.exe` 连接、断开、状态检测和冲突进程清理。
- `internal/tun`: OpenConnect + sing-box TUN 会话管理、配置生成、TUN 进程清理。
- `internal/route`: Windows `netsh` / `route print` 路由批处理、CIDR 汇总、残留路由恢复。
- `internal/monitor`: VPN 状态机、Cisco 适配器/默认路由检测、IPv4/IPv6 默认网关检测。
- `internal/ipdb`: APNIC CN IPv4/IPv6 CIDR 下载、解析、缓存。
- `internal/domainroute`: 将配置里的国内域名例外解析成主机路由。
- `internal/tray`: 托盘菜单、状态文字、图标状态、托盘点击行为。
- `internal/ui`: 登录窗口和 PowerShell WinForms 管理台。
- `internal/dashboard`: 管理台快照文件和命令文件的读写/调度。
- `internal/credential`: Windows Credential Manager 凭据保存。
- `internal/autostart`: Windows 开机自启配置。
- `tools/package.ps1`: 构建自包含安装器并准备 installer payload。
- `tools/icon_gen`: 生成图标和托盘图标数据。
- `tools/ipdb_seed`: 为安装包准备 `data/china_ip_list.txt`。
- `configs/config.yaml`: 本机开发/运行配置，可能包含个人偏好。
- `configs/config.dist.yaml`: 发布配置模板，打包时复制为安装包内的 `configs/config.yaml`。
- `data/china_ip_list.txt`: 中国 IP 段种子数据。
- `docs/packaging.md`: 打包行为和隐私说明。

## 开发原则

- 优先保持 Windows 行为真实可靠。涉及管理员权限、路由表、服务、托盘、快捷方式、Credential Manager 的变更，应考虑真实用户机器上的失败路径。
- 默认不要改 `configs/config.yaml` 中的个人运行偏好，发布模板应改 `configs/config.dist.yaml`。
- 不要把密码、用户名、日志、`data/applied_routes.json` 或本机安装路径写入发布产物。
- 静态路由和 TUN 后端都要维护。改连接流程时同时检查 `internal/tun`、`internal/vpn`、`internal/route`、`cmd/main.go` 的交互。
- 路由写入/删除必须可取消、可恢复，并能处理“已存在”“不存在”等 Windows 本地化错误文本。
- 托盘和管理台状态要保持一致：托盘状态通过 `StatusListener` 写入 dashboard snapshot，管理台通过 command JSON 请求动作。
- UI 文案以中文为主。面向用户的错误提示不要暴露过多内部进程名，除非对排障有帮助。
- `third_party/systray` 是本项目替换依赖的一部分；改动它时要格外小心，避免无意同步上游或大范围格式化。

## 配置与隐私

发布配置来自 `configs/config.dist.yaml`。它必须保持：

- `saved_username` 为空。
- `auto_connect` 为 `false`。
- 不包含密码。
- 不包含本机日志。
- 不包含运行时路由记录。

密码只应通过 Windows Credential Manager 保存，相关逻辑在 `internal/credential`。不要把密码落到 YAML、日志或安装包 payload。

## 测试策略

优先运行：

```powershell
go test ./internal/... ./cmd/... -v
```

对路由、TUN、安装器、托盘、图标相关改动，至少补或更新相邻包测试。涉及真实系统行为时，单元测试之外还需要人工验证：

- 管理员身份启动。
- 托盘图标和菜单可见。
- OpenConnect + sing-box 工具存在时能进入 TUN 后端。
- 缺少 TUN 工具时能按配置回退 Cisco 静态路由。
- VPN 断开和程序退出会清理路由。
- `split-tunnel.log` 中没有密码或敏感信息。

## 打包注意事项

`tools/package.ps1` 会：

- 运行 `tools/icon_gen`。
- 调用 `go-winres make` 生成主程序和安装器资源。
- 构建 `bin/anyconnect-split.exe`。
- 准备 `cmd/installer/payload`。
- 拷贝 `configs/config.dist.yaml` 为 payload 的 `configs/config.yaml`。
- 尝试打包 `C:\Program Files\OpenConnect` 和真实 `sing-box.exe`。
- 构建 `artifacts\AnyConnectSplitTunnelSetup.exe`。
- 默认清理 payload，除非传入 `-KeepPayload`。

如果本机没有 OpenConnect 或 sing-box，默认打包会失败。只有在明确需要非自包含安装包时才使用 `-AllowMissingBundledTools`。

## 高风险区域

- `cmd/main.go`: 连接状态、后端选择、路由刷新、断开清理都集中在这里，小改动也可能影响实际网络。
- `internal/tun/session.go`: 会启动/杀掉 OpenConnect 和 sing-box 进程，并生成运行时脚本和 sing-box 配置。
- `internal/route/manager.go`: 会操作 Windows 路由表。不要在测试里真实写系统路由，除非测试明确隔离并由用户同意。
- `tools/package.ps1`: 会复制本地二进制和 payload。改动前确认不会把本机隐私文件带进安装器。
- `cmd/installer/main.go`: 安装器以管理员身份运行，并会写入 Program Files、桌面、开始菜单。

## 飞书文档偏好

以后对飞书文档（Feishu/Lark Docs）的任何修改，都不要直接覆盖或删除原文；需要用紫色文字颜色和删除线标注修改内容，让改动以可审阅的形式呈现。
