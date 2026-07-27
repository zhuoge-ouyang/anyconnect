# Windows 与 Android 联系作者二维码实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Windows 与 Android 登录状态常驻显示微信二维码，并在两端主界面右下角提供可弹出大二维码的“联系作者”按钮。

**Architecture:** 保留用户提供二维码的原始像素，以确定性裁切生成同源 PNG，分别进入 Windows `ui-assets` 与 Android `drawable-nodpi`。Windows 继续使用 PowerShell WinForms，Android 继续使用单 Activity 的代码式 View；两端仅根据登录/连接状态切换联系区域，不改 VPN 业务链路。

**Tech Stack:** Go 1.x、PowerShell WinForms、Android Kotlin、Android SDK 35、JUnit 4、Gradle。

## Global Constraints

- 唯一源图为 `C:\Users\MSI\Desktop\20260717-022449.jpg`；只裁切和缩放，不重绘二维码。
- 登录状态在“联系作者”文字下常驻显示二维码。
- 主界面不常驻二维码，只在右下角显示“联系作者”按钮并弹出二维码。
- 保留 Windows 现有“充值说明”和 Android 复制微信号能力。
- 不处理 MuMu、应用图标、其他 APK 布局、托盘联系入口、VPN 或路由逻辑。
- 不增加依赖，不增加 fallback，不提交或推送 Git。

---

### Task 1: 生成并接入同源二维码资源

**Files:**
- Create: `internal/ui/assets/wechat-contact-qr.png`
- Create: `android/app/src/main/res/drawable-nodpi/wechat_contact_qr.png`
- Modify: `Makefile`
- Modify: `tools/package.ps1`

**Interfaces:**
- Consumes: 用户提供的 792×1248 JPEG。
- Produces: 两份像素一致的 632×632 PNG；Windows 运行时路径为 `ui-assets\wechat-contact-qr.png`，Android 资源 ID 为 `R.drawable.wechat_contact_qr`。

- [ ] **Step 1: 确认裁切范围只包含完整二维码**

使用源矩形 `X=80, Y=360, Width=632, Height=632`，覆盖二维码三个定位角和四周留白，不包含头像、地区及底部说明。

- [ ] **Step 2: 确定性裁切并复制 Android 资源**

运行 PowerShell `System.Drawing.Bitmap.Clone(Rectangle(80,360,632,632), PixelFormat)`，PNG 保存到 Windows 资源路径，再原样复制到 Android 资源路径。不得使用生成式图像编辑。

- [ ] **Step 3: 验证资源尺寸和哈希**

运行：

```powershell
Add-Type -AssemblyName System.Drawing
$a = [System.Drawing.Image]::FromFile('internal\ui\assets\wechat-contact-qr.png')
$b = [System.Drawing.Image]::FromFile('android\app\src\main\res\drawable-nodpi\wechat_contact_qr.png')
"$($a.Width)x$($a.Height)"; "$($b.Width)x$($b.Height)"
$a.Dispose(); $b.Dispose()
Get-FileHash internal\ui\assets\wechat-contact-qr.png,android\app\src\main\res\drawable-nodpi\wechat_contact_qr.png
```

Expected: 两张图片均为 `632x632`，SHA256 相同。

- [ ] **Step 4: 更新 Windows 复制和打包断言**

在 `Makefile` 的 `build` 资源复制命令中增加 `wechat-contact-qr.png`。在 `tools/package.ps1` 的 `Copy-UiAssetsToDir` 增加：

```powershell
Copy-Item -LiteralPath (Join-Path $source "wechat-contact-qr.png") -Destination (Join-Path $TargetDir "wechat-contact-qr.png") -Force
```

在 `Assert-SelfContainedPayload` 增加：

```powershell
Assert-FileExists -Path (Join-Path $payloadDir "ui-assets\wechat-contact-qr.png") -Message "Payload is missing the contact QR code."
```

---

### Task 2: Windows 登录窗口常驻二维码

**Files:**
- Modify: `internal/ui/login_test.go`
- Modify: `internal/ui/login.go`

**Interfaces:**
- Consumes: `loginContactSectionScript(qrPath string) string` 的二维码绝对路径。
- Produces: 登录窗口中的 `$contactPanel`、`$contactTitle`、`$contactHint`、`$contactQr`；加载失败时显示 `$contactError`。

- [ ] **Step 1: 写失败测试**

在 `internal/ui/login_test.go` 增加测试，调用 `loginContactSectionScript("C:\\Program Files\\AnyConnect Split Tunnel\\ui-assets\\wechat-contact-qr.png")`，断言脚本包含：

