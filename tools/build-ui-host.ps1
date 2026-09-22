param(
    [Parameter(Mandatory=$true)][string]$OutputPath,
    [string]$Version = '1.0.5.0',
    [string]$Publisher = ''
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-support.ps1')
Assert-ReleaseVersion $Version
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$source = Join-Path $root 'native\ui-host'
$compiler = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (!(Test-Path -LiteralPath $compiler)) { throw 'The Windows .NET Framework C# compiler is required to build the native UI host.' }
$OutputPath = [IO.Path]::GetFullPath($OutputPath)
$outDir = Split-Path -Parent $OutputPath
New-Item -ItemType Directory -Path $outDir -Force | Out-Null
$assemblyPath = Join-Path $outDir 'ui-host-assembly.cs'
$company = $Publisher.Replace('\','\\').Replace('"','\"').Replace("`r",'\r').Replace("`n",'\n')
$assembly = @"
using System.Reflection;
[assembly: AssemblyTitle("AnyConnect Split Tunnel UI")]
[assembly: AssemblyProduct("AnyConnect Split Tunnel")]
[assembly: AssemblyCompany("$company")]
[assembly: AssemblyVersion("$Version")]
[assembly: AssemblyFileVersion("$Version")]
"@
[IO.File]::WriteAllText($assemblyPath, $assembly, (New-Object Text.UTF8Encoding($false)))
$manifestPath = Join-Path $outDir 'ui-host.manifest'
$manifest = [IO.File]::ReadAllText((Join-Path $source 'app.manifest')).Replace('version="1.0.0.0"', ('version="'+$Version+'"'))
[IO.File]::WriteAllText($manifestPath,$manifest,(New-Object Text.UTF8Encoding($false)))
$arguments = @('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output',('/out:'+$OutputPath),('/win32manifest:'+$manifestPath),('/win32icon:'+(Join-Path $root 'internal\tray\app.ico')),
    '/reference:System.dll','/reference:System.Core.dll','/reference:System.Drawing.dll','/reference:System.Windows.Forms.dll','/reference:System.Windows.Forms.DataVisualization.dll','/reference:System.Web.Extensions.dll',
    ('/resource:'+(Join-Path $source 'Recharge.txt')+',Recharge.txt'), $assemblyPath)
$arguments += @(Get-ChildItem -LiteralPath $source -Filter '*.cs' -File | ForEach-Object {$_.FullName})
Invoke-ReleaseCommand $compiler $arguments
Write-Host "Native UI host built: $OutputPath"
