# 影策本地二次开发重启。
# 前端 5178，后端 8178，避开被其他项目占用的 3000/8080。
# 数据目录、Go 缓存、Vite 代理约定见 docs/content/docs/backend/local-development.mdx

[CmdletBinding()]
param(
    [switch]$Check,
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Remaining
)

$ErrorActionPreference = "Stop"
$WebPort = 5178
$BackendPort = 8178

function ConvertTo-ArgSet([string[]]$Values) {
    $set = @{}
    foreach ($value in @($Values)) {
        if ($value) { $set[$value.ToLowerInvariant()] = $true }
    }
    return $set
}

$argSet = ConvertTo-ArgSet $Remaining
if ($argSet.ContainsKey("--help") -or $argSet.ContainsKey("-h") -or $argSet.ContainsKey("/?")) {
    Write-Host "restart.bat [--check]"
    Write-Host ("  前端 http://localhost:{0}" -f $WebPort)
    Write-Host ("  后端 http://127.0.0.1:{0}" -f $BackendPort)
    Write-Host "  本项目占用指定端口则结束并重启；其他项目占用则写入 .local\restart.log 后退出。"
    exit 0
}
if ($argSet.ContainsKey("--check") -or $argSet.ContainsKey("-check") -or $argSet.ContainsKey("/check")) {
    $Check = $true
}
$unknown = @($Remaining | Where-Object {
    $_ -and ($_ -notin @("--check", "-check", "/check", "--help", "-h", "/?", "-Check"))
})
if ($unknown.Count -gt 0) {
    throw ("未知参数: {0}" -f ($unknown -join " "))
}

. (Join-Path $PSScriptRoot "windows-proxy.ps1")

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$repoKey = [IO.Path]::GetFullPath($repoRoot).TrimEnd('\').ToLowerInvariant().Replace('/', '\')
$backendDir = Join-Path $repoRoot "backend"
$webDir = Join-Path $repoRoot "web"
$dataDir = Join-Path $repoRoot ".local\project-workbench-debug"
$goBuildCache = Join-Path $repoRoot ".local\cache\go-build"
$goModuleCache = Join-Path $repoRoot ".local\cache\go-mod"
$localDir = Join-Path $repoRoot ".local"
$logFile = Join-Path $localDir "restart.log"
$markerFile = Join-Path $localDir "restart-dev.json"
$agentRuntimeDir = Join-Path $repoRoot "backend\agent-runtime\pi"

New-Item -ItemType Directory -Force -Path $localDir | Out-Null

function Write-RestartLog {
    param(
        [ValidateSet("INFO", "WARN", "ERROR")]
        [string]$Level,
        [string]$Message
    )
    $line = "[{0}] [{1}] {2}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $Level, $Message
    $color = @{ INFO = "Gray"; WARN = "Yellow"; ERROR = "Red" }[$Level]
    Write-Host $line -ForegroundColor $color
    Add-Content -LiteralPath $logFile -Value $line -Encoding UTF8
}

Set-Content -LiteralPath $logFile -Value ("# 影策 restart  {0}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss")) -Encoding UTF8

