$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-support.ps1')
$root = Split-Path -Parent $PSScriptRoot
$testDir = Join-Path $root ('artifacts\package-staging\release-tests-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testDir | Out-Null
$script:checks = 0

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (!$Condition) { throw $Message }
    $script:checks++
}
function Assert-Throws {
    param([scriptblock]$Action, [string]$Pattern)
    $message = ''
    try { & $Action | Out-Null } catch { $message = $_.Exception.Message }
    Assert-True ($message -like $Pattern) "Expected error matching '$Pattern', got '$message'"
}

Assert-ReleaseVersion '1.0.1.0'
Assert-ReleaseVersion '65535.0.0.0'
foreach ($invalid in @('', '1.2.3', '1.2.3.4.5', '-1.0.0.0', '65536.0.0.0', '9999999999999.0.0.0')) {
    Assert-Throws { Assert-ReleaseVersion $invalid } '*version*'
}
$unsigned = New-ReleaseSigningSettings -Mode Unsigned
Assert-True ($unsigned.Mode -eq 'Unsigned') 'Unsigned must be labeled explicitly.'
Assert-Throws { New-ReleaseSigningSettings -Mode Unsigned -CertificateThumbprint ('A' * 40) } '*require*Authenticode*'
Assert-Throws { New-ReleaseSigningSettings -Mode Authenticode } '*real publisher*'
Assert-Throws { New-ReleaseSigningSettings -Mode Authenticode -Publisher 'Test Publisher' } '*thumbprint*'
Assert-Throws { New-ReleaseSigningSettings -Mode Authenticode -Publisher 'Test Publisher' -CertificateThumbprint ('A' * 40) -TimestampUrl 'file:///tmp' } '*timestamp URL*'

$resource = Join-Path $testDir 'resource.json'
$source = Join-Path $root 'cmd\winres\winres.json'
$before = (Get-FileHash -LiteralPath $source).Hash
$testPublisher = -join [char[]]@(0x6D4B, 0x8BD5, 0x53D1, 0x5E03, 0x8005)
New-ReleaseResource -Source $source -Destination $resource -Version '1.2.3.4' -Publisher $testPublisher
$data = Get-Content -LiteralPath $resource -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-True ($data.RT_VERSION.'#1'.'0000'.info.'0409'.CompanyName -eq $testPublisher) 'Publisher mismatch.'
Assert-True ($data.RT_VERSION.'#1'.'0000'.fixed.file_version -eq '1.2.3.4') 'Version mismatch.'
Assert-True ($data.RT_MANIFEST.'#1'.'0409'.identity.version -eq '1.2.3.4') 'Manifest mismatch.'
Assert-True (Test-Path -LiteralPath (Join-Path $testDir $data.RT_GROUP_ICON.APP.'0000')) 'Relative icon resource must exist.'
Assert-True ((Get-FileHash -LiteralPath $source).Hash -eq $before) 'Source resource was modified.'

$hostExe = (Get-Process -Id $PID).Path
Invoke-ReleaseCommand $hostExe @('-NoProfile', '-Command', 'exit 0')
Assert-Throws { Invoke-ReleaseCommand $hostExe @('-NoProfile', '-Command', 'exit 23') } '*exit code 23*'

# Signature validation is isolated: no certificate creation, import or network calls.
$script:commands = @()
function Invoke-ReleaseCommand {
    param([string]$Command, [string[]]$Arguments)
    $script:commands += ,$Arguments
}
$script:fakeSignature = [pscustomobject]@{
    Status = 'Valid'; SignerCertificate = [pscustomobject]@{ Thumbprint = 'A' * 40 }
    TimeStamperCertificate = [pscustomobject]@{ Subject = 'test timestamp' }
}
function Get-AuthenticodeSignature { param([string]$LiteralPath) return $script:fakeSignature }
$settings = [pscustomobject]@{
    Mode = 'Authenticode'; Tool = 'test-signtool'; Thumbprint = 'A' * 40
    Store = 'LocalMachine'; TimestampUrl = 'https://timestamp.example.test'
}
Invoke-ReleaseSigning -Path 'test application.exe' -Settings $settings
Assert-True ($script:commands.Count -eq 2) 'Expected signing followed by verification.'
Assert-True ($script:commands[0] -contains '/sm') 'Machine-store selection missing.'
Assert-True ($script:commands[0] -contains '/tr') 'RFC 3161 timestamp missing.'
Assert-True ($script:commands[1] -contains '/pa') 'Authenticode verification missing.'
$script:fakeSignature.TimeStamperCertificate = $null
Assert-Throws { Invoke-ReleaseSigning 'test.exe' $settings } '*verification failed*'
$script:fakeSignature.TimeStamperCertificate = 'timestamp'
$script:fakeSignature.SignerCertificate.Thumbprint = 'B' * 40
Assert-Throws { Invoke-ReleaseSigning 'test.exe' $settings } '*verification failed*'
$script:fakeSignature.Status = 'NotTrusted'
Assert-Throws { Invoke-ReleaseSigning 'test.exe' $settings } '*verification failed*'

$payload = Join-Path $testDir 'payload'
New-Item -ItemType Directory -Path $payload | Out-Null
Copy-Item -LiteralPath $source -Destination (Join-Path $payload 'resource.json')
$manifest = Join-Path $testDir 'manifest.json'
Write-ReleaseManifest -PayloadDir $payload -Destination $manifest -Version '1.2.3.4' -SigningMode Unsigned
$inventory = Get-Content -LiteralPath $manifest -Raw | ConvertFrom-Json
Assert-True ($inventory.files.Count -eq 1) 'Payload manifest file count mismatch.'
Assert-True ($inventory.files[0].path -eq 'resource.json') 'Manifest must use relative paths.'
Assert-True ($inventory.files[0].sha256 -eq $before) 'Manifest hash mismatch.'
Write-Host "PASS: $script:checks release checks. Isolated evidence: $testDir"
