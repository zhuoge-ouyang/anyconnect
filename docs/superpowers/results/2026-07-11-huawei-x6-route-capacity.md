# Huawei X6 Android Route Capacity Gate 0 Result

Status: **IPV4 CAPACITY PASS; IPV6 DATA PATH BLOCKED; HELPER-UID RETEST READY**

Device report recorded at: 2026-07-12 12:32:50 +08:00

## Preserved Huawei report

- Report: `artifacts/android-route-probe-huawei-x6.json`
- Report SHA-256:
  `E0F250438F4F9BAD17DD697DE6FAFF75630D25E06A5C348DB56EF44A301C3A4E`
- Device: Huawei `ICL-AL20`
- Android API: 31
- ABI: `arm64-v8a`, `armeabi-v7a`, `armeabi`
- Page size: 4096 bytes
- Firmware fingerprint:
  `HUAWEI/ICL-AL20/HWICL:12/HUAWEIICL-AL20/104.3.0.200C00:user/release-keys`

The embedded IP database exactly matched the frozen Gate 0 input:

- SHA-256:
  `00CFDE18AFC0C0C7B5E46DF143BD47141F7F22A83EEE4DF24C722806ADDE3E89`
- Raw records: 10826
- IPv4: 8786 raw, 5488 collapsed, 11953 complement
- IPv6: 2040 raw, 2012 collapsed, 15212 complement

## First scenario measurement

`DOMESTIC_DIRECT_IPV4` used the unchanged `INCLUDES_ONLY` plan:

- Synthetic mappings: 1024
- Includes / builder entries: 1025
- Plan SHA-256:
  `D8C99A130E583038AE850D546BBE633E93F58A068794B50E5F65C99B223D2C4B`
- `Builder.establish()`: `SUCCESS`, 89 ms
- RSS: 225312 KiB before, 225892 KiB after, 226068 KiB high-water
- File descriptors: 103 before, 104 after
- Expected captured `8.8.8.8`: not observed
- Expected bypass `114.114.114.114`: passed
- Direct DNS response: `PASS`
- Reported Gate decision: `BLOCKED`

The report proves that Android accepted and established the complete 1025-entry
IPv4 Builder plan without returning null or throwing. It does not prove that
Huawei has a 1024-route capacity limit. The fixed `8.8.8.8/32` sentinel is the
40th sorted route, so a simple "keep the first 1024 and drop the last route"
explanation is inconsistent with this plan.

## Corrected Huawei run

Device report recorded at: 2026-07-12 14:52:13 +08:00

- Report: `artifacts/android-route-probe-huawei-x6-ipv4-pass-ipv6-blocked.json`
- Report SHA-256:
  `D0FF540A2B9C601AF5F38CD537CB171232B0F8BA57B387A7D382B963B89EEE60`
- Device: Huawei `ICL-AL20`, Android API 31
- Frozen IPDB hash and all frozen counts matched the original Gate 0 input.

`DOMESTIC_DIRECT_IPV4` now passed with the exact 1025-entry plan:

- `Builder.establish()`: `SUCCESS`, 82 ms
- Captured target `8.8.8.8`: passed
- Direct target `114.114.114.114`: passed

This proves that the Huawei X6 accepts the exact 1025 IPv4 host routes and that
the repaired probe can capture the expected IPv4 TUN packet.

`DOMESTIC_DIRECT_IPV6` established its exact 1025-entry plan in 90 ms, and the
physical IPv6 direct-DNS check passed. Its captured target
`2001:4860:4860::8888` did not reach TUN. The reader was healthy (`readCount=5`,
`parsedCount=5`, no EOF or reader failure), so this is an IPv6 path-selection
failure, not the old reader-start race. Gate 0 remains blocked until the next
diagnostic run identifies whether the application's IPv6 UDP socket selected
the VPN source address.

## Probe defect found after the run

The first APK sent its only captured-path datagram immediately after
`Builder.establish()` and then only waited for that already-sent packet. It had
no observable barrier proving that:

1. the TUN reader thread had started;
2. the probe VPN was the current UID's active network; and
3. the active VPN LinkProperties contained a route covering the captured target.

The TUN reader also silently converted an active EOF or an unchecked reader
failure into the same `capturedChecksPassed=false` result. Consequently the
first report cannot distinguish a real route failure from a probe-readiness
race or reader failure. Gate 1 remains stopped, but route capacity is not yet
adjudicated from this run.

## Corrected fixed APK

- Fixed APK: `artifacts/AnyConnectRouteProbe.apk`
- Package: `com.msitools.anyconnectmobile.routeprobe`
- Version: code 3, name `0.1.2-gate0-ipv6-source-diagnostic`
- APK size: 1024519 bytes
- APK SHA-256:
  `2475BA6D03F80F9900948BE49C5976642199E2702831CADE2F111DABAFD960DE`
