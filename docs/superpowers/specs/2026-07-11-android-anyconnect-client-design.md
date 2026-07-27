# Android 独立 AnyConnect 分流客户端设计

## 文档状态

- 日期：2026-07-11
- 状态：已完成逐节交互确认，等待用户审阅本文
- 目标设备：华为 X6，HarmonyOS 4.3.0.200
- 目标产物：可长期覆盖升级的自签名 Android APK
- 基线说明：设计以当前 D:\project\anyconnect 工作树为准。当前工作树包含尚未提交的分流模式和托盘修复改动，不能把它描述为已发布 Git 基线。

## 1. 背景

现有仓库是 Windows 桌面分流工具，使用 OpenConnect、sing-box、Windows 路由和托盘管理界面。手机端不是桌面端遥控器，而是一套可独立连接同一批 AnyConnect 节点的 Android 客户端。

用户已经确认：

1. 使用新的 Kotlin Android 应用，不直接改造旧 Android 客户端。
2. 使用官方 OpenConnect 核心及其 JNI 接口。
3. 认证只支持用户名和密码，不支持 OTP、SAML/SSO、WebView 登录或客户端证书。
4. 同时支持“国内直连优先”和“国外 VPN 优先”。
5. APK 内置当前桌面发布模板的全部节点，并允许手机端新增、编辑、删除。
6. 密码由 Android Keystore 加密，连接前使用强指纹验证。
7. 网络变化时自动重连当前节点和当前模式。
8. 使用 A 版“专注连接”界面。
9. 新 APK 覆盖固定文件名，旧 APK 先做时间戳备份。
10. 不允许静默切换节点、模式、全局 VPN、IPv4-only 或其他行为降级。

## 2. 目标与成功标准

### 2.1 功能目标

- 在华为 X6 上通过侧载 APK 安装并运行。
- 直接使用手机完成 VPN 权限申请、账号输入、指纹解锁、连接、断开和重连。
- 首次安装内置发布模板中的 18 个节点；不读取本机运行配置中的账号或自定义数据。
- 全新安装默认选择发布模板中的 preferred_site，默认模式为 domestic_direct，但不自动发起连接。
- 提供节点管理、分流规则管理、连接状态、明确错误和前台通知。
- 节点、规则、当前选择、证书状态和加密凭据在同一签名 APK 的覆盖升级后保留。

### 2.2 长期使用标准

- 固定 applicationId 和长期签名证书。
- versionCode 每次发布严格递增，versionName 使用语义版本。
- 原生依赖、Android 工具链和 IP 数据均可追溯、可复现。
- 发布版可从旧版直接覆盖安装，不卸载、不清数据。
- 在华为 X6 完成真实路由容量验证、网络切换验证和 72 小时灭屏 soak test 后，才可标记为“长期稳定版”。

### 2.3 非目标

首版不包含：

- iOS 或鸿蒙原生 Hap/AppGallery 发布。
- Google Play、华为应用市场或自动更新服务。
- 桌面端遥控、云同步或账号云端存储。
- 多账号、按节点保存不同账号。
- OTP、SAML/SSO、WebView 登录、客户端证书或未知认证表单。
- 自动选最快节点、自动换节点或自动换分流模式。
- 开机后无人值守自动连接、Android Always-on VPN 或 Lockdown VPN。
- 从 Windows Credential Manager 迁移密码。
- 对应用自带 DoH/DoT 的强制域名识别。

## 3. 仓库数据基线

### 3.1 节点

桌面节点只有 name 和 server 两个公开配置字段。当前以下来源包含相同顺序的 18 个节点：

- internal/config/config.go 中的 defaultVPNSites()
- configs/config.dist.yaml 中的 vpn_sites

Android 构建只从 configs/config.dist.yaml 提取发布种子，不复制 configs/config.yaml 或 bin/configs/config.yaml。运行配置可能包含本机选择和自定义规则，不能进入 APK。

构建必须执行一致性检查：

- config.dist.yaml 与 defaultVPNSites() 的节点名称、地址和顺序完全一致。
- 节点数量非零，名称和服务器地址合法。
- 资产中不存在 saved_username、密码、Cookie、Windows 路径、网关或接口号。
- 任一检查失败即停止发布。

Android 数据模型为每个内置节点分配稳定 builtinId；用户节点使用 UUID。升级合并规则：

1. 新增内置节点可以加入。
2. 未被用户修改的内置节点可以随新版更新。
3. 用户编辑过的内置节点和用户自定义节点永不被新版覆盖。
4. 用户删除的内置节点保存 tombstone，升级不得自动恢复。
5. 发布方下架的内置节点标记为停用，不直接删除用户当前数据。

删除或 tombstone 一个节点时，同时删除该节点保存的证书指纹；首版使用全局账号，因此不会删除全局加密凭据。

### 3.2 分流模式

内部值和界面文案固定为：

