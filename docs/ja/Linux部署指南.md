# Linux へのデプロイ

Docker Compose、または専用ユーザーで Go プログラムを実行します。初回の手順は[クイックスタート](快速开始.md)にあります

## プログラムと保存先

ビルドには Go、Node.js、pnpm、Make を使います。実行時に必要なのは Go プログラム、問題データ、書き込み可能な保存先です。Node、PostgreSQL、Redis は不要です

データはローカルディスクに保存し、NFS／SMB は使用しません。一つの SQLite ディレクトリを一つのプロセスが所有し、問題データは読み取り専用、バックアップは別に管理します

## コンテナー

```bash
make content-compile
make content-verify
cp .env.example .env
```

### LAN の設定例

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

## HTTPS で公開

HTTPS リバースプロキシを用意し、公開 Origin と Secure Cookie を設定します

```dotenv
UMA_CONFIG=/etc/umaguess/cloud.json
UMA_DEPLOYMENT=internet
UMA_PUBLIC_ORIGIN=https://uma.example.com
UMA_ALLOWED_ORIGINS=https://uma.example.com
UMA_SECURE_COOKIES=true
```

## 稼働状態とバックアップ

```bash
curl --fail http://localhost:8080/health/ready
docker compose exec app /app/uma-server backup --dir /backups
```

## URL と接続

スマートフォンやタブレットを同じネットワークに接続し、待ち受けを `0.0.0.0`、Origin を実際の接続 URL に設定します。オフライン機能はブラウザーの安全なコンテキストに依存します

公開サービスは HTTPS リバースプロキシを使い、Origin と信頼するプロキシ範囲を明示します。SSE のバッファリングを無効にし、接続のアイドルタイムアウトを確保します

設定は[設定リファレンス](配置参考.md)、バックアップと接続の対処は[運用と更新](维护与更新指南.md)をご覧ください
