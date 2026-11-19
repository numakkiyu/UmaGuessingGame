# Windows での起動とデプロイ

WSL2 の Linux ツールチェーン、または Docker Desktop の Linux コンテナーを推奨します。Dockerfile は Windows コンテナー用ではありません

## WSL2

WSL 内に Go、Node.js、pnpm、Make をインストールし、ソースを Linux ファイルシステムに置きます。[クイックスタート](快速开始.md)で起動できます

ブラウザーでは `http://localhost:8080` を開きます。他端末からの接続には WSL のネットワークモード、Windows ファイアウォール、待ち受けアドレスも関係します

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

Make を使わず、依存インストール、ビルド、埋め込みページ更新、問題ビルド、起動を実行します

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

## スマートフォンから接続

IP を実際の LAN アドレスに置き換えます

```powershell
$env:UMA_ADDR = "0.0.0.0:8080"
$env:UMA_PUBLIC_ORIGIN = "http://192.168.1.20:8080"
$env:UMA_ALLOWED_ORIGINS = "http://192.168.1.20:8080"
go run ./cmd/server --config config/lan.json
```

## Docker Desktop

[問題パックをビルド](内容编译.md)してから、Linux コンテナーで起動します

```powershell
Copy-Item .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 app
```

## URL と接続

スマートフォンやタブレットを同じネットワークに接続し、待ち受けを `0.0.0.0`、Origin を実際の接続 URL に設定します。オフライン機能はブラウザーの安全なコンテキストに依存します

公開サービスは HTTPS リバースプロキシを使い、Origin と信頼するプロキシ範囲を明示します。SSE のバッファリングを無効にし、接続のアイドルタイムアウトを確保します

設定は[設定リファレンス](配置参考.md)、バックアップと接続の対処は[運用と更新](维护与更新指南.md)をご覧ください