| 内部值 | 界面文案 | 语义 |
|---|---|---|
| domestic_direct | 国内直连优先 | 默认走物理网络，只有国外白名单域名解析结果及国外 CIDR 进入 VPN |
| foreign_direct | 国外 VPN 优先 | 默认进入 VPN，中国 IP 库、国内域名解析结果和局域网直连 |

全新安装默认 domestic_direct。以后若支持导入旧桌面配置：

- split_mode 缺失时按桌面旧配置语义导入为 foreign_direct。
- split_mode 存在但值未知时拒绝导入并显示错误，不自动选择其他模式。

### 3.3 规则种子

发布种子来自 configs/config.dist.yaml：

- 国内域名规则：当前 53 条。
- 国外域名规则：当前 31 条。
- 国外 IP/CIDR 规则：当前为空。
- 中国 IP 数据：构建时生成并冻结一份有效 APNIC CN 快照。

每条内置域名和 CIDR 规则也使用稳定 builtinId，自定义规则使用 UUID。APK 升级只更新未被用户覆盖的内置规则；用户编辑、停用或删除过的规则保存覆盖记录或 tombstone，不被新版静默恢复。

当前 Windows sing-box 可以按 .cn 后缀实时匹配；首版 Android 的域名规则只做 DNS 结果映射，不能等价表达无限的 .cn 通配规则。因此首版依靠中国 IP 数据库和发布模板中明确列出的国内域名；未知 .cn 域名若解析到境外地址，可能按 foreign_direct 的默认规则进入 VPN。规则页必须披露这一差异，不能宣称完整复刻 Windows 的实时域名嗅探。

根目录 data/china_ip_list.txt 当前只是占位文件，不能打进 APK。构建必须使用通过校验的真实快照，并记录：

- 数据来源。
- 生成时间。
- IPv4、IPv6 条目数。
- 规范化后的 SHA-256。
- 数据版本。

更新流程使用“下载到临时文件—完整解析—非空和数量范围校验—哈希计算—原子替换”。更新失败时不改变当前正在使用的数据，也不改变节点或分流模式；界面明确显示更新失败。

## 4. 技术路线

### 4.1 Android 项目

在仓库新增独立 android/ Gradle 工程。Windows Go 程序的运行逻辑不因手机端而改变。

建议的 Gradle 模块：

- :app：Compose UI、状态机、VpnService、Room、Keystore、通知和系统集成。
- :routing-core：纯 Kotlin 的 CIDR、规则优先级、DNS 映射、路由压缩和校验。
- :openconnect-native：C/C++ 构建、JNI 包装、OpenConnect 引擎进程和原生诊断。

首版固定：

- minSdk 26。
- compileSdk 35。
- targetSdk 35。
- 发布 ABI 为 arm64-v8a；x86_64 仅用于模拟器和自动测试。
- JDK 17。
- OpenConnect v9.21，使用发布源码和 PGP/哈希校验后锁定确切 commit。
- Android NDK r29；所有原生库按 16 KB page size 要求构建和验证。
- applicationId 固定为 com.msitools.anyconnectmobile。

Android Gradle Plugin、Kotlin、Compose、Room 和其他依赖在实施时写入版本目录并锁定校验和；发布构建禁止动态版本。

### 4.2 许可证边界

OpenConnect 核心采用 LGPL 2.1。官方 ics-openconnect Android 应用整体采用 GPLv2。

本项目只使用 OpenConnect 官方仓库中的 LGPL 核心和 JNI binding，不复制 ics-openconnect 的 Service、UI 或其他 GPL 实现。ics-openconnect 只能作为兼容性和构建行为参考。

每次发布必须附带：

- THIRD_PARTY_NOTICES。
- 许可证全文。
- SPDX SBOM。
- 每个原生库的版本、commit 和许可证。
- 对应源码、修改补丁和可复现构建脚本。
- 未剥离 native symbols 的独立归档。

## 5. 进程与组件边界

### 5.1 UI 进程

负责：

- 展示连接页、节点页、规则页和设置页。
- 发起 VpnService 权限申请。
- 启动专用 CredentialUnlockActivity 完成 BiometricPrompt；主 UI 进程不接收解密后的密码。
- 发送连接、断开、重试和保存规则命令。
- 只展示状态，不直接持有 TUN 或原生 session。

### 5.2 VpnService 进程

VpnService 是 VPN 会话的唯一所有者，负责：

- 进入前台并维护持续通知。
- 监听非 VPN 物理网络变化。
- 构建 VpnService.Builder。
- 持有原始 ParcelFileDescriptor。
- 维护“用户希望保持连接”的 desiredConnected 状态。
- 通过受限 Binder 控制原生引擎。
- 在原生引擎崩溃时继续保留 TUN，使应走 VPN 的流量暂时黑洞，而不是恢复普通直连。

VpnService 与 CredentialUnlockActivity 固定运行在同一个 :vpn 进程。CredentialUnlockActivity 在该进程内解密凭据并写入 SessionCredentialVault，避免把明文密码序列化到主 UI Binder 消息。

