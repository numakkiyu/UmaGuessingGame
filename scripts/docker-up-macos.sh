#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-prod}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

if [[ ! -f ".env" ]]; then
  cp ".env.example" ".env"
  echo "已根据 .env.example 创建 .env，请按需修改其中配置。"
fi

if [[ "${MODE}" == "dev" ]]; then
  COMPOSE_FILE="docker-compose.dev.yml"
else
  COMPOSE_FILE="docker-compose.yml"
fi

echo "正在启动 Docker 服务，模式: ${MODE}"
docker compose -f "${COMPOSE_FILE}" up -d --build

echo
echo "当前容器状态："
docker compose -f "${COMPOSE_FILE}" ps
