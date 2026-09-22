# 打包安装器

默认打包命令会生成一个干净的自包含安装器，不会包含本机账号、密码、日志或已写入路由记录。

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1
```

生成文件：

```text
artifacts\AnyConnectSplitTunnelSetup.exe
```

## 保持正在运行的分流守卫

如果当前正在使用分流守卫，指定独立构建目录，避免打包覆盖被运行进程占用的 `bin` 文件：

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1 -BuildDir artifacts\package-staging
```

这个模式只在 `artifacts\package-staging` 生成待打包主程序和 UI 资源，不会退出、重启或替换当前 `bin` 中正在运行的分流守卫。

## 打包 Cisco 客户端

默认安装包会内置 OpenConnect、sing-box、wintun.dll 和 `data\china_ip_list.txt`，朋友电脑安装后通常不需要额外安装 Cisco 客户端。

如果需要保留 Cisco 静态路由后端，程序仍支持检测本机 Cisco AnyConnect / Cisco Secure Client 的 `vpncli.exe` 和 `vpnagent` 服务。只拷贝 `vpncli.exe` 不够，因为还需要 Cisco 的驱动和服务。

如果你有合法授权的 Cisco Core VPN 官方 MSI，可以把它一并打进安装器：

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1 -CiscoInstallerPath "D:\path\anyconnect-win-core-vpn-predeploy-k9.msi"
```

安装器会先安装本程序，再检测本机是否已有 Cisco AnyConnect / Cisco Secure Client；如果没有，会打开内置 Cisco MSI 的正常安装界面，不做静默安装。用户按 Cisco 安装向导完成后，本安装器会继续检测并启动主程序。

新版安装器支持：

- 选择安装位置
- 显示安装进度
- 自带 OpenConnect + sing-box 连接环境时跳过 Cisco 安装
- Cisco 客户端使用官方安装界面

## 隐私

发布配置来自 `configs\config.dist.yaml`，其中：

- `saved_username` 为空
- `auto_connect` 为 `false`
- 不包含密码
- 不包含 `bin\split-tunnel.log`
- 不包含 `data\applied_routes.json`

密码只会在使用者勾选“记住并下次自动连接”后写入他自己 Windows 用户的 Credential Manager。

## 版本、签名与可追溯发布

`tools/package.ps1` 默认版本为 `1.0.4.0`，使用 `-Version` 设置四段 Windows 版本号。
主程序、原生界面宿主、独立卸载程序、安装器的版本在同一次构建中保持一致；资源源文件不会被改写。
`-Publisher` 只能填真实的发布者名称，不能冒用 Cisco 或其他厂商。当前没有确定发布者时保持为空。

未提供签名模式时是 **Unsigned 开发构建**，脚本明确警告，不代表杀毒软件放行。
正式 Authenticode 构建需要可信代码签名证书、可用私钥、Windows SDK SignTool 和证书服务商提供的 RFC 3161 时间戳地址：

```powershell
& .\tools\package.ps1 `
  -BuildDir artifacts\package-staging\signed-build `
  -OutputDir artifacts\package-staging\signed-output `
  -Version 1.0.4.0 -Publisher '你的真实发布者名称' `
  -SigningMode Authenticode `
  -CertificateThumbprint '<证书的40位指纹>' `
  -CertificateStore CurrentUser `
  -SignToolPath 'C:\实际WindowsSDK路径\signtool.exe' `
  -TimestampUrl 'https://你的证书服务商时间戳地址'
