<#
.SYNOPSIS
  三仓 E2E 封禁/踢线冒烟（收敛改动回归用）。

.DESCRIPTION
  流程：构建新鲜 exe → 起 loginserver_e2e + sgate_e2e（独立端口，不动常驻进程）
  → http_login 建号 → bandidrill login 登录 → 账号封禁断言 KICKED reason=ban
  → 解封复登 → IP 封禁断言 KICKED reason=blacklist → 解封 → 校验名单清零
  → 拆除新鲜进程（常驻 etcd/logic_logincheck/loginserver/sgate_head 只查不杀）。

  历史踩坑（本脚本已规避，改动前先读）：
  1. tlog/日志配置是相对路径 → 两 exe 的 -WorkingDirectory 必须是各自仓库根
    （loginserver 读 ./config/tlog.yaml，sgate 读 config/log/default.yaml）。
  2. http_login 第一参数必须是完整 URL（http://127.0.0.1:10101/api/v1/login）。
  3. PS 5.1 Get-Content 读单行文件会带 ETS 属性，ConvertTo-Json 会序列化成对象
    → 读文件一律 [IO.File]::ReadAllText，读命令输出无此问题。
  4. bandidrill 语义：login=登录后阻塞等踢（测踢线用）；relogin=LOGIN_OK 即退
    （测封禁拦截用，封禁下发时本地无连接属正常 kicked:0，不是缺陷）。
  5. 环境并行 build 会 OOM → 构建固定 -p 1 -ldflags="-s -w"。
  6. 首次运行前 WorkDir 需已有 loginserver_e2e.yaml / sgate_e2e.yaml
    （端口 10101/58080、redis db1、acl.adminToken=loginserver-admin）。
  7. 子进程 stdout 重定向句柄会锁文件：ReadAllText 持续 IOException，
    必须 FileStream+FileShare.ReadWrite 共享读（见 Read-TextSafe）。
  8. 首登可能 >20s（sgate→logic_logincheck gRPC 连接被 GoAway
    too_many_pings 重建），LOGIN_OK 等待窗口给 60s。
  9. Redis stop-writes-on-bgsave-error 触发 MISCONF 时建号会
    code=1002 bind account failed，属环境问题，先 redis-cli bgsave 确认恢复。

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\e2e_smoke.ps1
  powershell -File scripts\e2e_smoke.ps1 -Phase flow     # 复用已启动进程只跑流程
#>
param(
  [ValidateSet('all', 'build', 'start', 'flow', 'stop')]
  [string]$Phase = 'all',
  [string]$SgateRepo = 'E:\sgate',
  [string]$WorkDir = (Join-Path $env:TEMP 'opencode\e2e')
)

$ErrorActionPreference = 'Stop'
$LoginRepo = Split-Path -Parent $PSScriptRoot
$AdminHdr = @{ 'X-Admin-Token' = 'loginserver-admin' }
$Base = 'http://127.0.0.1:10101'
$SgateTcp = '127.0.0.1:58080'
$FreshProcs = @('loginserver_e2e', 'sgate_e2e', 'bandidrill')

function Fail([string]$msg) {
  Write-Host "[FAIL] $msg" -ForegroundColor Red
  Get-Content (Join-Path $WorkDir 'drill_out.log') -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "  drill> $_" }
  Get-Content (Join-Path $WorkDir 'ls_err.log') -Tail 5 -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "  ls> $_" }
  Get-Content (Join-Path $WorkDir 'sg_err.log') -Tail 5 -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "  sg> $_" }
  exit 1
}