```go
for _, want := range []string{
    "联系作者",
    "微信扫码添加作者",
    "PictureBox",
    "wechat-contact-qr.png",
    "二维码加载失败",
} {
    if !strings.Contains(script, want) { t.Fatalf("login contact script missing %q", want) }
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./internal/ui -run TestLoginContactSectionScript -v`

Expected: FAIL，原因是 `loginContactSectionScript` 尚未定义。

- [ ] **Step 3: 实现最小 WinForms 联系区域**

在 `internal/ui/login.go` 中：

- 从 EXE 同级目录解析 `ui-assets\wechat-contact-qr.png`。
- 新增 `loginContactSectionScript(qrPath string) string`，使用 `PictureBoxSizeMode.Zoom` 等比显示 190×190 二维码。
- 将 `$form` 调整为约 620×850、`$shell` 调整为约 544×760，在原有充值说明按钮下方插入联系区域。
- `Test-Path` 为 false 或 `Image.FromFile` 抛错时显示“二维码加载失败”，不使用替代图片。
- 将返回脚本拼入 `$form.ShowDialog()` 之前，不改变登录结果 JSON。

- [ ] **Step 4: 运行测试确认 GREEN**

Run: `go test ./internal/ui -run 'TestLogin(DefaultSite|ContactSectionScript)' -v`

Expected: PASS。

---

### Task 3: Windows 管理台右下角联系按钮和二维码弹窗

**Files:**
- Modify: `internal/ui/dashboard_test.go`
- Modify: `internal/ui/dashboard.go`

**Interfaces:**
- Consumes: `$contactQrPath = Join-Path ([System.IO.Path]::GetDirectoryName($iconPath)) 'ui-assets\wechat-contact-qr.png'`。
- Produces: `Show-ContactDialog($owner)` 和右下角 `$btnContact`；不再发送 `contact_author` 命令。

- [ ] **Step 1: 写失败测试**

在 `internal/ui/dashboard_test.go` 增加 `TestDashboardContactButtonOpensQRCodeDialog`，断言 `dashboardScript(...)` 包含：

```go
for _, want := range []string{
    "Show-ContactDialog",
    "wechat-contact-qr.png",
    "微信扫码添加作者",
    "$btnContact.Add_Click({ Show-ContactDialog $form })",
} { /* strings.Contains assertions */ }
```

并断言脚本不再包含：