清单明确设置 SUPPORTS_ALWAYS_ON=false。首版不承诺开机自动恢复，因为指纹保护的凭据不能在后台无人值守解密。

### 5.3 OpenConnect 引擎进程

libopenconnect 运行在独立 :engine 进程。职责：

- 解析认证表单并只接受预期的用户名、密码字段。
- 建立 TLS/CSTP 连接。
- 获取隧道地址、DNS、MTU 和服务器路由信息。
- 接收 VpnService 传入的 TUN 副本。
- 执行 OpenConnect mainloop、取消和同节点重连。
- 将结构化状态和错误回传给 VpnService。

TUN 所有权必须明确：

- VpnService 保留原始 fd。
- Binder 只传递 dup 后的 ParcelFileDescriptor。
- engine 关闭自己的副本，不得关闭 service 原始 fd。
- 每次重建路由先建立新 TUN，成功切换后再关闭旧 TUN。
- 所有失败路径测试重复关闭、fd 泄漏和 use-after-free。

### 5.4 socket 保护

OpenConnect 创建的连接和重连 socket 必须在 connect() 前：

1. 通过 VpnService.protect(fd) 排除出自身 VPN。
2. 绑定当前物理 Network。

任一步失败即取消当前连接并返回 SOCKET_PROTECT_FAILED。不能忽略 protect 回调失败，也不能尝试不绑定网络继续连接。

网络监听只接受 NET_CAPABILITY_NOT_VPN，避免监听到本应用 VPN 后形成重连循环。每次网络 generation 递增，旧 generation 的异步结果不得覆盖新状态。

## 6. 认证与本地安全

### 6.1 账号模型

首版只有一套用户名和密码，所有节点共用。多账号和按节点凭据不在首版范围。

只接受用户名和密码认证。服务器出现 OTP、SAML、WebView、客户端证书或未知必填字段时返回 UNSUPPORTED_AUTH_FORM，不猜测填写，不改变节点。

### 6.2 密码存储

- 使用 Android Keystore 生成 auth-per-use AES-GCM 密钥。
- BiometricPrompt 使用 BIOMETRIC_STRONG 和 CryptoObject。
- 密文和随机 IV 保存到应用私有存储；明文不落盘。
- 密码、Cookie、认证表单和解密字节不进入日志、剪贴板、崩溃报告或 Android Backup。
- 指纹重新录入或锁屏凭据变化导致密钥失效时，删除失效密文并要求重新输入密码。

明文生命周期固定为：

1. CredentialUnlockActivity 在 :vpn 进程中把解密结果放入可清零 ByteArray；禁止转换成 Kotlin/Java String。
2. VpnService 的 SessionCredentialVault 是当前存活会话中唯一允许长期持有明文的组件。Activity 写入后立即清零自己的临时数组。
3. engine 请求认证时，VpnService 创建一次性匿名 pipe，只通过 Binder 传递 ParcelFileDescriptor，不在 Binder Parcel 中放明文。
4. engine 读入临时 native buffer 后立即关闭 pipe；JNI 认证回调完成后清零 binding 和 OpenConnect 表单中的密码副本。
5. Cookie 只存在于 engine 原生内存；engine teardown 时清除。
6. 手动断开、VpnService 销毁、设备重启或凭据变更时，SessionCredentialVault 必须清零。
7. :engine 崩溃但 :vpn 仍存活时进入 NATIVE_ENGINE_CRASHED，不自动重启 engine；用户点“重试”后可在同一存活会话内复用 vault，无需再次解锁。
8. :vpn 进程死亡后 vault 必然丢失，下一次连接只能进入 AUTH_REQUIRED。

### 6.3 自动重连与指纹

首次连接必须前台验证指纹。成功后，当前服务会话可在内存中保留最小认证状态：

- 优先保留 OpenConnect 当前 Cookie。
- 必须重新认证时，可使用当前存活会话中的密码缓冲区。
- Cookie 和密码都不落盘。
- 手动断开、服务停止、进程死亡、设备重启或凭据变更时立即清除。

因此：

- 同一存活会话内的 Wi-Fi/移动网络变化可以自动重连；先尝试当前 Cookie，Cookie 不再可用时只允许使用 SessionCredentialVault 对当前节点重新认证一次。
- 服务器拒绝该密码时清除 Cookie 和 vault，进入 AUTH_REQUIRED，并携带 AUTH_FAILED；不继续自动重试。
- :vpn 进程死亡或服务器在 vault 已清除后再次要求密码时进入 AUTH_REQUIRED，通知用户点开并验证指纹。
- 不自动改用设备 PIN、不延长认证窗口、不跳过指纹。

### 6.4 证书

