# 打包安装器

默认打包命令会生成一个干净的安装器，不会包含本机账号、密码、日志或已写入路由记录。

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1
```

生成文件：

```text
artifacts\AnyConnectSplitTunnelSetup.exe
```

## 打包 Cisco 客户端

当前程序依赖 Cisco AnyConnect 的 `vpncli.exe` 和 `vpnagent` 服务。只拷贝 `vpncli.exe` 不够，因为还需要 Cisco 的驱动和服务。

如果你有合法授权的 Cisco Core VPN 官方 MSI，可以把它一并打进安装器：

```powershell
PowerShell -ExecutionPolicy Bypass -File tools\package.ps1 -CiscoInstallerPath "D:\path\anyconnect-win-core-vpn-predeploy-k9.msi"
```

安装器会先安装本程序，再检测本机是否已有 Cisco AnyConnect / Cisco Secure Client；如果没有，会打开内置 Cisco MSI 的正常安装界面，不做静默安装。用户按 Cisco 安装向导完成后，本安装器会继续检测并启动主程序。

新版安装器支持：

- 选择安装位置
- 显示安装进度
- Cisco 客户端使用官方安装界面

## 隐私

发布配置来自 `configs\config.dist.yaml`，其中：

- `saved_username` 为空
- `auto_connect` 为 `false`
- 不包含密码
- 不包含 `bin\split-tunnel.log`
- 不包含 `data\applied_routes.json`

密码只会在使用者勾选“记住并下次自动连接”后写入他自己 Windows 用户的 Credential Manager。
