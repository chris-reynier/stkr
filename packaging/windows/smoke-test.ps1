<#
.SYNOPSIS
  Silent install, check, upgrade, uninstall smoke test for the Windows setup.exe.

.DESCRIPTION
  Changes the current user's real PATH and registry for a moment. It is meant for CI
  (windows-latest) and disposable machines, never for a working desktop.
  The data folder is a temporary STKR_HOME that must survive the uninstall untouched.
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory)][string]$Setup
)
$ErrorActionPreference = 'Stop'

function Get-UserPath {
  $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
  try { $v = $k.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) } finally { $k.Close() }
  return [string]$v
}
function Assert($cond, $msg) { if (-not $cond) { throw "FAIL: $msg" } else { Write-Host "ok: $msg" } }
function Wait-Until($cond, $what, $seconds = 60) {
  $end = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $end) { if (& $cond) { return $true }; Start-Sleep -Milliseconds 500 }
  throw "FAIL: timed out waiting for $what"
}

$Setup = (Resolve-Path $Setup).Path
$app = Join-Path $env:LOCALAPPDATA 'Programs\STKR'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$link = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\STKR.lnk'
$logDir = $env:RUNNER_TEMP
if (-not $logDir) { $logDir = $env:TEMP }
$log = Join-Path $logDir 'stkr-setup.log'

# A data folder that must not change.
$home_ = Join-Path ([System.IO.Path]::GetTempPath()) ("stkr-smoke-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $home_ | Out-Null
Set-Content (Join-Path $home_ 'keep.txt') 'user data'
$env:STKR_HOME = $home_
function Get-HomeState { (Get-ChildItem $home_ -Recurse -Force | ForEach-Object { $_.FullName + '|' + $_.Length }) -join "`n" }
$homeBefore = Get-HomeState

$pathBefore = Get-UserPath
Assert (-not ($pathBefore.ToLower().Contains($app.ToLower()))) 'PATH does not contain the install folder before'

function Install-Setup {
  $argList = '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER /TASKS="" /LOG="' + $log + '"'
  $p = Start-Process -FilePath $Setup -ArgumentList $argList -Wait -PassThru
  Assert ($p.ExitCode -eq 0) "setup exit code 0 (got $($p.ExitCode))"
}

# --- install -----------------------------------------------------------------
Install-Setup
foreach ($f in 'stkr.exe', 'stkrw.exe', 'LICENSE', 'README.md', 'install.json', 'docs\user\install.md') {
  Assert (Test-Path (Join-Path $app $f)) "installed $f"
}
$meta = Get-Content (Join-Path $app 'install.json') -Raw | ConvertFrom-Json
Assert ($meta.method -eq 'setup' -and $meta.version) "install.json says setup, version $($meta.version)"
Assert (Test-Path $link) 'Start menu shortcut exists'
Assert (-not (Get-ItemProperty $runKey -Name STKR -ErrorAction SilentlyContinue)) 'no sign-in entry without the autostart task'
$entries = ((Get-UserPath) -split ';') | Where-Object { $_.TrimEnd('\') -ieq $app }
Assert (@($entries).Count -eq 1) 'user PATH holds the install folder once'

$out = & (Join-Path $app 'stkr.exe') version
Assert ($LASTEXITCODE -eq 0) "stkr version runs: $out"
Assert ("$out" -match [regex]::Escape($meta.version)) 'stkr version shows the installed version'

# stkrw --quit with nothing running must be a harmless no-op that leaves the data folder alone.
$q = Start-Process -FilePath (Join-Path $app 'stkrw.exe') -ArgumentList '--quit' -Wait -PassThru
Assert ($q.ExitCode -eq 0) 'stkrw --quit exits 0 when nothing runs'
Assert ((Get-HomeState) -eq $homeBefore) 'data folder untouched by install and --quit'

# --- upgrade in place --------------------------------------------------------
Install-Setup
$entries = ((Get-UserPath) -split ';') | Where-Object { $_.TrimEnd('\') -ieq $app }
Assert (@($entries).Count -eq 1) 'PATH entry is not duplicated by an upgrade'

# --- uninstall ---------------------------------------------------------------
$unins = Join-Path $app 'unins000.exe'
Assert (Test-Path $unins) 'uninstaller exists'
Start-Process -FilePath $unins -ArgumentList '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -Wait | Out-Null
# The first process hands over to a temporary copy and returns early.
Wait-Until { -not (Test-Path (Join-Path $app 'stkr.exe')) } 'program files to be removed' | Out-Null
Wait-Until { -not (Test-Path $app) } 'the install folder to be removed' | Out-Null
Assert (-not (Test-Path $link)) 'Start menu shortcut removed'
Assert ((Get-UserPath) -ceq $pathBefore) 'user PATH is exactly as before the install'
Assert ((Get-HomeState) -eq $homeBefore) 'data folder still untouched after uninstall'
Remove-Item $home_ -Recurse -Force
Write-Host 'installer smoke test passed'
