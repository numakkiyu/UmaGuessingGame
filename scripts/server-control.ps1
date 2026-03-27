param(
  [ValidateSet('start', 'stop', 'status', 'restart')]
  [string]$Action = 'status'
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repoRoot 'data\runtime'
$pidFile = Join-Path $runtimeDir 'app-server.pid'
$outLog = Join-Path $repoRoot 'prod-server.out.log'
$errLog = Join-Path $repoRoot 'prod-server.err.log'
$nodeCmd = (Get-Command node.exe).Source
$nextBin = Join-Path $repoRoot 'node_modules\next\dist\bin\next'
$buildIdFile = Join-Path $repoRoot '.next\BUILD_ID'
$port = 3000

function Ensure-RuntimeDir {
  if (-not (Test-Path $runtimeDir)) {
    New-Item -ItemType Directory -Path $runtimeDir | Out-Null
  }
}

function Get-TrackedProcess {
  if (-not (Test-Path $pidFile)) {
    return $null
  }

  $pidValue = (Get-Content $pidFile -Raw).Trim()
  if (-not $pidValue) {
    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
    return $null
  }

  $process = Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue
  if (-not $process) {
    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
    return $null
  }

  return $process
}

function Get-PortOwner {
  $matches = netstat -ano | Select-String (':{0}\s+.*LISTENING\s+(\d+)$' -f $port)
  if (-not $matches) {
    return $null
  }

  $pidValue = ($matches | Select-Object -First 1).Matches[0].Groups[1].Value
  if (-not $pidValue) {
    return $null
  }

  return Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue
}

function Get-AppServerProcessIds {
  $escapedRepoRoot = [Regex]::Escape($repoRoot)

  return @(Get-CimInstance Win32_Process -Filter "name = 'node.exe'" |
    Where-Object {
      $_.CommandLine -match $escapedRepoRoot -and $_.CommandLine -match 'next'
    } |
    Select-Object -ExpandProperty ProcessId)
}

function Wait-ForPort {
  param(
    [int]$TimeoutSeconds = 20
  )

  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  do {
    $portOwner = Get-PortOwner
    if ($portOwner) {
      return $portOwner
    }

    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)

  return $null
}

function Show-Status {
  $tracked = Get-TrackedProcess
  $portOwner = Get-PortOwner
  $appServerPids = Get-AppServerProcessIds

  if ($tracked) {
    Write-Output ('tracked_pid={0}' -f $tracked.Id)
  } else {
    Write-Output 'tracked_pid=none'
  }

  if ($portOwner) {
    Write-Output ('port_3000_pid={0}' -f $portOwner.Id)
    Write-Output ('port_3000_name={0}' -f $portOwner.ProcessName)
  } else {
    Write-Output 'port_3000_pid=none'
  }

  if ($appServerPids.Count -gt 0) {
    Write-Output ('app_server_pids={0}' -f ($appServerPids -join ','))
  } else {
    Write-Output 'app_server_pids=none'
  }
}

function Start-AppServer {
  if (-not (Test-Path $nextBin)) {
    throw 'Next start entry not found. Run npm install first.'
  }

  if (-not (Test-Path $buildIdFile)) {
    throw 'Production build not found. Run npm run build first.'
  }

  $existingPids = Get-AppServerProcessIds
  if ($existingPids.Count -gt 0) {
    Write-Output ('server_already_running={0}' -f ($existingPids -join ','))
    return
  }

  Ensure-RuntimeDir

  $portOwner = Get-PortOwner
  if ($portOwner) {
    throw ('Port 3000 is already occupied by process {0}.' -f $portOwner.Id)
  }

  if (Test-Path $outLog) {
    Remove-Item $outLog -Force
  }
  if (Test-Path $errLog) {
    Remove-Item $errLog -Force
  }

  $process = Start-Process -FilePath $nodeCmd -ArgumentList @($nextBin, 'start', '-p', $port.ToString()) -WorkingDirectory $repoRoot -RedirectStandardOutput $outLog -RedirectStandardError $errLog -WindowStyle Hidden -PassThru

  $portOwner = Wait-ForPort
  if ($portOwner) {
    Set-Content -Path $pidFile -Value $portOwner.Id -NoNewline
    Write-Output ('server_started={0}' -f $portOwner.Id)
    return
  }

  $stderrPreview = ''
  if (Test-Path $errLog) {
    $stderrPreview = (Get-Content $errLog -Tail 20) -join [Environment]::NewLine
  }

  if (-not $process.HasExited) {
    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
  }

  throw ("Server failed to start on port {0}. {1}" -f $port, $stderrPreview)
}

function Stop-AppServer {
  $processIds = @()
  $tracked = Get-TrackedProcess
  if ($tracked) {
    $processIds += $tracked.Id
  }

  $processIds += Get-AppServerProcessIds
  $processIds = $processIds | Sort-Object -Unique

  if ($processIds.Count -eq 0) {
    Write-Output 'server_already_stopped=true'
    return
  }

  foreach ($processId in $processIds) {
    Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
  }

  Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
  Write-Output ('server_stopped={0}' -f ($processIds -join ','))
}

switch ($Action) {
  'start' { Start-AppServer }
  'stop' { Stop-AppServer }
  'status' { Show-Status }
  'restart' {
    Stop-AppServer
    Start-AppServer
  }
}
