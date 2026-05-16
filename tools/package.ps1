param(
    [string]$CiscoInstallerPath = "",
    [string]$OutputDir = "",
    [switch]$KeepPayload
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$payloadDir = Join-Path $root "cmd\installer\payload"
$binDir = Join-Path $root "bin"
$appExe = Join-Path $binDir "anyconnect-split.exe"
if ($OutputDir -eq "") {
    $OutputDir = Join-Path $root "artifacts"
}
$setupExe = Join-Path $OutputDir "AnyConnectSplitTunnelSetup.exe"

function Clear-Payload {
    if (!(Test-Path $payloadDir)) {
        New-Item -ItemType Directory -Path $payloadDir | Out-Null
    }
    Get-ChildItem -LiteralPath $payloadDir -Force |
        Where-Object { $_.Name -notin @(".gitignore", "README.txt") } |
        Remove-Item -Recurse -Force
}

function Copy-RequiredPayload {
    New-Item -ItemType Directory -Path (Join-Path $payloadDir "configs") -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $payloadDir "data") -Force | Out-Null

    Copy-Item -LiteralPath $appExe -Destination (Join-Path $payloadDir "anyconnect-split.exe") -Force
    Copy-Item -LiteralPath (Join-Path $root "internal\tray\app.ico") -Destination (Join-Path $payloadDir "app.ico") -Force
    Copy-Item -LiteralPath (Join-Path $root "configs\config.dist.yaml") -Destination (Join-Path $payloadDir "configs\config.yaml") -Force

    Copy-IpDatabaseSeed

    # --- Bundle OpenConnect (含所有 DLL 和 wintun.dll) ---
    $openconnectSrc = "C:\Program Files\OpenConnect"
    $openconnectDst = Join-Path $payloadDir "openconnect"
    if (Test-Path $openconnectSrc) {
        Write-Host "Bundling OpenConnect from $openconnectSrc ..."
        New-Item -ItemType Directory -Force -Path $openconnectDst | Out-Null
        Copy-Item -Path "$openconnectSrc\*" -Destination $openconnectDst -Recurse -Force
        Write-Host "  Done. Files: $((Get-ChildItem $openconnectDst).Count)"
    } else {
        Write-Warning "OpenConnect not found at $openconnectSrc - skipping bundle"
    }

    # --- Bundle sing-box ---
    $toolsDst = Join-Path $payloadDir "tools"
    New-Item -ItemType Directory -Force -Path $toolsDst | Out-Null

    # sing-box: 尝试查找实际二进制（chocolatey shim 指向实际文件）
    $singBoxCandidates = @(
        "C:\ProgramData\chocolatey\lib\sing-box\tools\sing-box-1.12.17-windows-amd64\sing-box.exe",
        "C:\ProgramData\chocolatey\lib\sing-box\tools\sing-box.exe"
    )
    $singBoxSrc = $null
    foreach ($candidate in $singBoxCandidates) {
        if (Test-Path $candidate) {
            $singBoxSrc = $candidate
            break
        }
    }
    # 如果上面都找不到，尝试 which
    if (-not $singBoxSrc) {
        $singBoxCmd = Get-Command "sing-box.exe" -ErrorAction SilentlyContinue
        if ($singBoxCmd) { $singBoxSrc = $singBoxCmd.Source }
    }

    if ($singBoxSrc) {
        Write-Host "Bundling sing-box from $singBoxSrc ..."
        Copy-Item -Path $singBoxSrc -Destination (Join-Path $toolsDst "sing-box.exe") -Force
        $sz = [math]::Round((Get-Item (Join-Path $toolsDst "sing-box.exe")).Length / 1MB, 1)
        Write-Host "  Done. Size: ${sz} MB"
    } else {
        Write-Warning "sing-box.exe not found - skipping bundle"
    }

    if ($CiscoInstallerPath -ne "") {
        $resolvedCisco = (Resolve-Path $CiscoInstallerPath).Path
        $ext = [System.IO.Path]::GetExtension($resolvedCisco).ToLowerInvariant()
        if ($ext -notin @(".msi", ".exe")) {
            throw "CiscoInstallerPath must point to a .msi or .exe file."
        }
        $ciscoDir = Join-Path $payloadDir "cisco"
        New-Item -ItemType Directory -Path $ciscoDir -Force | Out-Null
        Copy-Item -LiteralPath $resolvedCisco -Destination (Join-Path $ciscoDir ([System.IO.Path]::GetFileName($resolvedCisco))) -Force
        Write-Host "Bundled Cisco installer: $resolvedCisco"
    }
}

function Test-IpListFile {
    param([Parameter(Mandatory=$true)][string]$Path)

    if (!(Test-Path $Path)) {
        return $false
    }
    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if ($trimmed -eq "" -or $trimmed.StartsWith("#")) {
            continue
        }
        if ($trimmed -match "^[0-9A-Fa-f:.]+/\d{1,3}$") {
            return $true
        }
    }
    return $false
}

function Copy-IpDatabaseSeed {
    $payloadList = Join-Path $payloadDir "data\china_ip_list.txt"
    $candidateLists = @(
        (Join-Path $root "data\china_ip_list.txt"),
        (Join-Path $binDir "data\china_ip_list.txt")
    )

    foreach ($candidate in $candidateLists) {
        if (Test-IpListFile -Path $candidate) {
            Copy-Item -LiteralPath $candidate -Destination $payloadList -Force
            Write-Host "Bundled IP database: $candidate"
            return
        }
    }

    Write-Host "No valid local IP database found. Downloading APNIC seed..."
    $seedDir = Join-Path $binDir "data"
    go run ./tools/ipdb_seed -out $seedDir
    $seedList = Join-Path $seedDir "china_ip_list.txt"
    if (!(Test-IpListFile -Path $seedList)) {
        throw "Failed to prepare a valid IP database seed."
    }
    Copy-Item -LiteralPath $seedList -Destination $payloadList -Force
    Write-Host "Bundled IP database: $seedList"
}

Push-Location $root
try {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

    go run ./tools/icon_gen

    # Generate Windows resource files (.syso) with embedded icon
    Push-Location (Join-Path $root "cmd")
    go-winres make
    Pop-Location
    Push-Location (Join-Path $root "cmd\installer")
    go-winres make
    Pop-Location

    go build -ldflags "-s -w" -o $appExe ./cmd/

    Clear-Payload
    Copy-RequiredPayload

    go build -ldflags "-s -w -H=windowsgui" -o $setupExe ./cmd/installer

    Write-Host "Installer created: $setupExe"
    if ($CiscoInstallerPath -eq "") {
        Write-Host "No Cisco installer was bundled. Pass -CiscoInstallerPath <official-core-vpn-msi> to include it."
    } else {
        Write-Host "Cisco installer will run with its normal interactive UI during setup."
    }
}
finally {
    if (-not $KeepPayload) {
        Clear-Payload
    }
    Pop-Location
}