- 正常 PKIX、主机名和有效期验证必须全部通过。
- 原生 TLS 不能假设 Android Network Security Config 会自动作用于 GnuTLS；实现必须明确接入 Android TrustManager 或经审计的 CA 集。
- 公开 CA 证书首次正常验证通过后，按节点记录完整 SHA-256 指纹。
- 自签名或私有 CA 证书首次使用时，必须展示完整 SHA-256 指纹，由用户通过带外渠道核对后明确接受。
- 后续任何节点证书指纹变化都必须在发送用户名和密码前返回 CERT_CHANGED，即使新证书本身可通过公开 CA 验证。
- 更新已保存的证书指纹必须再次通过 BIOMETRIC_STRONG 身份验证，并同时展示旧、新完整证书指纹。
- 禁止 SHA-1、部分指纹、“自动信任”和未配置的跨主机重定向。

## 7. 连接和路由数据流

### 7.1 连接阶段

状态机按以下顺序运行：

1. DISCONNECTED。
2. PREPARING：检查 VPN 权限、通知权限、规则库、IP 库和物理网络。
3. AUTH_REQUIRED：需要时显示指纹。
4. RESOLVING_GATEWAY：在物理 Network 上解析节点，保护网关地址。
5. AUTHENTICATING：libopenconnect 执行用户名密码认证。
6. NEGOTIATING：获取地址、MTU 和服务器参数。
7. RESOLVING_RULES：使用当前会话的 VPN 侧 DNS 解析规则域名。
8. APPLYING_ROUTES：生成并校验精确路由计划，建立最终 TUN。
9. CONNECTED。
10. RECONNECTING：只针对当前节点、当前模式和当前规则 generation。
11. ERROR 或 AUTH_REQUIRED。

任何阶段失败都不会自动选择另一节点、模式或规则集。

### 7.2 DNS 策略

首版不实现按域名实时嗅探，而是将域名规则解析为 A/AAAA 地址并按 TTL 维护。因此界面和文档必须称为“域名解析映射规则”，不能宣称“严格域名级分流”或“可控制所有应用 DoH”。

为使规则解析结果与普通应用的系统 DNS 尽量一致，VPN 活跃期间：

- 使用单一 VPN 侧 DNS，初始配置为 8.8.8.8。
- 该 DNS 地址在两种模式下都强制走 VPN。
- 规则解析和 Android 系统 DNS 使用同一 DNS。
- 节点主机名在 VPN 建立前仍由物理 Network 解析，并始终从 VPN 路由中排除。
- 若严格 Private DNS 与本应用 DNS 策略冲突，返回 PRIVATE_DNS_CONFLICT，不自动改变系统设置。

连接时使用受限的 DNS bootstrap 路由先解析全部规则，再建立最终路由。DNS bootstrap 只允许 VPN DNS 与隧道控制流量，不临时开启全局 VPN。

DNS bootstrap TUN 的生命周期固定为：

1. OpenConnect 完成 TLS/CSTP 协商并取得隧道地址和 MTU，但尚未建立最终路由。
2. VpnService 创建 bootstrap TUN。它只包含隧道地址、VPN DNS 路由和必需的系统保护规则；VpnService 保留原始 fd。
3. VpnService 将 dup 后的 fd 交给 engine；engine 在自己的单线程 native event loop 中调用 openconnect_setup_tun_fd() 并启动 mainloop。
4. 规则解析器通过该 VPN Network 和 VPN DNS 获取全部 A/AAAA 与 TTL。
5. routing-core 生成并完整校验最终计划后，VpnService 创建 candidate TUN 并保留其原始 fd，再把 dup 后的 candidate fd 交给 engine。
6. engine 在同一个 native event loop 中暂停旧 TUN 读取、切换到 candidate fd，并向 VpnService 返回确认；禁止在两个 TUN 上并发读写。
7. VpnService 收到确认后才关闭 bootstrap TUN。TTL 刷新沿用相同的 candidate—确认—关闭旧 TUN 流程。
8. 任一步失败都关闭 candidate、bootstrap 和 engine session，进入明确失败终态；不保留 DNS-only TUN，不执行第二次认证式重连，也不恢复旧路由伪装成功。

Gate 1 必须证明 openconnect_setup_tun_fd() 的运行中切换在锁定版本上安全可用。若不能证明，Gate 1 失败并重新设计，不增加隐藏重连路径。

解析要求：

- 同时请求 A 和 AAAA。
- 记录 DNS TTL、解析 generation 和规则来源。
- TTL 到期前生成新路由计划并进行受控 TUN 重建。
- 刷新失败时断开受影响会话并返回 DNS_RULE_REFRESH_FAILED，不继续使用过期映射。
- 应用自带 DoH/DoT 可能绕开系统 DNS；其最终连接只能按目的 IP 命中现有规则，此限制必须在规则页说明。

### 7.3 国内直连优先

domestic_direct 的路由优先级：

1. VPN 网关、局域网、链路本地地址始终走物理网络。
2. VPN DNS 始终进入 VPN。
3. 用户国外 IP/CIDR 规则进入 VPN。
4. 用户国外域名的 A/AAAA 映射进入 VPN。
5. 内置国外域名的 A/AAAA 映射进入 VPN。
6. 其余流量走物理网络。

