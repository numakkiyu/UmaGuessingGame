param(
  [ValidateSet("prod", "dev")]
  [string]$Mode = "prod"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

if (-not (Test-Path ".env")) {
  Copy-Item ".env.example" ".env"
  Write-Host "已根据 .env.example 创建 .env，请按需修改其中配置。" -ForegroundColor Yellow
}

$composeArgs = @("-f")
if ($Mode -eq "dev") {
  $composeArgs += "docker-compose.dev.yml"
} else {
  $composeArgs += "docker-compose.yml"
}

Write-Host "正在启动 Docker 服务，模式: $Mode" -ForegroundColor Cyan
docker compose @composeArgs up -d --build

Write-Host ""
Write-Host "当前容器状态：" -ForegroundColor Green
docker compose @composeArgs ps