- Signature: same Android Debug certificate as version 1; APK Signature Scheme v2 verified
- Signing certificate SHA-256:
  `0252456256883F2CB865B2CA658C0EB34B356472D50395BD2F733B5764830D8C`
- Previous fixed APK backup:
  `artifacts/backups/AnyConnectRouteProbe-20260712-130846.apk`
- Previous APK SHA-256:
  `85360C4FEBC092500105CEB8D7B1D73A4DE9A9FE103F6545551829D2CFE3D678`
- Host unit tests: 99/99 passed
- Android lint: 0 errors
- Debug APK assembly: passed

The corrected probe keeps the exact route plans, 1024 synthetic mappings per
requested family, path targets, one captured-path send, and the no-fallback
Gate decision. It does not retry the datagram, reduce route counts, drop IPv6,
switch modes, or substitute a smaller plan.

Before that one send it now waits for the reader-start barrier and observable
owned-VPN route readiness. Each establish is compared with the immediately
preceding `(Network handle, TUN interface name)` baseline so Android 12 may
reuse its VPN NetworkAgent without letting the old TUN satisfy the new check.
Every include route and every API 33+ throw/exclude route must also be present
in the active LinkProperties; the sentinel alone cannot produce a PASS.

A readiness timeout reports VPN count, active interface, complete planned-route
coverage, missing-route samples, matched targets, and missing targets. A later
capture miss reports reader start/finish, read and parse counts, EOF, target,
nonce, exact-match, and reader-failure diagnostics in the existing schema 1
`errorClass` / `errorMessage` fields.

Version 3 adds one non-routing diagnostic field only: on a captured-path miss,
`errorMessage` reports `senderSource=VPN_TUN` or `senderSource=NON_VPN` and the
source address family. It never records the actual physical address, changes
no route, sends no retry, and does not alter Gate decisions.

## Helper-UID diagnostic APK

Version 4 adds the final dual-UID classifier for the Huawei IPv6 miss:

- Main APK: `artifacts/AnyConnectRouteProbe.apk`
- Main package: `com.msitools.anyconnectmobile.routeprobe`
- Main version: code 4, name `0.1.3-gate0-helper-uid-diagnostic`
- Main APK size: 1028251 bytes
- Main APK SHA-256:
  `70DB3995D0575456AC33F3C9D65478093B3A07F9F226862AA199231CED55ABEE`
- Helper APK: `artifacts/AnyConnectRouteProbeSender.apk`
- Helper package: `com.msitools.anyconnectmobile.routeprobesender`
- Helper version: code 1, name `0.1.0-helper-uid-probe`
- Helper APK size: 834757 bytes
- Helper APK SHA-256:
  `E4CE68A1E531C40FC6276A73CF6AD303618CA46099461031B4CADD2D1E2E646F`
- Signature: both APKs use the same Android Debug certificate; APK Signature
  Scheme v2 verified
- Signing certificate SHA-256:
  `0252456256883F2CB865B2CA658C0EB34B356472D50395BD2F733B5764830D8C`
- Previous main APK backup:
  `artifacts/backups/AnyConnectRouteProbe-20260712-182334.apk`
- Host unit tests: 103/103 passed (`route-probe` 100, helper 3)
- Android lint: 0 errors
- Debug APK assembly: passed

The helper app is a separate package and therefore a separate Android UID. On
a captured-path miss, the main VPN app asks the helper provider to send the
same destination with a fresh nonce. The provider is protected by a signature
permission declared by the main APK, and the helper returns only status/error
metadata, never a physical source address.

The v4 diagnostic still keeps the original Gate decision strict. If the main
UID packet misses but the helper UID packet is captured, the report remains
`BLOCKED` and adds `helperSend=SUCCESS helperCaptured=true`; this classifies
the miss as VPN-owner-UID-specific behavior. If the helper UID packet also
misses, the report adds `helperSend=SUCCESS helperCaptured=false`, which means
Huawei is not routing a normal app UID's IPv6 packet into this strict TUN plan.

## Remaining device step

MuMu Player 12 was connected through `127.0.0.1:7555`; version 4 main APK and
version 1 helper APK installed successfully. Package manager showed the
signature permission granted to the main APK and the helper provider registered
under `com.msitools.anyconnectmobile.routeprobesender.probe`. Its Android 12
virtual network has IPv4 `10.0.2.15` but no IPv6 default route, so its correct
probe result remains `INCONCLUSIVE` with zero scenarios. MuMu can validate APK
installation, provider registration, permissions, UI, and report writing, but
cannot replace the Huawei X6 dual-stack Gate.

Install the fixed version 4 main APK and the helper APK on the Huawei phone,
with the main APK installed first. Run the full probe once on the same physical
dual-stack network and preserve the new JSON report. The IPv6 failure message
will now distinguish VPN-owner-UID behavior from a real normal-app-UID IPv6
routing failure.