规则冲突时，系统保护规则最高；用户显式规则高于内置规则。输入 CIDR 必须规范化、去重并检查重叠。

### 7.4 国外 VPN 优先

foreign_direct 的路由优先级：

1. VPN 网关、局域网、链路本地地址始终走物理网络。
2. 中国 IP 数据库及明确配置的国内域名 A/AAAA 映射走物理网络。
3. VPN DNS 进入 VPN。
4. 其余流量进入 VPN。

Android API 33 及以上：

- 添加 IPv4/IPv6 默认 VPN 路由。
- 使用 excludeRoute() 排除中国网段、国内域名 A/AAAA 映射、局域网和 VPN 网关。

Android API 26—32：

- 先合并中国网段、国内域名 A/AAAA 映射、局域网和 VPN 网关，形成完整直连集合。
- 计算该直连集合的精确补集。
- 只把补集添加为 VPN 路由。

两种实现必须得到相同语义。若系统无法承载完整精确路由，返回 ROUTE_CAPACITY_EXCEEDED 或 ROUTE_APPLY_FAILED。禁止：

- 改为全局 VPN。
- 丢弃部分中国网段。
- 只启用 IPv4。
- 自动切换为 domestic_direct。
- 使用上一套路由继续显示“已连接”。

### 7.5 IPv6

- 路由核心同时处理 IPv4 和 IPv6。
- 若物理网络提供 IPv6且存在应进入 VPN 的 AAAA/IPv6 CIDR，服务器隧道也必须提供可用 IPv6。
- 无法安全满足策略时返回 IPV6_POLICY_UNSATISFIED。
- 不允许静默关闭 IPv6或让受保护 IPv6 直接绕行。

### 7.6 QUIC

Windows sing-box 当前拒绝 UDP/443 以强制浏览器离开 QUIC。Android 首版不复制这一规则，因为 VpnService 的 IP 路由同时作用于 TCP 和 UDP，且纯 OpenConnect TUN 路径没有额外包过滤器。

发布验收必须单独验证 UDP/443 的路径。如果 QUIC 实测与路由计划不一致，首版发布被阻断；不能静默禁用 QUIC 或把全局 UDP/443 拦截作为隐藏修复。

## 8. 路由容量硬门槛

当前 bin/data/china_ip_list.txt 的只读测量结果：

- 原始记录 10,826 条。
- IPv4 原始 8,786 条，合并后约 5,488 条。
- IPv6 原始 2,040 条，合并后约 2,012 条。
- API 33 以下精确 IPv4 补集约 11,953 条。
- API 33 以下精确 IPv6 补集约 15,212 条。

因此第一项实现工作不是完整 UI，而是“路由容量探针 APK”。必须在华为 X6 上读取真实 SDK_INT 并测试：

- 纯 IPv4 最大规则集。
- 纯 IPv6 最大规则集。
- 双栈完整规则集。
- Builder.establish() 返回值。
- 建立耗时、路由数量、RSS 和重建耗时。
- 受控探针包是否按计划进入 TUN 或绕过 TUN。Gate 0 不声称已经验证真实 OpenConnect 出口；真实 VPN 路径属于 Gate 1。

任一 CIDR 无效、Binder 超限、establish() 返回空、进程异常或路由验证不一致，立即停止该架构的正式实现。若真机无法承载精确规则，需要重新提交架构决策给用户，不能继续包装成“两种模式已支持”。

## 9. 传输层

首版固定为 TLS/CSTP-only：

- 在连接前显式关闭 DTLS。
- 状态页显示实际传输为 TLS。
- 不执行 DTLS 失败后转 TLS 的传输层 fallback。

以后若需要 DTLS 性能优化，必须作为新的明确设计选择，经用户批准后实现和测试。

## 10. 自动重连

ConnectivityManager 只监听具有互联网能力且 NOT_VPN 的物理网络。

重连规则：

- 用户主动连接后 desiredConnected=true。
- 物理网络丢失时进入 RECONNECTING，保留当前节点、模式和规则 generation。
- 有效网络恢复后使用 2 秒、5 秒、15 秒、30 秒的退避间隔；随后保持 30 秒间隔，直到成功、用户断开或需要指纹。
- Wi-Fi 和移动网络切换时更新 setUnderlyingNetworks()。
- 同一时刻只允许一个 native session。
- 旧网络 generation 的回调和连接结果全部丢弃。
- 手动断开会取消定时器、关闭 native session、关闭 TUN、清除内存认证状态并设置 desiredConnected=false。

规则、节点或模式保存时：

- 若未连接，只持久化。
- 若已连接，界面明确显示“正在应用并短暂重连”。
- 新路由建立失败时进入 ERROR，不恢复旧规则继续伪装成功。

## 11. UI 设计

### 11.1 主连接页

