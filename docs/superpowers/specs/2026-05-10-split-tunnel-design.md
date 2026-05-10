# AnyConnect 智能分流工具设计文档

## Context

使用 Cisco AnyConnect VPN 后，所有流量都通过 VPN 隧道路由，导致访问国内网站时流量绕经海外节点，速度极慢。需要开发一个工具，在 VPN 连接后自动修改系统路由表，让国内 IP 段的流量绕过 VPN 直连，从而实现国内外流量智能分流。

## 技术方案

### 核心原理

AnyConnect 连接后会修改 Windows 路由表，将默认路由指向 VPN 虚拟网卡。本工具通过在路由表中添加更具体的中国 IP 段路由（指向原始本地网关），利用最长前缀匹配原则，使国内流量走本地直连，国外流量继续走 VPN。

### 技术选型

- 语言: Go
- 平台: Windows
- GUI: 系统托盘（systray 库）
- 权限: 需要管理员权限运行

## 架构设计

### 模块划分

```
┌─────────────────────────────────────────┐
│              System Tray UI             │
│         (状态显示, 菜单操作)             │
├─────────────────────────────────────────┤
│              Core Engine                │
│  ┌───────────┐  ┌──────────┐  ┌──────┐ │
│  │VPN Monitor│  │Route Mgr │  │IP DB │ │
│  │(状态检测) │  │(路由操作) │  │(数据)│ │
│  └───────────┘  └──────────┘  └──────┘ │
├─────────────────────────────────────────┤
│           Config Manager                │
│        (配置读写, 状态持久化)            │
└─────────────────────────────────────────┘
```

### 1. VPN Monitor (VPN 状态监控)

**职责**: 检测 AnyConnect VPN 连接/断开事件

**实现方式**:
- 每 2 秒轮询 Windows 网络适配器列表
- 使用 `GetAdaptersAddresses` Win32 API
- 检测包含 "Cisco" 关键字的虚拟网卡上线/下线
- 状态变化时通知 Core Engine

**状态机**:
```
Idle (VPN未连接)
  │ 检测到 Cisco 适配器上线
  ▼
Connected (VPN已连接, 待应用路由)
  │ 路由添加完成
  ▼
Active (分流生效中)
  │ 检测到 Cisco 适配器下线
  ▼
Cleaning (清理路由中)
  │ 清理完成
  ▼
Idle
```

**原始网关记录**:
- 程序启动时(VPN未连接)保存当前默认网关
- 网关信息持久化到配置文件
- 若启动时VPN已连接,从配置文件读取上次保存的网关

### 2. Route Manager (路由管理器)

**职责**: 添加/删除路由规则

**添加路由**:
```
route add <network> mask <mask> <original_gateway> metric 5
```
- metric 5 确保优先级高于 VPN 默认路由
- 批量执行,每条路由独立错误处理

**清理路由**:
- VPN 断开时删除所有已添加的路由
- 维护已添加路由列表(内存+文件),用于:
  - 正常清理
  - 异常恢复(程序崩溃后重启时清理残留)

**容错机制**:
- 已添加路由记录到 `data/applied_routes.json`
- 程序启动时检查是否有残留路由需要清理
- 单条路由添加失败不影响其他路由

### 3. IP Database (IP 数据库管理)

**数据源**: APNIC delegated 文件
- URL: `https://ftp.apnic.net/apnic/stats/apnic/delegated-apnic-latest`
- 解析规则: 筛选 `apnic|CN|ipv4|` 开头的行
- 字段格式: `apnic|CN|ipv4|<start_ip>|<host_count>|...`
- 将 host_count 转换为 CIDR 掩码

**本地存储**: `data/china_ip_list.txt`
- 格式: 每行一个 CIDR (如 `1.0.1.0/24`)
- 约 8000-9000 条记录

**更新策略**:
- 每 7 天自动检查更新
- 托盘菜单支持手动更新
- 更新失败时继续使用本地缓存
- 记录上次更新时间

### 4. System Tray (系统托盘 UI)

**依赖库**: `github.com/getlantern/systray`

**托盘图标状态**:
- 灰色: VPN 未连接
- 绿色: 分流已生效
- 黄色: 正在处理中
- 红色: 出现错误

**右键菜单**:
```
[状态: VPN已连接, 分流生效中]  (不可点击,仅显示)
─────────────────────
☑ 启用分流
  更新 IP 数据库
  查看日志
─────────────────────
☑ 开机启动
  退出
```

**通知**: VPN 连接/断开、分流启用/禁用时发送 Windows Toast 通知

### 5. Config Manager (配置管理)

**配置文件**: `configs/config.yaml`

```yaml
# 原始网关 (自动检测并保存)
original_gateway: "192.168.1.1"

# 分流开关
split_tunnel_enabled: true

# 开机启动
auto_start: false

# IP 数据库更新间隔 (天)
update_interval_days: 7

# 上次更新时间
last_update: "2026-05-10T12:00:00Z"

# 日志级别
log_level: "info"
```

## 项目结构

```
anyconnect/
├── cmd/
│   └── main.go                 # 程序入口, 管理员权限检查
├── internal/
│   ├── monitor/
│   │   └── vpn.go              # VPN 状态监控
│   ├── route/
│   │   └── manager.go          # 路由表操作
│   ├── ipdb/
│   │   └── updater.go          # IP 数据库下载/解析/更新
│   ├── tray/
│   │   └── tray.go             # 系统托盘 UI
│   └── config/
│       └── config.go           # 配置管理
├── configs/
│   └── config.yaml             # 默认配置
├── data/
│   ├── china_ip_list.txt       # 中国 IP 段缓存
│   └── applied_routes.json     # 已应用路由记录(运行时)
├── assets/
│   └── icons/                  # 托盘图标资源
├── go.mod
├── go.sum
└── Makefile                    # 构建脚本
```

## 运行流程

1. 启动 → 检查管理员权限(不足则提示提升)
2. 加载配置 → 读取/初始化中国 IP 列表
3. 检查残留路由 → 清理异常退出遗留的路由
4. 记录原始网关 → 保存到配置
5. 启动系统托盘 → 显示图标和菜单
6. 启动 VPN 监控循环:
   - 检测到 VPN 连接 → 批量添加路由 → 发送通知
   - 检测到 VPN 断开 → 清理路由 → 发送通知
7. 退出时 → 清理所有路由 → 保存配置

## 管理员权限处理

程序启动时检测是否以管理员身份运行:
- 是: 正常启动
- 否: 使用 `ShellExecute` 以 "runas" 动词重新启动自身

## 验证方式

1. 编译运行程序,确认托盘图标正常显示
2. 连接 AnyConnect VPN,确认程序检测到连接并添加路由
3. 用 `route print` 验证国内 IP 段路由已添加
4. 访问国内网站(如 baidu.com),确认速度正常
5. 断开 VPN,确认路由被清理
6. 测试异常退出后重启的路由清理功能
