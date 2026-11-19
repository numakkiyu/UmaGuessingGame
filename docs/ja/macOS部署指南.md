# macOS での起動とデプロイ

ネイティブの開発ツール、または Docker Desktop を使用します。コマンドはリポジトリのルートで実行します

## ネイティブ実行

Go 1.26.11、Node.js 24、pnpm 10.33.0、Make を用意し、[クイックスタート](快速开始.md)の手順でビルドします。`make dev` は `.cache/runtime`、Go の直接実行は `data/runtime` を使います

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

Linux コンテナーを使用します。問題パックは読み取り専用、実行データとバックアップは別のボリュームに保存します

```bash
make content-compile
make content-verify
cp .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

## スマートフォンから接続

IP をコンピューターの LAN アドレスに置き換えます

```bash
UMA_ADDR=0.0.0.0:8080 \
UMA_PUBLIC_ORIGIN=http://192.168.1.20:8080 \
UMA_ALLOWED_ORIGINS=http://192.168.1.20:8080 \
make dev
```

## URL と接続

スマートフォンやタブレットを同じネットワークに接続し、待ち受けを `0.0.0.0`、Origin を実際の接続 URL に設定します。オフライン機能はブラウザーの安全なコンテキストに依存します

公開サービスは HTTPS リバースプロキシを使い、Origin と信頼するプロキシ範囲を明示します。SSE のバッファリングを無効にし、接続のアイドルタイムアウトを確保します

設定は[設定リファレンス](配置参考.md)、バックアップと接続の対処は[運用と更新](维护与更新指南.md)をご覧ください
