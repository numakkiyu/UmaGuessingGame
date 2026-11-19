# Windows 运行与部署

推荐在 WSL2 中运行开发命令，或使用 Docker Desktop 的 Linux 容器。当前 Dockerfile 面向 Linux 容器，不是 Windows 容器镜像

## WSL2

在 WSL2 发行版安装 Git、Go 1.26.11、Node.js 24、pnpm 10.33.0 和 Make。建议把仓库与 SQLite 运行数据放在 WSL 的 Linux 文件系统中，避免网络共享和跨文件系统 I/O 影响数据库行为

在仓库根目录：

```bash
corepack enable
make web-install
make web-build
make embed-spa
make embed-spa-check
make content-compile
make content-verify
make dev
```

浏览器打开 `http://localhost:8080`。若从其他设备访问，需要同时检查 WSL 网络模式、Windows 防火墙、监听地址与 Origin，而不是只改应用监听到 `0.0.0.0`

## PowerShell 原生开发

安装 Go、Node.js 与 pnpm 后，可直接运行：

```powershell
corepack enable
pnpm --dir web install --frozen-lockfile
pnpm --dir web/tools/toolchain install --frozen-lockfile
pnpm --dir web run build
if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
Remove-Item internal/spafs/dist -Recurse -Force
New-Item internal/spafs/dist -ItemType Directory -Force | Out-Null
Copy-Item web/dist/* internal/spafs/dist/ -Recurse -Force
go run ./cmd/content build --out content
if ($LASTEXITCODE -ne 0) { throw "Content build failed" }
go run ./cmd/content verify
if ($LASTEXITCODE -ne 0) { throw "Content verification failed" }
go run ./cmd/server
```

`internal/spafs/dist` 是可重建的嵌入产物，替换时先移除旧文件，防止残留 chunk；不要将此删除操作用于运行数据目录。直接运行 Go 的数据默认在 `data/runtime`，监听默认 `127.0.0.1:8080`

环境变量使用 PowerShell 语法，Go 不自动加载 `.env`：

```powershell
$env:UMA_ADDR = "0.0.0.0:8080"
$env:UMA_PUBLIC_ORIGIN = "http://192.168.1.20:8080"
$env:UMA_ALLOWED_ORIGINS = "http://192.168.1.20:8080"
go run ./cmd/server --config config/lan.json
```

地址应替换为实际 IP，避免在错误环境中保留这些变量；公网部署不能沿用 HTTP 示例

## Docker Desktop

使用 Linux 容器模式并启用 WSL2 集成，确认 `docker info` 和 `docker compose version`。先用 Go 编译并校验内容包，然后：

```powershell
Copy-Item .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 app
```

Compose 自动读取 `.env` 做插值，但只向容器传入显式声明的变量。`UMA_CONFIG` 使用 `/etc/umaguess/lan.json` 等容器路径，不能填写 `C:\...`。使用命名卷存放 SQLite，不能将运行数据库放在 SMB 共享目录

## 排查与维护

页面能打开但作答失败，先检查访问域名、端口、Origin 与 Cookie；手机连不上先检查 Windows 防火墙和 WSL 端口可达性。备份与恢复不能只复制正在运行的 `runtime.db`

完整配置、HTTPS、更新与回滚见[配置参考](配置参考.md)、[Linux 部署中的代理说明](Linux部署指南.md)和[维护指南](维护与更新指南.md)