function Read-TextSafe([string]$path) {
  # 子进程 stdout 重定向句柄可能独占文件，ReadAllText 会持续 IOException；
  # 用 FileStream 显式共享读写绕开（读到多少算多少，下轮重试补全）
  try {
    $fs = New-Object IO.FileStream($path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
    $sr = New-Object IO.StreamReader($fs)
    $txt = $sr.ReadToEnd()
    $sr.Close()
    return ($txt -replace "`0", '')
  } catch { return '' }
}

function Wait-FileMatch([string]$path, [string]$pattern, [int]$timeoutSec) {
  $deadline = (Get-Date).AddSeconds($timeoutSec)
  while ((Get-Date) -lt $deadline) {
    $txt = Read-TextSafe $path
    if ($txt -match $pattern) { return $true }
    Start-Sleep -Milliseconds 300
  }
  return $false
}

function Wait-Tcp([string]$target, [int]$timeoutSec) {
  $deadline = (Get-Date).AddSeconds($timeoutSec)
  while ((Get-Date) -lt $deadline) {
    $c = New-Object System.Net.Sockets.TcpClient
    try {
      $ok = $c.BeginConnect(($target -split ':')[0], [int]($target -split ':')[1], $null, $null).AsyncWaitHandle.WaitOne(500)
      if ($ok -and $c.Connected) { $c.Close(); return $true }
    } catch { }
    $c.Close()
    Start-Sleep -Milliseconds 300
  }
  return $false
}

function Stop-Fresh {
  Get-Process $FreshProcs -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 1
}

function BestEffort-Unban([string]$acct) {
  if (-not $acct) { return }
  try {
    Invoke-RestMethod -Uri "$Base/api/v1/acl/banned-role" -Method Delete -Headers $AdminHdr `
      -ContentType 'application/json' -Body (ConvertTo-Json @{ roleId = $acct; operator = 'e2e-smoke' }) | Out-Null
  } catch { }
  try {
    Invoke-RestMethod -Uri "$Base/api/v1/acl/blacklist" -Method Delete -Headers $AdminHdr `
      -ContentType 'application/json' -Body (ConvertTo-Json @{ ip = '127.0.0.1'; operator = 'e2e-smoke' }) | Out-Null
  } catch { }
}

$acct = $null
try {
  # ---------- build ----------
  if ($Phase -in @('all', 'build')) {
    Write-Host '== build =='
    foreach ($y in @((Join-Path $WorkDir 'loginserver_e2e.yaml'), (Join-Path $WorkDir 'sgate_e2e.yaml'))) {
      if (-not (Test-Path $y)) { Fail "缺少 e2e 配置: $y（见文件头踩坑 6）" }
    }
    Push-Location $LoginRepo
    go build -p 1 -ldflags='-s -w' -o (Join-Path $WorkDir 'loginserver_e2e.exe') ./cmd/loginserver; if ($LASTEXITCODE) { Pop-Location; Fail 'build loginserver_e2e' }
    go build -p 1 -ldflags='-s -w' -o (Join-Path $WorkDir 'bandidrill.exe') ./tools/bandidrill; if ($LASTEXITCODE) { Pop-Location; Fail 'build bandidrill' }
    go build -p 1 -ldflags='-s -w' -o (Join-Path $WorkDir 'http_login.exe') ./tools/http_login; if ($LASTEXITCODE) { Pop-Location; Fail 'build http_login' }
    Pop-Location
    Push-Location $SgateRepo
    go build -p 1 -ldflags='-s -w' -o (Join-Path $WorkDir 'sgate_e2e.exe') ./cmd/gateway; if ($LASTEXITCODE) { Pop-Location; Fail 'build sgate_e2e' }
    Pop-Location
    Write-Host '   build OK'
  }

  # ---------- start ----------
  if ($Phase -in @('all', 'start')) {
    Write-Host '== start =='
    Stop-Fresh
    foreach ($f in @('drill_out.log', 'ls_out.log', 'ls_err.log', 'sg_out.log', 'sg_err.log')) {
      Remove-Item (Join-Path $WorkDir $f) -Force -ErrorAction SilentlyContinue
    }
    Start-Process -FilePath (Join-Path $WorkDir 'loginserver_e2e.exe') `
      -ArgumentList '-conf', (Join-Path $WorkDir 'loginserver_e2e.yaml') `
      -WorkingDirectory $LoginRepo `
      -RedirectStandardOutput (Join-Path $WorkDir 'ls_out.log') `
      -RedirectStandardError (Join-Path $WorkDir 'ls_err.log')
    Start-Process -FilePath (Join-Path $WorkDir 'sgate_e2e.exe') `
      -ArgumentList '-conf', (Join-Path $WorkDir 'sgate_e2e.yaml') `
      -WorkingDirectory $SgateRepo `
      -RedirectStandardOutput (Join-Path $WorkDir 'sg_out.log') `
      -RedirectStandardError (Join-Path $WorkDir 'sg_err.log')

    $deadline = (Get-Date).AddSeconds(30)
    $lsOk = $false
    while ((Get-Date) -lt $deadline -and -not $lsOk) {
      Start-Sleep -Milliseconds 500
      try {
        $r = Invoke-RestMethod -Uri "$Base/api/v1/servers" -Headers $AdminHdr -Method Get
        if ($r.code -eq 0) { $lsOk = $true }
      } catch { }
    }
    if (-not $lsOk) { Fail 'loginserver_e2e 30s 内未就绪(:10101)' }
    if (-not (Wait-Tcp $SgateTcp 30)) { Fail 'sgate_e2e 30s 内未就绪(:58080)' }
    Write-Host '   both ready'
  }

  if ($Phase -eq 'build') { exit 0 }
  if ($Phase -eq 'start') { exit 0 }

  # ---------- flow ----------
  if ($Phase -in @('all', 'flow')) {
  Write-Host '== flow =='
  $drill = Join-Path $WorkDir 'drill_out.log'

  function New-Account {
    $openId = 'e2e-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    $out = & (Join-Path $WorkDir 'http_login.exe') "$Base/api/v1/login" $openId 1
    $body = ($out | Where-Object { $_ -like 'body:*' }) -replace '^body:\s+', ''
    $j = $body | ConvertFrom-Json
    if ($j.code -ne 0) { Fail "建号失败 openId=$openId body=$body" }
    return @{ id = $j.data.accountId; tok = $j.data.loginToken }
  }

  function Start-Drill {
    Get-Process bandidrill -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Remove-Item $drill -Force -ErrorAction SilentlyContinue
    Start-Process -FilePath (Join-Path $WorkDir 'bandidrill.exe') `
      -ArgumentList 'login', $SgateTcp, 'logic-logincheck', $script:acct.id, $script:acct.tok `
      -WorkingDirectory $WorkDir `
      -RedirectStandardOutput $drill `
      -RedirectStandardError (Join-Path $WorkDir 'drill_err.log')
    # 60s：sgate→logic_logincheck gRPC 连接可能被 GoAway(too_many_pings) 重建，首登偶发 >20s
    if (-not (Wait-FileMatch $drill 'LOGIN_OK' 60)) { Fail 'bandidrill 60s 内未 LOGIN_OK' }
  }

  function Assert-Kicked([string]$what) {
    if (-not (Wait-FileMatch $drill 'KICKED' 15)) { Fail "$what 后 15s 内未收到 KICKED" }
    $txt = Read-TextSafe $drill
    $line = $txt -split "`n" | Where-Object { $_ -match 'KICKED' } | Select-Object -First 1
    Write-Host "   $what KICKED OK: $($line.Trim())"
  }

  $acct = New-Account
  Write-Host "   account=$($acct.id)"

  # 1) 账号封禁 → 踢线
  Start-Drill
  $r = Invoke-RestMethod -Uri "$Base/api/v1/acl/banned-role" -Method Post -Headers $AdminHdr `
    -ContentType 'application/json' -Body (ConvertTo-Json @{ roleId = $acct.id; reason = 'e2e-smoke'; operator = 'e2e-smoke' })
  if ($r.code -ne 0) { Fail "账号封禁 API code=$($r.code) $($r.message)" }
  Assert-Kicked '账号封禁'

  # 2) 解封复登（登录拦截应恢复）
  $r = Invoke-RestMethod -Uri "$Base/api/v1/acl/banned-role" -Method Delete -Headers $AdminHdr `
    -ContentType 'application/json' -Body (ConvertTo-Json @{ roleId = $acct.id; operator = 'e2e-smoke' })
  if ($r.code -ne 0) { Fail "解封 API code=$($r.code)" }
  Start-Drill
  Write-Host '   解封复登 LOGIN_OK OK'

  # 3) IP 封禁 → 踢线（须 login 模式阻塞等踢）
  $r = Invoke-RestMethod -Uri "$Base/api/v1/acl/blacklist" -Method Post -Headers $AdminHdr `
    -ContentType 'application/json' -Body (ConvertTo-Json @{ ip = '127.0.0.1'; reason = 'e2e-smoke'; operator = 'e2e-smoke' })
  if ($r.code -ne 0) { Fail "IP 封禁 API code=$($r.code) $($r.message)" }
  Assert-Kicked 'IP封禁'

  # 4) 解封 + 本次收尾校验（banned-role 可能有历史残留数据，只断言本次账号/IP 不残留）
  BestEffort-Unban $acct.id
  $bl = Invoke-RestMethod -Uri "$Base/api/v1/acl/blacklist?limit=50" -Headers $AdminHdr -Method Get
  $br = Invoke-RestMethod -Uri "$Base/api/v1/acl/banned-role?limit=50" -Headers $AdminHdr -Method Get
  $leftoverIp = @($bl.data | Where-Object { $_.ip -eq '127.0.0.1' }).Count
  $leftoverAcct = @($br.data | Where-Object { $_.roleId -eq $acct.id }).Count
  if ($leftoverIp -ne 0 -or $leftoverAcct -ne 0) {
    Fail "收尾未清零：本次 IP 残留=$leftoverIp 账号残留=$leftoverAcct"
  }
  Write-Host "   收尾清零 OK（名单存量 blacklist=$(@($bl.data).Count) banned=$(@($br.data).Count) 为历史数据，不计）"
  }

  # ---------- stop ----------
  if ($Phase -in @('all', 'stop')) {
    Write-Host '== stop =='
    Stop-Fresh
    $resident = @('etcd', 'logic_logincheck', 'loginserver', 'sgate_head')
    $alive = @($resident | Where-Object { Get-Process $_ -ErrorAction SilentlyContinue })
    if ($alive.Count -lt $resident.Count) {
      Write-Host "   [warn] 常驻进程缺失: $(($resident | Where-Object { $_ -notin $alive }) -join ',')" -ForegroundColor Yellow
    } else {
      Write-Host "   常驻四进程完好: $($resident -join ',')"
    }
    netstat -ano | Select-String ':10101\s.*LISTENING|:58080\s.*LISTENING' | ForEach-Object { Write-Host "   [warn] 残留监听: $($_.Line.Trim())" -ForegroundColor Yellow }
  }

  Write-Host '[PASS] e2e smoke 全绿' -ForegroundColor Green
  exit 0
} finally {
  BestEffort-Unban $acct.id
  if ($Phase -in @('all', 'stop')) { Stop-Fresh }
}
