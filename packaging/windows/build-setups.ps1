<#
.SYNOPSIS
  Builds one per-user setup.exe for each Windows architecture in a GoReleaser dist folder.

.DESCRIPTION
  Reads dist/artifacts.json and dist/metadata.json (written by GoReleaser), finds
  stkr.exe and stkrw.exe per architecture, compiles packaging/windows/stkr.iss with
  Inno Setup and writes dist/stkr_<version>_windows_<arch>_setup.exe. With -Checksums
  the SHA-256 of each setup is appended to dist/checksums.txt.

.EXAMPLE
  goreleaser build --snapshot --clean
  ./packaging/windows/build-setups.ps1 -Checksums
#>
[CmdletBinding()]
param(
  [string]$Dist = (Join-Path $PSScriptRoot '..\..\dist'),
  [switch]$Checksums
)
$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$dist = (Resolve-Path $Dist).Path
$iss = Join-Path $PSScriptRoot 'stkr.iss'
$gen = Join-Path $PSScriptRoot 'build.generated.iss'

# Find the Inno Setup compiler (preinstalled on GitHub's windows runners).
$iscc = (Get-Command iscc -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
  foreach ($d in @("${env:ProgramFiles(x86)}", "$env:ProgramFiles", "$env:LOCALAPPDATA\Programs")) {
    $c = Join-Path $d 'Inno Setup 6\ISCC.exe'
    if (Test-Path $c) { $iscc = $c; break }
  }
}
if (-not $iscc) { throw 'Inno Setup 6 (ISCC.exe) not found' }

$version = (Get-Content (Join-Path $dist 'metadata.json') -Raw | ConvertFrom-Json).version
$m = [regex]::Match($version, '^(\d+)\.(\d+)\.(\d+)')
if (-not $m.Success) { throw "cannot read a version number from '$version'" }
$numeric = "$($m.Groups[1].Value).$($m.Groups[2].Value).$($m.Groups[3].Value).0"

$artifacts = Get-Content (Join-Path $dist 'artifacts.json') -Raw | ConvertFrom-Json
$built = @()
foreach ($arch in @('amd64', 'arm64')) {
  $bins = @($artifacts | Where-Object { $_.type -eq 'Binary' -and $_.goos -eq 'windows' -and $_.goarch -eq $arch })
  $stkr = $bins | Where-Object { $_.name -eq 'stkr.exe' } | Select-Object -First 1
  $stkrw = $bins | Where-Object { $_.name -eq 'stkrw.exe' } | Select-Object -First 1
  if (-not $stkr -or -not $stkrw) { Write-Host "skip $arch (no stkr.exe/stkrw.exe)"; continue }
  $name = "stkr_${version}_windows_${arch}_setup"
  $lines = @(
    "#define AppVersion `"$version`"",
    "#define VersionNumeric `"$numeric`"",
    "#define Arch `"$arch`"",
    "#define RepoRoot `"$repo`"",
    "#define StkrExe `"$((Resolve-Path (Join-Path $repo $stkr.path)).Path)`"",
    "#define StkrwExe `"$((Resolve-Path (Join-Path $repo $stkrw.path)).Path)`"",
    "#define OutDir `"$dist`"",
    "#define OutName `"$name`""
  )
  Set-Content -Path $gen -Value $lines -Encoding ASCII
  & $iscc /Q $iss
  if ($LASTEXITCODE -ne 0) { throw "iscc failed for $arch" }
  $built += (Join-Path $dist "$name.exe")
}
Remove-Item $gen -ErrorAction SilentlyContinue
if ($built.Count -eq 0) { throw 'no setup was built' }

if ($Checksums) {
  $sums = Join-Path $dist 'checksums.txt'
  $out = [System.Collections.Generic.List[string]]::new()
  if (Test-Path $sums) { Get-Content $sums | Where-Object { $_.Trim() } | ForEach-Object { $out.Add($_) } }
  foreach ($f in $built) {
    $h = (Get-FileHash $f -Algorithm SHA256).Hash.ToLower()
    $out.Add("$h  $(Split-Path $f -Leaf)")
  }
  # LF line endings, like GoReleaser writes.
  [System.IO.File]::WriteAllText($sums, ($out -join "`n") + "`n")
}
$built | ForEach-Object { Write-Host "built $_" }