function ConvertTo-PowerShellLiteral([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

function Test-ProcessAlive([int]$ProcessId) {
    return $ProcessId -gt 4 -and $null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)
}

function Get-ListeningPids([int]$Port) {
    $ids = New-Object System.Collections.Generic.List[int]
    try {
        foreach ($row in @(Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop)) {
            if (Test-ProcessAlive $row.OwningProcess) { [void]$ids.Add([int]$row.OwningProcess) }
        }
    } catch {}
    if ($ids.Count -eq 0) {
        $pattern = "(?i)TCP\s+\S+:{0}\s+\S+\s+LISTENING\s+(\d+)\s*$" -f $Port
        foreach ($line in @(netstat -ano)) {
            if ($line -match $pattern) {
                $procId = [int]$Matches[1]
                if (Test-ProcessAlive $procId) { [void]$ids.Add($procId) }
            }
        }
    }
    return @($ids | Select-Object -Unique)
}

function Get-ProcessRecord([int]$ProcessId) {
    if (-not (Test-ProcessAlive $ProcessId)) { return $null }
    try {
        $record = Get-CimInstance -ClassName Win32_Process -Filter ("ProcessId={0}" -f $ProcessId) -ErrorAction Stop
        if ($record) { return $record }
    } catch {}
    $proc = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if (-not $proc) { return $null }
    return [pscustomobject]@{
        ProcessId = $ProcessId
        ParentProcessId = 0
        Name = $proc.ProcessName
        ExecutablePath = $proc.Path
        CommandLine = $proc.Path
    }
}

function Test-TextFromRepo([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    $normalized = $Text.ToLowerInvariant().Replace('/', '\')
    return $normalized.Contains($repoKey)
}

function Test-ProcessFromRepo([int]$ProcessId) {
    $seen = New-Object "System.Collections.Generic.HashSet[int]"
    $current = $ProcessId
    while ($current -gt 4 -and $seen.Add($current)) {
        $record = Get-ProcessRecord $current
        if (-not $record) { break }
        $haystack = @($record.CommandLine, $record.ExecutablePath, $record.Name) -join " "
        if (Test-TextFromRepo $haystack) { return $true }
        $name = [string]$record.Name
        if ($name -match '(?i)^(explorer|services|svchost|wininit|winlogon)\.exe$') { break }
        $current = [int]$record.ParentProcessId
    }
    return $false
}

function Get-ProcessSummary([int]$ProcessId) {
    $record = Get-ProcessRecord $ProcessId
    if (-not $record) { return ("PID {0} (无法读取进程信息，按其他项目占用处理)" -f $ProcessId) }
    $command = [string]$record.CommandLine
    if ($command.Length -gt 240) { $command = $command.Substring(0, 240) + "..." }
    if ([string]::IsNullOrWhiteSpace($command)) { $command = [string]$record.ExecutablePath }
    if ([string]::IsNullOrWhiteSpace($command)) { $command = [string]$record.Name }
    return ("PID {0} {1} | {2}" -f $ProcessId, $record.Name, $command)
}

function Get-RepoProcessRoot([int]$ProcessId) {
    $seen = New-Object "System.Collections.Generic.HashSet[int]"
    $current = $ProcessId
    $root = $ProcessId
    while ($current -gt 4 -and $seen.Add($current)) {
        $record = Get-ProcessRecord $current
        if (-not $record) { break }
        if (Test-TextFromRepo ((@($record.CommandLine, $record.ExecutablePath, $record.Name) -join " "))) {
            $root = $current
        } else {
            break
        }
        $name = [string]$record.Name
        if ($name -match '(?i)^(explorer|services|svchost|wininit|winlogon)\.exe$') { break }
        $current = [int]$record.ParentProcessId
    }
    return $root
}

function Read-RestartMarker {
    if (-not (Test-Path -LiteralPath $markerFile)) { return $null }
    try {
        return Get-Content -LiteralPath $markerFile -Raw -Encoding UTF8 | ConvertFrom-Json
    } catch {
        return $null
    }
}

function Get-PortOccupancy([int]$Port) {
    $pids = @(Get-ListeningPids $Port)
    if ($pids.Count -eq 0) {
        return [pscustomobject]@{ Port = $Port; Status = "free"; Pids = @(); Ours = @(); Foreign = @(); Details = @() }
    }
    $ours = @()
    $foreign = @()
    $details = @()
    foreach ($procId in $pids) {
        $summary = Get-ProcessSummary $procId
        $details += $summary
        if (Test-ProcessFromRepo $procId) { $ours += $procId } else { $foreign += $procId }
    }
    $status = "foreign"
    if ($foreign.Count -eq 0) { $status = "ours" }
    return [pscustomobject]@{
        Port = $Port
        Status = $status
        Pids = $pids
        Ours = $ours
        Foreign = $foreign
        Details = $details
    }
}

function Stop-ProcessTree {
    param(
        [int]$ProcessId,
        [System.Collections.Generic.HashSet[int]]$Seen = $null
    )
    if ($ProcessId -le 4) { return }
    if ($null -eq $Seen) { $Seen = New-Object "System.Collections.Generic.HashSet[int]" }
    if (-not $Seen.Add($ProcessId)) { return }
    foreach ($child in @(Get-CimInstance -ClassName Win32_Process -Filter ("ParentProcessId={0}" -f $ProcessId) -ErrorAction SilentlyContinue)) {
        Stop-ProcessTree -ProcessId ([int]$child.ProcessId) -Seen $Seen
    }
    Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
}

function Wait-PortFree([int]$Port) {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        $pids = @(Get-ListeningPids $Port)
        if ($pids.Count -eq 0) { return }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw ("端口 {0} 仍被占用: {1}" -f $Port, ((Get-ListeningPids $Port | ForEach-Object { Get-ProcessSummary $_ }) -join "; "))
}

function Test-CommandExists([string]$Name) {
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Install-WebDependencies {
    $viteBinary = Join-Path $webDir "node_modules\.bin\vite"
    if (Test-Path -LiteralPath $viteBinary) { return }
    Write-RestartLog INFO "web/node_modules 不存在，正在执行 bun install --frozen-lockfile"
    Push-Location $webDir
    try {
        & bun install --frozen-lockfile
        if ($LASTEXITCODE -ne 0) { throw "bun install 失败，无法启动前端。" }
    } finally {
        Pop-Location
    }
}

function Install-AgentRuntime {
    $runtimeEntry = Join-Path $agentRuntimeDir "agent-runtime.mjs"
    $runtimeModules = Join-Path $agentRuntimeDir "node_modules"
    if (-not (Test-Path -LiteralPath $runtimeEntry)) { return }
    if (Test-Path -LiteralPath $runtimeModules) { return }
    if (-not (Test-CommandExists "npm")) {
        Write-RestartLog WARN "未找到 npm，跳过 backend/agent-runtime/pi 依赖安装。嵌入式 Agent 可能不可用。"
        return
    }
    Write-RestartLog INFO "正在安装 backend/agent-runtime/pi 依赖"
    Push-Location $agentRuntimeDir
    try {
        & npm install
        if ($LASTEXITCODE -ne 0) { throw "agent-runtime npm install 失败。" }
    } finally {
        Pop-Location
    }
}

$webOcc = Get-PortOccupancy $WebPort
$backendOcc = Get-PortOccupancy $BackendPort

Write-RestartLog INFO ("仓库: {0}" -f $repoRoot)
Write-RestartLog INFO ("前端端口 {0}: {1}" -f $WebPort, $webOcc.Status)
foreach ($detail in @($webOcc.Details)) { Write-RestartLog INFO ("  {0}" -f $detail) }
Write-RestartLog INFO ("后端端口 {0}: {1}" -f $BackendPort, $backendOcc.Status)
foreach ($detail in @($backendOcc.Details)) { Write-RestartLog INFO ("  {0}" -f $detail) }

$blocked = $false
foreach ($occ in @($webOcc, $backendOcc)) {
    if ($occ.Status -ne "foreign") { continue }
    $blocked = $true
    Write-RestartLog ERROR ("端口 {0} 已被其他项目占用，拒绝重启。" -f $occ.Port)
    foreach ($detail in @($occ.Details)) { Write-RestartLog ERROR ("  {0}" -f $detail) }
}
if ($blocked) {
    Write-Host ("详情已写入 {0}" -f $logFile) -ForegroundColor Red
    exit 2
}

if ($Check) {
    Write-Host ("检查完成。前端 {0}={1}，后端 {2}={3}。" -f $WebPort, $webOcc.Status, $BackendPort, $backendOcc.Status) -ForegroundColor Green
    exit 0
}

$killIds = New-Object "System.Collections.Generic.HashSet[int]"
foreach ($procId in @($webOcc.Ours + $backendOcc.Ours)) {
    [void]$killIds.Add((Get-RepoProcessRoot $procId))
}
$marker = Read-RestartMarker
if ($marker) {
    foreach ($procId in @($marker.webPid, $marker.backendPid)) {
        if ($procId -gt 4 -and (Get-Process -Id $procId -ErrorAction SilentlyContinue) -and (Test-ProcessFromRepo $procId)) {
            [void]$killIds.Add((Get-RepoProcessRoot $procId))
        }
    }
}

if ($killIds.Count -gt 0) {
    Write-RestartLog INFO ("发现本项目实例，正在结束进程: {0}" -f ((@($killIds) | Sort-Object) -join ", "))
    foreach ($procId in @($killIds)) { Stop-ProcessTree $procId }
    Wait-PortFree $WebPort
    Wait-PortFree $BackendPort
    Start-Sleep -Milliseconds 400
}

foreach ($commandName in @("go", "bun")) {
    if (-not (Test-CommandExists $commandName)) {
        Write-RestartLog ERROR ("未找到 {0}，请先安装项目要求的运行时。" -f $commandName)
        exit 1
    }
}

Import-CanvasWindowsProxy
foreach ($directory in @($dataDir, $goBuildCache, $goModuleCache)) {
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
}
Install-WebDependencies
Install-AgentRuntime

$powerShellPath = (Get-Command pwsh -ErrorAction SilentlyContinue).Source
if (-not $powerShellPath) { $powerShellPath = (Get-Command powershell.exe -ErrorAction SilentlyContinue).Source }
if (-not $powerShellPath) {
    Write-RestartLog ERROR "未找到 PowerShell，无法打开前后端独立窗口。"
    exit 1
}

$backendDirLiteral = ConvertTo-PowerShellLiteral $backendDir
$webDirLiteral = ConvertTo-PowerShellLiteral $webDir
$dataDirLiteral = ConvertTo-PowerShellLiteral $dataDir
$goBuildLiteral = ConvertTo-PowerShellLiteral $goBuildCache
$goModLiteral = ConvertTo-PowerShellLiteral $goModuleCache
$backendAddr = "127.0.0.1:{0}" -f $BackendPort
$proxyTarget = "http://127.0.0.1:{0}" -f $BackendPort

$backendCommand = @"
`$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $backendDirLiteral
`$env:CANVAS_BACKEND_ADDR = '$backendAddr'
`$env:CANVAS_BACKEND_DATA_DIR = $dataDirLiteral
`$env:CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS = 'localhost,127.0.0.1'
`$env:GOCACHE = $goBuildLiteral
`$env:GOMODCACHE = $goModLiteral
Write-Host '影策后端: http://$backendAddr' -ForegroundColor Cyan
Write-Host '已放行本机模型上游: localhost, 127.0.0.1' -ForegroundColor DarkGray
go run ./cmd/server
"@

$webCommand = @"
`$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $webDirLiteral
`$env:VITE_API_PROXY_TARGET = '$proxyTarget'
Write-Host '影策前端: http://localhost:$WebPort' -ForegroundColor Cyan
bun run vite --host 0.0.0.0 --port $WebPort --strictPort
"@

$backendProcess = Start-Process -FilePath $powerShellPath -WindowStyle Normal -WorkingDirectory $backendDir -PassThru -ArgumentList @("-NoLogo", "-NoExit", "-NoProfile", "-Command", $backendCommand)
$webProcess = Start-Process -FilePath $powerShellPath -WindowStyle Normal -WorkingDirectory $webDir -PassThru -ArgumentList @("-NoLogo", "-NoExit", "-NoProfile", "-Command", $webCommand)

[pscustomobject]@{
    repoRoot = $repoRoot
    webPort = $WebPort
    backendPort = $BackendPort
    webPid = $webProcess.Id
    backendPid = $backendProcess.Id
    startedAt = (Get-Date).ToString("o")
} | ConvertTo-Json | Set-Content -LiteralPath $markerFile -Encoding UTF8

Write-RestartLog INFO ("已打开开发窗口。后端 PID {0}，前端 PID {1}" -f $backendProcess.Id, $webProcess.Id)
Write-Host ("前端 http://localhost:{0}" -f $WebPort) -ForegroundColor Green
Write-Host ("后端 http://127.0.0.1:{0}/api/health" -f $BackendPort) -ForegroundColor Green
Write-Host "分别在两个窗口按 Ctrl+C 停止服务。再次运行 restart.bat 会结束本项目实例后重启。"

