# Linux 部署

可使用 Docker Compose，或以專用使用者執行 Go 程式。完整建置入口見[快速開始](快速开始.md)

## 程式與目錄

原始碼建置需要 Go、Node.js、pnpm 和 Make，執行時使用 Go 程式、內容包與可寫資料目錄。正式服務不需 Node、PostgreSQL 或 Redis

執行資料放在本機磁碟，避免 NFS／SMB。SQLite 的同一資料目錄由一個程序持有；內容包唯讀，備份與執行目錄分開

## 容器

```bash
make content-compile
make content-verify
cp .env.example .env
```

### 區域網路設定範例

```dotenv
UMA_CONFIG=/etc/umaguess/lan.json
UMA_HOST_PORT=8080
UMA_PUBLIC_ORIGIN=http://192.168.1.20:8080
UMA_ALLOWED_ORIGINS=http://192.168.1.20:8080
UMA_SECURE_COOKIES=false
```

```bash
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 app
```

## 公開 HTTPS 服務

設定 HTTPS 反向代理、公開 Origin 與 Secure Cookie

```dotenv
UMA_CONFIG=/etc/umaguess/cloud.json
UMA_DEPLOYMENT=internet
UMA_PUBLIC_ORIGIN=https://uma.example.com
UMA_ALLOWED_ORIGINS=https://uma.example.com
UMA_SECURE_COOKIES=true
```

## 執行狀態與備份

```bash
curl --fail http://localhost:8080/health/ready
docker compose exec app /app/uma-server backup --dir /backups
```

## 網址與連線

讓手機或平板連至同一網路，監聽位址設為 `0.0.0.0`，Origin 設為實際訪問網址。離線能力需要瀏覽器支援安全環境

公開服務使用 HTTPS 反向代理，精確設定 Origin 與可信代理範圍，SSE 停用緩衝並保留足夠的閒置連線時間

設定見[設定參考](配置参考.md)，備份與連線排查見[維護與更新](维护与更新指南.md)
