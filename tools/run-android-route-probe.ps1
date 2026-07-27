param(
    [string]$AdbPath = "",
    [string]$DeviceSerial = "",
    [string]$MainApk = "",
    [string]$HelperApk = "",
    [string]$OutputDir = "",
    [int]$TimeoutSeconds = 240,
    [switch]$UninstallFirst,
    [switch]$NoAutoClick
)

$ErrorActionPreference = "Stop"

$MainPackage = "com.msitools.anyconnectmobile.routeprobe"
$HelperPackage = "com.msitools.anyconnectmobile.routeprobesender"
$MainActivity = "com.msitools.anyconnectmobile.routeprobe/.ProbeActivity"
$LatestReport = "files/reports/latest.json"

$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ($MainApk -eq "") {
    $MainApk = Join-Path $root "artifacts\AnyConnectRouteProbe.apk"
}
if ($HelperApk -eq "") {
    $HelperApk = Join-Path $root "artifacts\AnyConnectRouteProbeSender.apk"
}
if ($OutputDir -eq "") {
    $OutputDir = Join-Path $root "artifacts\android-auto-runs"
}

function Resolve-Adb {
    param([string]$RequestedPath)

    if ($RequestedPath -ne "") {
        if (!(Test-Path -LiteralPath $RequestedPath -PathType Leaf)) {
            throw "adb not found: $RequestedPath"
        }
        return (Resolve-Path -LiteralPath $RequestedPath).Path
    }

    $candidates = @(
        "D:\Android\Sdk\platform-tools\adb.exe",
        (Join-Path $env:LOCALAPPDATA "Android\Sdk\platform-tools\adb.exe")
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return $candidate
        }
    }

    $command = Get-Command adb.exe -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }
    throw "adb.exe not found. Install Android platform-tools or pass -AdbPath."
}

function Invoke-Adb {
    param([Parameter(ValueFromRemainingArguments=$true)][string[]]$Args)

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        $output = & $script:Adb -s $script:Device @Args 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "adb $($Args -join ' ') failed: $($output -join "`n")"
        }
        return $output
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
}

function Invoke-AdbAllowFailure {
    param([Parameter(ValueFromRemainingArguments=$true)][string[]]$Args)

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        & $script:Adb -s $script:Device @Args 2>&1 | Out-Null
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
}

function Select-Device {
    param([string]$RequestedSerial)

    if ($RequestedSerial -ne "") {
        return $RequestedSerial
    }

    $lines = & $script:Adb devices
    if ($LASTEXITCODE -ne 0) {
        throw "adb devices failed."
    }
    $devices = @()
    foreach ($line in $lines) {
        if ($line -match "^(\S+)\s+device$") {
            $devices += $Matches[1]
        }
    }
    if ($devices.Count -eq 0) {
        throw "No adb device is connected. Enable USB debugging and accept the RSA prompt."
    }
    if ($devices.Count -gt 1) {
        throw "Multiple adb devices found: $($devices -join ', '). Pass -DeviceSerial."
    }
    return $devices[0]
}

function Assert-File {
    param(
        [Parameter(Mandatory=$true)][string]$Path,
        [Parameter(Mandatory=$true)][string]$Name
    )

    if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Name not found: $Path"
    }
}

function Get-UiXml {
    $remote = "/sdcard/route_probe_uidump.xml"
    Invoke-AdbAllowFailure shell rm -f $remote
    Invoke-Adb shell uiautomator dump $remote | Out-Null
    $xml = Invoke-Adb shell cat $remote
    return ($xml -join "`n")
}

function Get-CenterFromBounds {
    param([string]$Bounds)

    if ($Bounds -notmatch "\[(\d+),(\d+)\]\[(\d+),(\d+)\]") {
        return $null
    }
    return [pscustomobject]@{
        X = [int](([int]$Matches[1] + [int]$Matches[3]) / 2)
        Y = [int](([int]$Matches[2] + [int]$Matches[4]) / 2)
    }
}

