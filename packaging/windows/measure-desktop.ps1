<#
.SYNOPSIS
  Measures the desktop app (stkrw.exe): cold start and idle memory, panel closed or open.

.DESCRIPTION
  Default (panel closed): runs `stkrw --headless-test` against a temporary data folder and
  a free port. The test mode has no notification-area icon, no windows, no hotkey and never
  opens a browser, so it is safe on a working desktop and on CI. It measures the program as
  it is while the panel is closed (WebView2 starts only when a window is first shown).

  With -Panel: runs the real app (tray icon and a visible panel window, WebView2) with
  desktop.open_panel_on_start, and adds the working set of every child process (the
  WebView2 processes). Run it on CI or a disposable machine, not on a working desktop:
  it shows a window and an icon.

  Start time = process start until the log line "tray ready" (the moment the app adds its
  icon) and until the server answers /healthz.
  Memory = working set (Task Manager "Memory") and private bytes after the idle time.
  The first run after a program was just built or copied can show a much larger working
  set (the system scans the new file); measure twice.
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory)][string]$Stkr,
  [Parameter(Mandatory)][string]$Stkrw,
  [int]$IdleSeconds = 30,
  [switch]$Panel,
  # Warn above these numbers (the targets); fail above twice as much.
  [double]$TargetWorkingSetMB = $(if ($Panel) { 80 } else { 50 }),
  [double]$TargetStartSeconds = 1.5
)
$ErrorActionPreference = 'Stop'
$Stkr = (Resolve-Path $Stkr).Path
$Stkrw = (Resolve-Path $Stkrw).Path

function Get-Descendants([int]$ProcessId) {
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId
  $found = [System.Collections.Generic.List[int]]::new()
  $queue = [System.Collections.Generic.Queue[int]]::new()
  $queue.Enqueue($ProcessId)
  while ($queue.Count -gt 0) {
    $cur = $queue.Dequeue()
    foreach ($c in $all | Where-Object { $_.ParentProcessId -eq $cur }) { $found.Add([int]$c.ProcessId); $queue.Enqueue([int]$c.ProcessId) }
  }
  return $found
}

$home_ = Join-Path ([System.IO.Path]::GetTempPath()) ("stkr-measure-" + [guid]::NewGuid().ToString('N'))
$env:STKR_HOME = $home_
$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start(); $port = $listener.LocalEndpoint.Port; $listener.Stop()
& $Stkr config set port $port | Out-Null
& $Stkr config set update.check false | Out-Null
if ($Panel) { & $Stkr config set desktop.open_panel_on_start true | Out-Null }

$p = $null
try {
  $stkrwArgs = @('-home', $home_)
  if (-not $Panel) { $stkrwArgs = @('--headless-test') + $stkrwArgs }
  $p = Start-Process -FilePath $Stkrw -ArgumentList $stkrwArgs -PassThru
  $start = $p.StartTime.ToUniversalTime()
  $serverAt = $null
  $deadline = (Get-Date).AddSeconds(30)
  while ((Get-Date) -lt $deadline -and -not $serverAt) {
    try {
      $r = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$port/healthz" -TimeoutSec 1
      if ($r.StatusCode -eq 200) { $serverAt = [DateTime]::UtcNow }
    } catch { Start-Sleep -Milliseconds 20 }
  }
  if (-not $serverAt) { throw 'FAIL: the server did not answer' }
  $log = Join-Path $home_ 'logs\desktop.log'
  $trayAt = $null
  foreach ($line in Get-Content $log) {
    if ($line -match '"time":"([^"]+)".*"msg":"tray ready"') { $trayAt = [DateTime]::Parse($Matches[1]).ToUniversalTime(); break }
  }
  if (-not $trayAt) { throw 'FAIL: no "tray ready" line in desktop.log' }
  $trayS = ($trayAt - $start).TotalSeconds
  $serverS = ($serverAt - $start).TotalSeconds

  Start-Sleep -Seconds $IdleSeconds
  $p.Refresh()
  $wsMB = $p.WorkingSet64 / 1MB
  $privMB = $p.PrivateMemorySize64 / 1MB
  $kids = @(Get-Descendants $p.Id)
  $kidsWsMB = 0.0
  foreach ($k in $kids) { $kp = Get-Process -Id $k -ErrorAction SilentlyContinue; if ($kp) { $kidsWsMB += $kp.WorkingSet64 / 1MB } }
  $totalMB = $wsMB + $kidsWsMB
  $state = if ($Panel) { 'panel open' } else { 'panel closed' }

  $rows = @(
    '| Measure | Value | Target |',
    '|---|---|---|',
    ('| Cold start to the tray icon | {0:N2} s | < {1} s |' -f $trayS, $TargetStartSeconds),
    ('| Cold start to the server answering | {0:N2} s | |' -f $serverS),
    ('| stkrw working set after {0} s, {1} | {2:N1} MB | |' -f $IdleSeconds, $state, $wsMB),
    ('| stkrw private bytes | {0:N1} MB | |' -f $privMB),
    ('| Child processes (WebView2) | {0}, working set {1:N1} MB | |' -f $kids.Count, $kidsWsMB),
    ('| Working set, stkrw and its children | {0:N1} MB | < {1} MB |' -f $totalMB, $TargetWorkingSetMB)
  )
  $rows | ForEach-Object { Write-Host $_ }
  if ($env:GITHUB_STEP_SUMMARY) { Add-Content $env:GITHUB_STEP_SUMMARY ("### stkrw, $state`n" + ($rows -join "`n")) }

  if ($trayS -gt $TargetStartSeconds) { Write-Host "::warning::tray start $trayS s is above the target $TargetStartSeconds s" }
  if ($totalMB -gt $TargetWorkingSetMB) { Write-Host "::warning::working set $totalMB MB is above the target $TargetWorkingSetMB MB" }
  if ($trayS -gt 2 * $TargetStartSeconds -or $totalMB -gt 2 * $TargetWorkingSetMB) { throw 'FAIL: more than twice the target' }
}
finally {
  if ($p -and -not $p.HasExited) {
    & $Stkrw --quit -home $home_
    if (-not $p.WaitForExit(10000)) { $p.Kill() }
  }
  Start-Sleep -Milliseconds 500
  Remove-Item -Recurse -Force $home_ -ErrorAction SilentlyContinue
}