A 版“专注连接”结构：

- 顶部显示应用名、设置入口和当前状态。
- 中部显示当前节点、分流模式和简要规则摘要。
- 中央大按钮负责连接/断开。
- 连接过程按“准备—认证—解析规则—应用路由—已连接”显示进度。
- 错误状态显示真实原因、错误码、“重试”和“断开”。

### 11.2 节点页

- 列出所有内置和自定义节点。
- 单击选择。
- 长按或更多菜单执行编辑、停用和删除。
- 添加节点校验 HTTPS 地址、端口和名称；禁止 URL 中嵌入用户名或密码。
- 编辑当前正在使用的节点时先明确断开。

### 11.3 规则页

- 按当前模式展示“域名”和“IP/CIDR”两个页签。
- 标识内置规则、自定义规则和已禁用规则。
- 保存时显示需要短暂重连。
- 显示“域名规则按 DNS 结果映射，应用自带 DoH 可能不受域名规则控制”的限制。
- 显示 IP 数据版本、条目数、哈希摘要和最近更新时间。

### 11.4 前台通知

从 CONNECTING 开始立即显示持续通知：

- 当前节点。
- 当前模式。
- 连接、重连或错误状态。
- 点击打开应用。
- 提供断开操作。

用户拒绝必要的通知权限时不建立后台 VPN，返回 NOTIFICATION_PERMISSION_REQUIRED。

### 11.5 华为引导

首次连接完成后提供一次性引导，说明用户需手动设置：

- 允许应用后台活动。
- 电池优化中允许持续运行。
- 休眠时保持网络。
- 在多任务界面锁定后台卡片。

应用不使用 root、Accessibility 或隐藏系统设置来规避华为后台策略，也不承诺系统永不结束进程。

## 12. 错误模型

ConnectionState 使用第 7.1 节定义的完整运行状态。只有失败终态进入 ERROR 或 AUTH_REQUIRED，且当前节点和模式不改变。AUTH_REQUIRED 是状态，不是错误码；状态可附带一个 ErrorCode 解释进入原因。

| 错误码 | 含义 |
|---|---|
| AUTH_FAILED | 服务器拒绝用户名或密码；清除当前内存认证状态并要求用户重新输入 |
| UNSUPPORTED_AUTH_FORM | 服务器要求首版不支持的认证方式 |
| CERT_UNTRUSTED | 证书无法通过系统信任且尚未明确接受 |
| CERT_CHANGED | 已记录证书指纹发生变化 |
| SOCKET_PROTECT_FAILED | VPN 控制 socket 无法排除自身隧道或绑定物理网络 |
| ROUTE_CAPACITY_EXCEEDED | 系统无法承载完整路由集 |
| ROUTE_APPLY_FAILED | 路由校验或 TUN 建立失败 |
| IPV6_POLICY_UNSATISFIED | IPv6 不能满足当前精确分流策略 |
| DNS_RULE_REFRESH_FAILED | 域名规则解析或 TTL 刷新失败 |
| PRIVATE_DNS_CONFLICT | 系统 Private DNS 与当前规则映射策略冲突 |
| NATIVE_ENGINE_CRASHED | OpenConnect 原生引擎异常退出 |
| NETWORK_UNAVAILABLE | 没有可用物理网络 |
| RULE_DB_INVALID | 规则或 IP 数据库校验失败 |
| NOTIFICATION_PERMISSION_REQUIRED | 无法满足前台 VPN 通知要求 |

原生引擎崩溃时：

- VpnService 保留 TUN。
- 应走 VPN 的流量暂时黑洞。
- 直连排除项仍按既定路由工作。
- 通知显示 NATIVE_ENGINE_CRASHED。
- 只允许重试当前节点或由用户断开。

AUTH_FAILED 后不自动再次提交已保存密码。加密密文标记为 requiresReplacement，只有用户重新输入并完成 BIOMETRIC_STRONG 保存后才替换；取消输入则保持 AUTH_REQUIRED。

若 VpnService 本身被系统或用户强停，Android 会移除 VPN 接口。本应用无法同时保证分流直连和 Lockdown 的全量阻断，因此首版不把 Lockdown 当作补救策略。

## 13. 本地数据

Room 保存：

- 节点目录、稳定 ID、来源、用户覆盖和 tombstone。
- 两种模式的域名/IP 规则。
- 当前节点和当前模式。
- IP 数据元信息。
- 节点证书指纹状态。
- 应用设置和 schema 版本。

DataStore 只保存非关系型 UI 偏好。Keystore 密文与 Room 逻辑数据分离。

应用数据禁止进入 Android 自动备份：

- 密码密文和 IV。
- Cookie。
- 节点证书指纹状态。
- 诊断日志。

日志为本地、脱敏、轮转日志，只记录：

- 状态阶段。
- 错误码。
- 路由数量和计划哈希。
- 数据库版本。
- 网络 generation。

