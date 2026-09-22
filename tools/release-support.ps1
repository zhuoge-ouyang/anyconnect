# Release helpers shared by the packaging entry point and isolated tests.
function Invoke-ReleaseCommand {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE."
    }
}

function Assert-ReleaseVersion {
    param([string]$Version)
    if ($Version -notmatch '^\d+\.\d+\.\d+\.\d+$') {
        throw 'Version must contain four numeric components, e.g. 1.0.1.0.'
    }
    foreach ($part in $Version.Split('.')) {
        $number = 0
        if (![int]::TryParse($part, [ref]$number) -or $number -gt 65535) {
            throw 'Each version component must be between 0 and 65535.'
        }
    }
}

function New-ReleaseResource {
    param([string]$Source, [string]$Destination, [string]$Version, [string]$Publisher)
    Assert-ReleaseVersion $Version
    $resource = Get-Content -LiteralPath $Source -Raw | ConvertFrom-Json
    $sourceDir = Split-Path -Parent $Source
    # go-winres resolves image paths relative to the generated JSON directory.
    $iconName = [IO.Path]::GetFileNameWithoutExtension($Destination) + '.ico'
    Copy-Item -LiteralPath (Join-Path $sourceDir $resource.RT_GROUP_ICON.APP.'0000') `
        -Destination (Join-Path (Split-Path -Parent $Destination) $iconName) -Force
    $resource.RT_GROUP_ICON.APP.'0000' = $iconName
    $resource.RT_MANIFEST.'#1'.'0409'.identity.version = $Version
    $versionInfo = $resource.RT_VERSION.'#1'.'0000'
    $versionInfo.fixed.file_version = $Version
    $versionInfo.fixed.product_version = $Version
    $versionInfo.info.'0409'.FileVersion = $Version
    $versionInfo.info.'0409'.ProductVersion = $Version
    $versionInfo.info.'0409'.CompanyName = $Publisher
    $json = $resource | ConvertTo-Json -Depth 20
    [IO.File]::WriteAllText($Destination, $json, (New-Object Text.UTF8Encoding($false)))
}

function New-ReleaseSigningSettings {
    param(
        [ValidateSet('Unsigned', 'Authenticode')][string]$Mode,
        [string]$Publisher, [string]$CertificateThumbprint,
        [ValidateSet('CurrentUser', 'LocalMachine')][string]$CertificateStore,
        [string]$SignToolPath, [string]$TimestampUrl
    )
    if ($Mode -eq 'Unsigned') {
        if ($CertificateThumbprint -or $SignToolPath -or $TimestampUrl) {
            throw 'Signing parameters require -SigningMode Authenticode.'
        }
        Write-Warning 'Unsigned build: no verified publisher identity; not an antivirus clearance.'
        return [pscustomobject]@{ Mode = $Mode }
    }
    if ([string]::IsNullOrWhiteSpace($Publisher)) {
        throw 'Authenticode requires the real publisher name via -Publisher.'
    }
    if ($CertificateThumbprint -notmatch '^[A-Fa-f0-9]{40}$') {
        throw 'Authenticode requires an exact 40-character certificate thumbprint.'
    }
    $timestampUri = $null
    if (![Uri]::TryCreate($TimestampUrl, [UriKind]::Absolute, [ref]$timestampUri) -or
        $timestampUri.Scheme -notin @('http', 'https')) {
        throw 'An explicit RFC 3161 HTTP(S) timestamp URL is required.'
    }
    $certificate = Get-Item -LiteralPath "Cert:\$CertificateStore\My\$CertificateThumbprint" -ErrorAction Stop
    if (!$certificate.HasPrivateKey -or $certificate.NotBefore -gt (Get-Date) -or $certificate.NotAfter -le (Get-Date)) {
        throw 'Signing certificate must be valid now and have an accessible private key.'
    }
    if ('1.3.6.1.5.5.7.3.3' -notin @($certificate.EnhancedKeyUsageList | ForEach-Object { $_.ObjectId.Value })) {
        throw 'Certificate must have the code-signing extended key usage.'
    }
    if ([string]::IsNullOrWhiteSpace($SignToolPath)) {
        $SignToolPath = (Get-Command signtool.exe -CommandType Application -ErrorAction Stop).Source
    }
    if (!(Test-Path -LiteralPath $SignToolPath -PathType Leaf)) {
        throw "SignTool not found: $SignToolPath"
    }
    return [pscustomobject]@{
        Mode = $Mode; Tool = $SignToolPath; Thumbprint = $CertificateThumbprint
        Store = $CertificateStore; TimestampUrl = $TimestampUrl
    }
}

function Invoke-ReleaseSigning {
    param([string]$Path, $Settings)
    if ($Settings.Mode -eq 'Unsigned') { return }
    $signArgs = @('sign', '/sha1', $Settings.Thumbprint, '/s', 'My', '/fd', 'SHA256',
        '/tr', $Settings.TimestampUrl, '/td', 'SHA256')
    if ($Settings.Store -eq 'LocalMachine') { $signArgs += '/sm' }
    Invoke-ReleaseCommand $Settings.Tool ($signArgs + @($Path))
    Invoke-ReleaseCommand $Settings.Tool @('verify', '/pa', '/all', '/tw', $Path)
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid' -or !$signature.SignerCertificate -or
        $signature.SignerCertificate.Thumbprint -ne $Settings.Thumbprint -or
        !$signature.TimeStamperCertificate) {
        throw "Release signature or timestamp verification failed: $Path"
    }
}

function Write-ReleaseManifest {
    param([string]$PayloadDir, [string]$Destination, [string]$Version, [string]$SigningMode)
    $payloadRoot = (Resolve-Path -LiteralPath $PayloadDir).Path.TrimEnd('\') + '\'
    $entries = @(Get-ChildItem -LiteralPath $PayloadDir -Recurse -File |
        Where-Object { $_.Name -notin @('.gitignore', 'README.txt') } |
        Sort-Object FullName | ForEach-Object {
            $signature = $null
            if ($_.Extension -in @('.exe', '.dll')) {
                $signature = (Get-AuthenticodeSignature -LiteralPath $_.FullName).Status.ToString()
            }
            [ordered]@{
                path = $_.FullName.Substring($payloadRoot.Length).Replace('\', '/')
                bytes = $_.Length
                sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
                authenticode = $signature
            }
        })
    $manifest = [ordered]@{
        version = $Version; signingMode = $SigningMode
        generatedAtUtc = [DateTime]::UtcNow.ToString('o'); files = $entries
    }
    [IO.File]::WriteAllText($Destination, ($manifest | ConvertTo-Json -Depth 6), (New-Object Text.UTF8Encoding($false)))
}
