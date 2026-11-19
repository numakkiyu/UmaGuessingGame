# Windows 執行與部署

建議在 WSL2 使用 Linux 工具鏈，或用 Docker Desktop 的 Linux 容器。專案 Dockerfile 不產生 Windows 容器映像

## WSL2

在 WSL 內安裝 Go、Node.js、pnpm 與 Make，將原始碼放在 Linux 檔案系統中，依[快速開始](快速开始.md)執行

瀏覽器可開啟 `http://localhost:8080`。其他裝置連線還受 WSL 網路模式、Windows 防火牆與監聽位址影響

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

## PowerShell

不使用 Make 時，依序安裝依賴、建置、更新嵌入頁面、編譯題庫並啟動

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

## 手機連線

將 IP 換成電腦的區域網路位址

```powershell
$env:UMA_ADDR = "0.0.0.0:8080"
$env:UMA_PUBLIC_ORIGIN = "http://192.168.1.20:8080"
$env:UMA_ALLOWED_ORIGINS = "http://192.168.1.20:8080"
go run ./cmd/server --config config/lan.json
```

## Docker Desktop

先[建置內容包](内容编译.md)，再使用 Linux 容器啟動

```powershell
Copy-Item .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 app
```

## 網址與連線

讓手機或平板連至同一網路，監聽位址設為 `0.0.0.0`，Origin 設為實際訪問網址。離線能力需要瀏覽器支援安全環境

公開服務使用 HTTPS 反向代理，精確設定 Origin 與可信代理範圍，SSE 停用緩衝並保留足夠的閒置連線時間

設定見[設定參考](配置参考.md)，備份與連線排查見[維護與更新](维护与更新指南.md)