禁止记录用户名、密码、Cookie、认证表单、完整证书、运行时网关和签名秘密。导出诊断必须由用户主动触发并再次做脱敏。

## 14. 数据更新

中国 IP 数据默认每 7 天检查一次，与桌面默认间隔一致。

更新不在活动连接中直接替换路由：

1. 下载、解析和验证新数据。
2. 生成新计划并完成容量预检。
3. 用户可见地进入短暂重连。
4. 新 TUN 和路由成功后才切换数据库 generation。
5. 下载或候选数据校验失败时保持当前已验证 generation 和现有连接，显示非致命 IP_DB_UPDATE_FAILED 警告。
6. 候选数据通过校验、开始应用后若 TUN 或路由切换失败，则断开当前会话并进入 ROUTE_APPLY_FAILED；数据库 generation 不切换，不继续显示“已连接”。

IP_DB_UPDATE_FAILED 是 WarningCode，不是 ConnectionState 或自动 fallback。它只表示候选更新从未成为活动数据。

节点和内置域名目录首版随 APK 更新，不从未签名远端源自动下载。

## 15. 测试方案

### 15.1 路由容量探针

这是正式开发前的发布阻断 Gate 0：

- 连接华为 X6 后读取 model、SDK_INT、ABI、page size。
- 分别测试 IPv4、IPv6、双栈路由。
- 记录 establish()、内存、耗时和真实路径。
- 验证 API 33+ excludeRoute 与 API 26—32 补集实现。
- 失败时停止完整功能开发并提交新的架构选择。

DNS bootstrap 需要最小 OpenConnect 引擎，因此不属于纯 Builder 探针。它在原生依赖首次构建后作为 Gate 1 单独验证；Gate 1 未通过时仍不得进入完整路由、存储或 UI 开发。

### 15.2 单元测试

routing-core：

- CIDR 规范化、去重、合并、相减和精确补集。
- IPv4/IPv6 边界、单地址、重叠和冲突优先级。
- 中国网段与补集互斥且覆盖完整地址空间。
- VPN 网关和局域网永不进入 VPN。
- 两种模式的规则顺序。
- A/AAAA、TTL、generation 和过期处理。
- 非法规则不能部分生效。

存储：

- 节点 seed 与升级合并。
- 用户覆盖和 tombstone 不被新版恢复。
- Room schema 迁移。
- Keystore 失效、错误指纹和密文篡改。

### 15.3 原生与集成测试

- 使用本地 ocserv 测试环境，不使用生产账号。
- 用户名密码认证成功和失败。
- 未知认证表单返回 UNSUPPORTED_AUTH_FORM。
- 证书首次信任、指纹变化和主机名错误。
- protect socket 失败立即取消。
- JNI 回调线程 attach/detach。
- cancel fd、global ref、密码缓冲清零。
- TUN fd 重复关闭、泄漏和进程崩溃。
- TLS-only，不出现 DTLS socket。
- release symbols 可对 native tombstone 完成符号化。

### 15.4 Android 仪器测试

- 首次 VPN 权限。
- 指纹成功、失败、取消和密钥失效。
- 节点增删改。
- 规则增删改及可见重连。
- 通知权限、onRevoke() 和手动断开。
- UI 旋转、进程重建和状态恢复。
- API 26、33、35 代表环境。
- 16 KB page size Android 15 模拟器。

### 15.5 网络验收

在受控双栈网络验证：

- 两种模式下国内、国外测试地址的真实出口。
- 系统 DNS UDP/53、TCP/53 和 Private DNS 冲突。
- 受控 DoH 应用的已知限制。
- TCP/443 与 UDP/443。
- Wi-Fi 到移动网络、移动网络到 Wi-Fi。
- 100 次网络切换后无双 session、回调泄漏或节点/模式变化。
- 规则 TTL 刷新前后路径一致。
- 原生引擎强制崩溃后 VPN 路由黑洞而非直连。

### 15.6 华为长期测试

- 安装、首次权限、指纹和后台设置引导。
- 锁屏、灭屏、充电、低电量模式和网络休眠。
- 72 小时 soak test。
- 无 ANR、native tombstone、通知消失、非预期模式变化或持续 RSS 增长。

若没有连接到华为 X6，最多只能交付“已构建、待真机验证”的 APK，不能宣称已支持该设备或已达到长期稳定。

## 16. 构建与交付

### 16.1 可复现原生构建

官方 Android OpenConnect 原生依赖构建链以 Linux 为主。仓库提供固定 Linux 容器构建脚本：

- 下载并校验 OpenConnect v9.21 发布源码。
- 锁定全部 native 依赖及校验和。
- 使用 NDK r29 构建 arm64-v8a 和测试用 x86_64。
- 检查 16 KB ELF segment 对齐。
- 输出 stripped .so、unstripped symbols 和许可证清单。

Windows 侧只调用统一包装脚本，不手工复制未知来源的预编译 .so。

### 16.2 长期签名

