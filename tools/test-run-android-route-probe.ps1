$ErrorActionPreference = "Stop"

$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$scriptPath = Join-Path $root "tools\run-android-route-probe.ps1"

function Assert-True {
    param(
        [Parameter(Mandatory=$true)][bool]$Condition,
        [Parameter(Mandatory=$true)][string]$Message
    )

    if (-not $Condition) {
        throw $Message
    }
}

Assert-True -Condition (Test-Path -LiteralPath $scriptPath -PathType Leaf) -Message "Automation script is missing."

$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseInput(
    (Get-Content -Raw -LiteralPath $scriptPath),
    [ref]$tokens,
    [ref]$errors
) | Out-Null
if ($errors.Count -ne 0) {
    $errors | ForEach-Object { Write-Host $_.Message }
}
Assert-True -Condition ($errors.Count -eq 0) -Message "Automation script has PowerShell parse errors."

$source = Get-Content -Raw -LiteralPath $scriptPath
Assert-True -Condition ($source.Contains("install -r")) -Message "Script must install APKs with adb install -r."
Assert-True -Condition ($source.Contains("uiautomator dump")) -Message "Script must use UIAutomator for button clicks."
Assert-True -Condition ($source.Contains("run-as com.msitools.anyconnectmobile.routeprobe cat files/reports/latest.json")) -Message "Script must read latest.json through run-as."
Assert-True -Condition ($source.Contains("com.msitools.anyconnectmobile.routeprobe/.ProbeActivity")) -Message "Script must launch ProbeActivity."
Assert-True -Condition ($source.Contains("AnyConnectRouteProbeSender.apk")) -Message "Script must install the helper APK."
Assert-True -Condition ($source.Contains("[int]`$TimeoutSeconds = 240")) -Message "Script default timeout must cover slow Huawei helper-broadcast runs."
Assert-True -Condition ($source.Contains("previousErrorActionPreference")) -Message "Script must capture adb stderr without PowerShell 5 NativeCommandError aborts."

Write-Host "OK: Android route probe automation script checks passed."
