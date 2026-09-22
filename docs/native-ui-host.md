# 原生桌面界面宿主（1.0.4.0）

## 本轮范围

原来的登录窗口、管理台、白名单、智能选线结果、联系作者、充值说明及安装向导，现在由编译后的 C# WinForms 程序 `anyconnect-ui.exe` 承载。不再生成或执行用于这些窗口的 PowerShell 脚本，也未嵌入 PowerShell 解释器。

沿用已批准的方案 3 布局、图片和功能。C# WinForms 使用 .NET Framework，这是编译后的 Windows 桌面宿主，不是“整个程序改写为 C/C++ 机器码”，也不是权限降级或网络后端重写。

Go 主程序继续负责 VPN、路由、配置、Credential Manager 和托盘；状态 JSON、命令 JSON 及动作名称保持一致。命令仍通过“临时文件写完整后改名”提交，状态无效时禁用操作，提交错误会显示，不回退 PowerShell。

## 凭据与进程生命周期

- Go 通过子进程标准输入发送登录参数，C# 通过标准输出返回结果。密码不放命令行、不写临时 JSON、不与错误日志合并。Credential Manager 的保存策略未改。
- Go 校验返回的节点名称和服务器与提供的节点列表一致；取消登录不产生成功结果。
- 管理台建立启动就绪握手；缺宿主、启动失败或超时明确报错，不使用备用界面。
- 同一主进程只保留一个管理台，重复打开会恢复并聚焦原窗口。
- 宿主持有父进程句柄，父进程结束后自动退出，避免遗留后台窗口。
- 安装器只将已编译宿主释放到临时目录；向导关闭后清理。现有安装后端与升级步骤不变。

## 构建与运行要求

支持验证目标为 Windows x64、Microsoft .NET Framework 4.8 或更高。完整安装向导会检查运行库，不自动下载、不静默换回脚本。Windows 11 和较新的 Windows 10 通常已包含该运行库，具体见 [Microsoft 版本说明](https://learn.microsoft.com/en-us/dotnet/framework/get-started/system-requirements)。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File tools/build-ui-host.ps1 -OutputPath bin/anyconnect-ui.exe
```

构建使用系统 .NET Framework C# 编译器，无新 Go/NuGet/WebView2 依赖。开发运行也必须将宿主放在 Go 主程序旁边，并准备 `app.ico` 和 `ui-assets`；单独 `go build` 不会自动构建 C# 宿主。

完整打包仍使用 `tools/package.ps1`，统一版本默认 1.0.4.0。原生宿主先构建、按所选签名模式签名并验证，再进入安装包和卸载文件清单；不把测试入口或测试账号编译进发布宿主。

## 验证与明确边界

`go test ./internal/ui -count=1` 会在独立临时目录编译发布宿主和另一个测试程序，用真实 WinForms 控件验证模式、网址/IP/CIDR、重复/无效输入、状态读取失败与恢复、导航、登录节点/凭据/取消、白名单、推荐采用/恢复及说明窗口。C# 发出的命令再交给 Go 的 `dashboard.Command.Validate()` 检查。

进程集成测试验证原生管理台启动、最小化恢复、单实例聚焦、关闭和父进程退出清理，以及登录窗口与 Go 之间的真实管道回传。测试凭据为明确的虚构数据，不连接 VPN。可用 `ANYCONNECT_UI_EVIDENCE` 指定独立渲染证据目录。

此次**不是全面消除 PowerShell**：自启任务设置、安装/卸载快捷方式与进程检查、其他网络辅助操作仍可能使用 PowerShell。修改的是界面宿主；没有关闭防护、添加排除项、混淆或改动连接协议。

代码签名仍需真实证书。宿主迁移不能保证消除误报。没有对当前正在运行的 VPN 做升级、卸载或重连；真实新机联网、不同 DPI/多屏环境和完整安装-卸载-重启周期仍需实机验收。