- 生成一把有效期至少 25 年的 release keystore。
- keystore 保存在仓库外，例如 C:\Users\MSI\.android-signing\anyconnect-mobile-release.jks。
- 密码不写入仓库、Gradle 文件、APK、日志或 release manifest。
- 保存两份加密离线备份，并单独保管恢复信息。
- 记录签名证书公开 SHA-256 指纹。
- 每版执行 apksigner verify --verbose --print-certs。

丢失或更换自管签名密钥后无法覆盖安装旧版，因此签名备份是发布阻断项。

### 16.3 固定产物

正式文件固定为：

- dist/AnyConnect-Mobile.apk

覆盖流程：

1. 构建候选 APK。
2. 完成测试、签名和哈希校验。
3. 若固定 APK 已存在，复制到 dist/backups/AnyConnect-Mobile-版本-时间戳.apk。
4. 原子替换 dist/AnyConnect-Mobile.apk。
5. 再次校验固定文件的 APK 哈希和签名指纹。

每版同时生成：

- APK SHA-256。
- 签名证书 SHA-256。
- release manifest：版本、versionCode、Git commit、节点目录版本、IP 数据版本/数量/哈希、构建工具版本。
- 发布说明。
- THIRD_PARTY_NOTICES、SPDX SBOM 和对应源码归档。
- native symbols 归档。

### 16.4 覆盖升级验收

在华为 X6 安装旧版后执行 adb install -r 或系统安装器覆盖：

- 安装成功且包名不变。
- 节点、规则、当前选择、Keystore 密文和节点证书指纹状态保留。
- versionCode 正确递增。
- 使用不同签名密钥的负例被系统拒绝。

## 17. 实施顺序

本文是 Android 客户端总设计，不对应一个超大实施计划。后续拆为四个有先后门槛的子项目：

1. Gate 0：纯 VpnService 路由容量探针。
2. Gate 1：最小 OpenConnect、真实隧道、DNS bootstrap 与 TUN 原子切换。
3. 完整客户端：routing-core、存储、安全、重连和 A 版 UI。
4. 发布工程：长期签名、兼容测试、72 小时 soak 和固定 APK 覆盖。

下一份 writing-plans 计划只覆盖 Gate 0。Gate 0 通过后，Gate 1 和后续子项目分别编写聚焦规格与实施计划，不能把未验证门槛后的工作伪装成已排定实现。

总路线顺序为：

1. Gate 0：华为 X6 的 SDK_INT、路由容量和双栈 Builder 探针。
2. 固化 OpenConnect v9.21、NDK r29、依赖许可证和可复现容器构建。
3. Gate 1：最小 TLS-only OpenConnect、socket protect、DNS bootstrap 和 TUN 切换探针。
4. 完成 routing-core 与全部纯 Kotlin 测试。
5. 完成独立 native engine、TUN 所有权和崩溃隔离。
6. 完成 Room、节点/规则 seed 与 Keystore 指纹流程。
7. 完成 A 版 Compose UI、前台通知和重连协调器。
8. 完成模拟器、ocserv 和华为真机测试。
9. 生成长期签名 APK、备份旧 APK并覆盖固定文件。

Gate 0 失败时停止第 2—9 步；Gate 1 失败时停止第 4—9 步，并重新进行架构设计。此顺序避免在华为路由和 DNS 数据流尚未证实时先投入完整 UI 和业务实现。

## 18. 最终验收

只有同时满足以下条件才能称为完成：

- 华为 X6 上两种模式均通过真实出口验证。
- IPv4/IPv6、DNS、TCP/UDP 路径符合本文。
- 没有静默节点、模式、传输或地址族 fallback。
- 指纹、进程死亡和自动重连行为符合安全边界。
- 所有自动测试通过。
- 72 小时真机 soak test 通过。
- 覆盖安装保留用户数据。
- APK 签名、哈希、许可证、SBOM、源码和 symbols 完整。
- dist/AnyConnect-Mobile.apk 是已验证的新版本，旧版本已有时间戳备份。

## 19. 参考

- Android VpnService：
  https://developer.android.com/reference/android/net/VpnService
- Android VpnService.Builder：
  https://developer.android.com/reference/android/net/VpnService.Builder
- Android Keystore：
  https://developer.android.com/privacy-and-security/keystore
- Android 生物识别：
  https://developer.android.com/identity/sign-in/biometric-auth
- Android 16 KB page size：
  https://developer.android.com/guide/practices/page-sizes
- Android 应用签名：
  https://developer.android.com/studio/publish/app-signing
- OpenConnect v9.21：
  https://www.infradead.org/openconnect/download.html
- OpenConnect 官方仓库及 LGPL：
  https://gitlab.com/openconnect/openconnect
- ics-openconnect 及 GPLv2 边界：
  https://gitlab.com/openconnect/ics-openconnect
- 华为后台运行说明：
  https://consumer.huawei.com/cn/support/content/zh-cn00428704/
