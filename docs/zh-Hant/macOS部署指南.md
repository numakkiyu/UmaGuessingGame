# macOS 執行與部署

可用原生工具開發，或以 Docker Desktop 執行。所有指令在儲存庫根目錄操作

## 原生執行

安裝 Go 1.26.11、Node.js 24、pnpm 10.33.0 與 Make，依[快速開始](快速开始.md)建置。`make dev` 使用 `.cache/runtime`，直接執行 Go 則使用 `data/runtime`

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

## Docker Desktop

使用 Linux 容器，內容包唯讀掛載，執行資料與備份使用不同卷

```bash
make content-compile
make content-verify
cp .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

## 手機連線

將 IP 換成電腦的區域網路位址

```bash
UMA_ADDR=0.0.0.0:8080 \
UMA_PUBLIC_ORIGIN=http://192.168.1.20:8080 \
UMA_ALLOWED_ORIGINS=http://192.168.1.20:8080 \
make dev
```

## 網址與連線

讓手機或平板連至同一網路，監聽位址設為 `0.0.0.0`，Origin 設為實際訪問網址。離線能力需要瀏覽器支援安全環境

公開服務使用 HTTPS 反向代理，精確設定 Origin 與可信代理範圍，SSE 停用緩衝並保留足夠的閒置連線時間

設定見[設定參考](配置参考.md)，備份與連線排查見[維護與更新](维护与更新指南.md)
