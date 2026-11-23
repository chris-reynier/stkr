<#
.SYNOPSIS
  Builds a portable STKR (stkr.exe + stkrw.exe, no installer) from this repository.

.DESCRIPTION
  Builds both programs for this PC with the same flags, icon and version resources as
  a release, and puts them in one folder with the user guide. Nothing is installed:
  no PATH entry, no Start menu entry, no sign-in entry. docs/user/portable.md lists
  the optional steps that add those by hand.

  Programs built on your own PC carry no "downloaded from the internet" mark, so
  SmartScreen does not ask before they run.

  The version is the git tag on HEAD, else the latest tag before it (with a warning),
  so in-app updates treat the build as a release and not as a development build.

  Needs Go (go.mod names the toolchain; Go downloads it if yours is older) and git.

.PARAMETER Out
  Folder to build into. Created if missing; existing STKR files in it are replaced.
  Default: %LOCALAPPDATA%\STKR-portable

.PARAMETER Arch
  amd64 or arm64. Default: the architecture of this PC.

.PARAMETER Version
  Version to stamp, for example 0.2.0. Default: from git tags (see above).

.EXAMPLE
  git clone https://github.com/chris-reynier/stkr; cd stkr; git checkout v0.2.0
  ./packaging/windows/build-portable.ps1
#>
[CmdletBinding()]
param(
  [string]$Out = (Join-Path $env:LOCALAPPDATA 'STKR-portable'),
  [ValidateSet('', 'amd64', 'arm64')][string]$Arch = '',
  [string]$Version = ''
)
$ErrorActionPreference = 'Stop'

function Invoke-Native([string]$What, [scriptblock]$Run) {
  & $Run
  if ($LASTEXITCODE -ne 0) { throw "$What failed (exit $LASTEXITCODE)" }
}

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $repo
try {
  foreach ($tool in 'go', 'git') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not on PATH" }
  }

  if (-not $Arch) {
    $Arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
  }

  if (-not $Version) {
    $tag = git describe --tags --exact-match 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $tag) {
      $tag = git describe --tags --abbrev=0 2>$null
      if ($LASTEXITCODE -ne 0 -or -not $tag) { throw 'no git tag found; pass -Version (for example -Version 0.2.0)' }
      Write-Warning "HEAD is not tagged; stamping $tag. For an exact release build run: git checkout $tag"
    }
    $Version = "$tag".Trim().TrimStart('v')
  }
  $m = [regex]::Match($Version, '^(\d+)\.(\d+)\.(\d+)')
  if (-not $m.Success) { throw "'$Version' is not a version number like 0.2.0" }
  $numeric = "$($m.Groups[1].Value).$($m.Groups[2].Value).$($m.Groups[3].Value).0"

  # A running copy in the target folder locks its .exe files.
  $outFull = [System.IO.Path]::GetFullPath($Out)
  $running = Get-Process stkr, stkrw -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -and ([System.IO.Path]::GetDirectoryName($_.Path) -eq $outFull.TrimEnd('\')) }
  if ($running) { throw "STKR is running from $outFull. Quit it first (tray icon, Quit), then run this again." }

  $commit = (git rev-parse --short HEAD).Trim()
  $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
  $bi = 'github.com/chris-reynier/stkr/internal/buildinfo'
  $x = "-X $bi.Version=$Version -X $bi.Commit=$commit -X $bi.Date=$date"

  Write-Host "Building STKR $Version ($commit) for windows/$Arch into $outFull"

  # Icon and version resources, as in .goreleaser.yaml (generated, gitignored).
  Invoke-Native 'genicon' { go run ./tools/genicon packaging/windows/stkr.ico }
  $res = @('--arch', $Arch, '--manifest', 'none', '--icon', 'packaging/windows/stkr.ico',
    '--file-version', $numeric, '--product-version', $numeric, '--product-name', 'STKR', '--copyright', 'MIT License')
  Invoke-Native 'go-winres (stkr)' { go tool go-winres simply --out cmd/stkr/rsrc @res --file-description 'STKR command line' --original-filename stkr.exe }
  Invoke-Native 'go-winres (stkrw)' { go tool go-winres simply --out cmd/stkrw/rsrc @res --file-description 'STKR desktop app' --original-filename stkrw.exe }

  New-Item -ItemType Directory -Force $outFull | Out-Null
  $oldEnv = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
  $env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = $Arch
  try {
    Invoke-Native 'go build stkr' { go build -trimpath -ldflags "-s -w $x" -o (Join-Path $outFull 'stkr.exe') ./cmd/stkr }
    Invoke-Native 'go build stkrw' { go build -trimpath -ldflags "-s -w -H windowsgui $x" -o (Join-Path $outFull 'stkrw.exe') ./cmd/stkrw }
  } finally {
    foreach ($k in @($oldEnv.Keys)) {
      if ($null -eq $oldEnv[$k]) { Remove-Item "env:$k" -ErrorAction SilentlyContinue } else { Set-Item "env:$k" -Value $oldEnv[$k] }
    }
  }

  Copy-Item README.md, LICENSE -Destination $outFull -Force
  $docs = Join-Path $outFull 'docs\user'
  if (Test-Path $docs) { Remove-Item $docs -Recurse -Force -Confirm:$false }
  New-Item -ItemType Directory -Force (Split-Path $docs) | Out-Null
  Copy-Item docs\user -Destination $docs -Recurse
  Copy-Item docs\user\portable.md -Destination (Join-Path $outFull 'PORTABLE.md') -Force
} finally {
  Pop-Location
}

if ($Arch -eq 'amd64' -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
  & (Join-Path $outFull 'stkr.exe') version
}
Write-Host ''
Write-Host "Done. Start $outFull\stkrw.exe; the STKR icon appears next to the clock."
Write-Host "Optional setup (Start menu, sign-in, PATH): $outFull\PORTABLE.md"