```

脚本不导入、不导出证书私钥，也不接收 PFX 密码。硬件证书可能要求持有人确认。
顺序为：构建主程序和原生 UI 宿主 → SHA-256 签名及时间戳 → 校验 → 准备 payload → 构建并签名独立卸载程序 → 构建安装器 → 签名及校验 → 发布。
原生命令非零退出、签名不可信、证书不匹配或缺时间戳均失败，不退回未签名包；失败候选仅留在构建目录，不发布到输出目录。
第三方程序保留原签名，不冒充本项目自产文件。

输出同时提供 `AnyConnectSplitTunnelSetup.exe.sha256` 和 `payload-manifest.json`。
清单记录每个 payload 文件的相对路径、大小、SHA-256 与 EXE/DLL 的签名状态，不写开发机绝对路径。
`-trimpath` 去除 Go 构建中的本机源码路径；哈希仅用于完整性核验，不证明安全。

签名不能保证消除病毒检测或 SmartScreen 提醒。仍需取得安全软件的具体检测名称、被报文件及哈希，确认来源后向对应厂商申诉；不得关闭防护、自动加白或混淆程序以绕过检测。
1.0.2.0 已按用户选定的方案 3 更新管理台，仍保留现有 WinForms 宿主；1.0.3.0 增加下述标准卸载能力，不修改网络权限或连接协议。

管理台新增 `desktop-dashboard-sidebar.png` 与 `app-brand.png` 发布资源；不会把设计图当作整张背景替代可操作控件。
完整网址／域名／IP/CIDR 可在首页统一输入，模式说明以实际分流规则为准；操作先写完整临时命令文件再重命名，状态读取失败会明确提示并禁用相关操作。
1.0.2.0 曾将界面脚本改为临时 UTF-8 BOM 文件启动；1.0.4.0 已由编译后的 C# WinForms 宿主替代登录、管理台、弹窗及安装向导，不再生成界面脚本。详见 [原生宿主迁移说明](native-ui-host.md)。构建机使用系统 C# 编译器，目标机要求 .NET Framework 4.8 或更高，缺失时明确提示，不回退脚本界面。迁移不代表杀毒误报已经解决。

相关验证：`powershell -NoProfile -File tools\test-release-support.ps1`。正向签名分支使用隔离模拟；真实证书签名必须另行验证。
参考：[Microsoft SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool)、[误报提交入口](https://www.microsoft.com/en-us/wdsi/filesubmission)。

## 标准卸载（1.0.3.0）

覆盖安装新版后，在 Windows“设置 → 应用 → 已安装的应用”或“控制面板 → 程序和功能”中查找“分流守卫”。也可以运行安装目录中的 `uninstall.exe`。只有安装文件复制完成且卸载程序存在时，安装器才注册标准入口：

`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\AnyConnectSplitTunnel`（64 位视图）。

- 显示名称、版本、图标、安装位置及带引号的卸载命令；不虚构发布者，不提供没有实现的修复/修改/静默卸载入口。
- 原生 Windows 确认框先询问是否卸载（默认否），然后选择是否保留配置、白名单和日志（默认是，可取消）。只有用户确认后才开始清理。
- 请先断开 VPN，在托盘选择退出，并关闭管理台。卸载程序只读检查进程与路由恢复记录；若同目录程序/组件仍运行、管理台未关闭，或存在非空/损坏的静态路由记录，则停止。不会强杀 VPN，也不会擅自修改路由表。
- 删除范围来自构建时编译进卸载程序的文件列表，而非可修改的磁盘 JSON；拒绝路径越界、盘符根目录、系统/用户公共目录、符号链接及目录联接。只删除明确文件及空子目录，不递归清空安装目录。
- 仅移除指向此安装目录主程序的自启任务、当前用户旧 Run 项和桌面/开始菜单快捷方式。同名但指向其他安装副本的项目保留。
- 独立安装的 Cisco、OpenConnect、系统驱动及其他未知文件不会删除。Windows 凭据 `AnyConnectSplitTunnel` 因可能跨安装副本共用而始终保留；如不再需要，请在当前用户的 Windows 凭据管理器中自行删除。
- 选择“不保留”只删除已知配置、IP 库及运行文件；未知文件和旧版未列入当前包的遗留文件仍保留，不承诺整目录清空。
- 卸载程序自身重命名为唯一 `.uninstall-<随机值>.pending-delete.exe` 后，使用 Windows 的重启清理机制移除，不自动重启。不会把固定的 `uninstall.exe` 路径加入重启删除队列，避免影响随后重装。安装根目录可能保留为空目录。
- 删除失败时停止，不静默忽略；注销前的失败保留卸载入口，注销失败会尝试恢复卸载程序原名。文件清理不是事务回滚，已删除的应用文件不会自动恢复，可用完整安装包修复。

隔离测试覆盖两种保留模式、未知文件、越界/联接拒绝、残留路由、真实测试进程占用、同名不同目标自启/快捷方式、失败重试及 HKCU 测试键注册。测试不卸载当前真实应用，不修改真实路由、共享凭据或系统重启删除队列；真实 UAC、系统应用列表、重启后的清理仍需在虚拟机/测试电脑验证。