function Click-Text {
    param(
        [Parameter(Mandatory=$true)][string[]]$Texts,
        [int]$WaitSeconds = 15
    )

    $deadline = (Get-Date).AddSeconds($WaitSeconds)
    while ((Get-Date) -lt $deadline) {
        $xml = Get-UiXml
        foreach ($text in $Texts) {
            $escaped = [regex]::Escape($text)
            $pattern = '<node[^>]*(text|content-desc)="' + $escaped + '"[^>]*bounds="([^"]+)"'
            $match = [regex]::Match($xml, $pattern)
            if (!$match.Success) {
                $pattern = '<node[^>]*bounds="([^"]+)"[^>]*(text|content-desc)="' + $escaped + '"'
                $match = [regex]::Match($xml, $pattern)
                if ($match.Success) {
                    $bounds = $match.Groups[1].Value
                }
            } else {
                $bounds = $match.Groups[2].Value
            }
            if ($match.Success) {
                $center = Get-CenterFromBounds -Bounds $bounds
                if ($center) {
                    $tapX = $center.X
                    $tapY = $center.Y
                    Invoke-Adb shell input tap $tapX $tapY | Out-Null
                    return $true
                }
            }
        }
        Start-Sleep -Milliseconds 700
    }
    return $false
}

function Read-LatestReport {
    $report = Invoke-Adb shell run-as com.msitools.anyconnectmobile.routeprobe cat files/reports/latest.json
    return ($report -join "`n").Trim()
}

function Wait-Report {
    param([int]$TimeoutSeconds)

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $json = Read-LatestReport
            if ($json.Contains('"gateDecision"')) {
                return $json
            }
        } catch {
            # Report may not exist yet.
        }
        Start-Sleep -Seconds 2
    }
    throw "Timed out waiting for latest.json after $TimeoutSeconds seconds."
}

function Write-ReportSummary {
    param([string]$Json)

    $report = $Json | ConvertFrom-Json
    Write-Host "Gate decision: $($report.gateDecision)"
    foreach ($scenario in $report.scenarios) {
        $line = "{0}: captured={1} bypass={2} direct={3}" -f `
            $scenario.scenario,
            $scenario.capturedChecksPassed,
            $scenario.bypassChecksPassed,
            $scenario.directResponseStatus
        Write-Host $line
        if ($scenario.errorMessage) {
            Write-Host "  error: $($scenario.errorMessage)"
        }
    }
}

$script:Adb = Resolve-Adb -RequestedPath $AdbPath
$script:Device = Select-Device -RequestedSerial $DeviceSerial
Assert-File -Path $MainApk -Name "Main APK"
Assert-File -Path $HelperApk -Name "Helper APK"
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "ADB: $script:Adb"
Write-Host "Device: $script:Device"
Write-Host "Installing main APK: $MainApk"
if ($UninstallFirst) {
    Invoke-AdbAllowFailure uninstall $MainPackage
    Invoke-AdbAllowFailure uninstall $HelperPackage
}
Invoke-Adb install -r $MainApk | Out-Host
Write-Host "Installing helper APK: $HelperApk"
Invoke-Adb install -r $HelperApk | Out-Host

Write-Host "Installed package versions:"
Invoke-Adb shell dumpsys package $MainPackage |
    Select-String -Pattern "versionCode|versionName" |
    ForEach-Object { Write-Host "  main: $($_.Line.Trim())" }
Invoke-Adb shell dumpsys package $HelperPackage |
    Select-String -Pattern "versionCode|versionName|ExternalProbeReceiver|SEND_HELPER_PROBE" |
    ForEach-Object { Write-Host "  helper: $($_.Line.Trim())" }

Invoke-AdbAllowFailure shell run-as $MainPackage rm -f $LatestReport
Invoke-Adb shell am force-stop $MainPackage | Out-Null
Invoke-Adb shell am start -n $MainActivity | Out-Null

$startButtonTextZh = -join @([char]0x5F00, [char]0x59CB, [char]0x5B8C, [char]0x6574, [char]0x63A2, [char]0x9488)
$okTextZh = -join @([char]0x786E, [char]0x5B9A)
$allowTextZh = -join @([char]0x5141, [char]0x8BB8)

if (!$NoAutoClick) {
    Write-Host "Clicking start button if visible..."
    $null = Click-Text -Texts @($startButtonTextZh, "Start full probe") -WaitSeconds 20
    Write-Host "Accepting VPN prompt if visible..."
    $null = Click-Text -Texts @($okTextZh, $allowTextZh, "OK", "Allow") -WaitSeconds 25
} else {
    Write-Host "Auto-click disabled. Tap Start and accept the VPN prompt on the device."
}

$json = Wait-Report -TimeoutSeconds $TimeoutSeconds
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$outFile = Join-Path $OutputDir "route-probe-$timestamp.json"
Set-Content -LiteralPath $outFile -Value $json -Encoding UTF8
Write-Host "Report saved: $outFile"
Write-ReportSummary -Json $json