```go
"$btnContact.Add_Click({ Write-Command 'contact_author' })"
"$btnContact = New-Button 24 240 158 42 '联系作者'"
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `go test ./internal/ui -run TestDashboardContactButtonOpensQRCodeDialog -v`

Expected: FAIL，缺少二维码弹窗和本地点击处理。

- [ ] **Step 3: 实现最小管理台交互**

在 `dashboardScript` 中：

- 增加 `Show-ContactDialog($owner)`，内容为标题、微信号 `ai_creater99`、二维码、复制微信号按钮和关闭按钮。
- 从“常用操作”按钮矩阵移除 `$btnContact`，保留其他按钮和动作。
- 将“充值说明”移动到右下角操作区左侧，将“联系作者”放在最右侧。
- 点击联系按钮直接执行 `Show-ContactDialog $form`。
- 图片缺失或加载失败时在弹窗内显示“二维码加载失败”。

- [ ] **Step 4: 运行测试确认 GREEN**

Run: `go test ./internal/ui -run 'TestDashboard(Contact|Script)' -v`

Expected: PASS。

---

### Task 4: Android 联系区域状态决策

**Files:**
- Create: `android/app/src/main/java/com/msitools/anyconnectmobile/core/ContactAuthorPresentation.kt`
- Create: `android/app/src/test/java/com/msitools/anyconnectmobile/core/ContactAuthorPresentationTest.kt`

**Interfaces:**
- Produces: `data class ContactAuthorPresentation(val showInlineQr: Boolean, val showMainButton: Boolean)` 与 `fun contactAuthorPresentation(isConnected: Boolean): ContactAuthorPresentation`。

- [ ] **Step 1: 写失败测试**

```kotlin
class ContactAuthorPresentationTest {
    @Test fun loginShowsInlineQrOnly() {
        assertEquals(ContactAuthorPresentation(showInlineQr = true, showMainButton = false), contactAuthorPresentation(false))
    }
    @Test fun connectedMainScreenShowsButtonOnly() {
        assertEquals(ContactAuthorPresentation(showInlineQr = false, showMainButton = true), contactAuthorPresentation(true))
    }
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `android\gradlew.bat -p android :app:testDebugUnitTest --tests '*ContactAuthorPresentationTest'`

Expected: FAIL，类型与函数尚未定义。

- [ ] **Step 3: 实现最小状态函数**

```kotlin
data class ContactAuthorPresentation(val showInlineQr: Boolean, val showMainButton: Boolean)

fun contactAuthorPresentation(isConnected: Boolean) = ContactAuthorPresentation(
    showInlineQr = !isConnected,
    showMainButton = isConnected,
)
```

- [ ] **Step 4: 运行测试确认 GREEN**

Run: `android\gradlew.bat -p android :app:testDebugUnitTest --tests '*ContactAuthorPresentationTest'`

Expected: PASS。

---

### Task 5: Android 登录常驻二维码与主界面按钮

**Files:**
- Modify: `android/app/src/main/java/com/msitools/anyconnectmobile/MainActivity.kt`
- Modify: `android/app/src/main/res/values/strings.xml`

**Interfaces:**
- Consumes: `contactAuthorPresentation(isConnected)` 与 `R.drawable.wechat_contact_qr`。
- Produces: `loginContactView`、`mainContactButton`、含二维码的 `showContactDialog()`。

- [ ] **Step 1: 接入两个状态控件**

在 `buildContent()` 中保留 `FrameLayout` 根布局和 `ScrollView`：

- 用 `loginContactCard()` 替换现有底部文字链接，卡片依次显示“联系作者”“微信扫码添加作者”、方形二维码和微信号。
- 在根 `FrameLayout` 右下角添加 `mainContactButton`，使用 `Gravity.END or Gravity.BOTTOM`、18dp 外边距，初始 `GONE`。
- 为滚动内容增加至少 88dp 底部留白，防止连接态按钮覆盖最后一张卡片。

- [ ] **Step 2: 根据连接状态切换可见性**

新增 `applyContactAuthorPresentation(isConnected: Boolean)`：

```kotlin
val presentation = contactAuthorPresentation(isConnected)
loginContactView.visibility = if (presentation.showInlineQr) View.VISIBLE else View.GONE
mainContactButton.visibility = if (presentation.showMainButton) View.VISIBLE else View.GONE
```

分别在 `showFormPage()` 和 `showConnectedPage(...)` 调用。

- [ ] **Step 3: 优化弹窗二维码布局**

在 `showContactDialog()` 中，在微信号上方加入 `ImageView`：

```kotlin
setImageResource(R.drawable.wechat_contact_qr)
scaleType = ImageView.ScaleType.CENTER_INSIDE
adjustViewBounds = true
```

二维码最大边长取 `min(screenWidth - dp(96), dp(320))`。底部使用并排的“复制微信号”和“关闭”按钮；`dialog.show()` 后将窗口宽度限制为屏幕可用宽度的 88%。

- [ ] **Step 4: 构建验证 Android UI**

Run: `android\gradlew.bat -p android :app:testDebugUnitTest :app:assembleDebug`

Expected: BUILD SUCCESSFUL；生成 `android/app/build/outputs/apk/debug/app-debug.apk`。

---

### Task 6: 全量验证与交付物

**Files:**
- Verify only: all files above

- [ ] **Step 1: 格式化并运行 Go 测试**

Run:

```powershell
gofmt -w internal\ui\login.go internal\ui\login_test.go internal\ui\dashboard.go internal\ui\dashboard_test.go
go test ./internal/... ./cmd/... -v
```

Expected: PASS，0 failures。

- [ ] **Step 2: 构建 Windows EXE**

Run:

```powershell
go build -ldflags="-s -w" -o bin\anyconnect-split.exe .\cmd\
```

复制三张 UI 资源到 `bin\ui-assets` 后确认 EXE 与二维码同时存在。

- [ ] **Step 3: 构建 Android APK**

Run: `android\gradlew.bat -p android :app:testDebugUnitTest :app:assembleDebug`

Expected: BUILD SUCCESSFUL。

- [ ] **Step 4: 构建自包含 Windows 安装器**

Run: `PowerShell -ExecutionPolicy Bypass -File tools\package.ps1`

Expected: `AnyConnectSplitTunnelSetup.exe` 生成，payload 校验包含 `ui-assets\wechat-contact-qr.png`。

- [ ] **Step 5: 检查二维码和最终 diff**

用 `view_image` 检查生成 PNG；检查 `git diff --check`、相关文件 diff 和 `git status --short`。确认没有修改 MuMu、应用图标、VPN、路由或托盘逻辑，也没有覆盖现有用户改动。

- [ ] **Step 6: 报告交付物**

报告 Windows EXE、Windows 安装器和 Android APK 的绝对路径、大小及 SHA256；明确自动测试、构建和未执行的人工扫码验证。
