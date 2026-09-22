# 原生桌面界面宿主（1.0.5.0）

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

完整打包仍使用 `tools/package.ps1`，统一版本默认 1.0.5.0。原生宿主先构建、按所选签名模式签名并验证，再进入安装包和卸载文件清单；不把测试入口或测试账号编译进发布宿主。

## 1.0.5：首页选站点与实时网速

- 本轮采用网速设计中**第一个显示的图**，不是请求提交顺序：右侧独立浅绿色网速栏、上传/下载分开绘图，左侧站点下拉框。原先方案 3 的森林石桥资源和品牌图标复用。
- Go 快照增加 `sites`（仅站点名称）与 `connection_busy`，增加 `select_site` 命令。后端将名称与可信配置精确匹配，绝不接收界面指定的服务器地址。会话凭据保留在 Go 内存，不进入快照或命令文件。
- 先选站点，再确认；取消不产生命令，同站点不重连。后端对重复手动连接和选线操作互锁；验证后才断开、连接成功后保存常用站点。失败明确报错，不自动改用其他站点。托盘重新连接也改为登录确认后才断开。
- 原生宿主每秒读取 Go 保存的本地出口接口索引对应网卡的累计收发字节，用单调时间差计算实际速率；图表仅保留最近 60 秒。使用 [.NET 网卡统计](https://learn.microsoft.com/en-us/dotnet/api/system.net.networkinformation.ipinterfacestatistics) 与系统 [WinForms Chart](https://learn.microsoft.com/en-us/dotnet/api/system.windows.forms.datavisualization.charting.chart?view=netframework-4.8.1)，无第三方采集服务、测速下载或 NuGet 依赖。
- 这是**指定本地出口网卡**的流量，包含经该网卡的直连与 VPN 传输，不是 VPN 专属统计，也不是多网卡全机合计。主程序优先提供 IPv4 出口索引，仅有 IPv6 时使用其索引；无法识别接口不转用其他网卡。界面标明网卡名称。
- 使用十进制 B/s、KB/s、MB/s、GB/s；无流量为 0，首样本/计数器重置/接口变化/休眠长间隔重新建立基线，失败显示“暂不可用”并清除旧曲线。关闭管理台停止采样；不落盘传输历史，不采集网址或包内容。
- 可用 `ANYCONNECT_UI_REFERENCE` 配合 `ANYCONNECT_UI_EVIDENCE` 保存同输入对比；`ANYCONNECT_PACKAGED_HOST` 指向隔离解包宿主时可运行 `TestPackagedNativeHostReady`。这些都是测试环境变量，不向生产宿主添加假数据入口。

## 验证与明确边界

`go test ./internal/ui -count=1` 会在独立临时目录编译发布宿主和另一个测试程序，用真实 WinForms 控件验证模式、网址/IP/CIDR、重复/无效输入、状态读取失败与恢复、导航、登录节点/凭据/取消、白名单、推荐采用/恢复及说明窗口。C# 发出的命令再交给 Go 的 `dashboard.Command.Validate()` 检查。

进程集成测试验证原生管理台启动、最小化恢复、单实例聚焦、关闭和父进程退出清理，以及登录窗口与 Go 之间的真实管道回传。测试凭据为明确的虚构数据，不连接 VPN。可用 `ANYCONNECT_UI_EVIDENCE` 指定独立渲染证据目录。

此次**不是全面消除 PowerShell**：自启任务设置、安装/卸载快捷方式与进程检查、其他网络辅助操作仍可能使用 PowerShell。修改的是界面宿主；没有关闭防护、添加排除项、混淆或改动连接协议。

代码签名仍需真实证书。宿主迁移不能保证消除误报。没有对当前正在运行的 VPN 做升级、卸载或重连；真实新机联网、不同 DPI/多屏环境和完整安装-卸载-重启周期仍需实机验收。
