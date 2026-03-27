# macOS 部署指南

本文档介绍赛马娘猜猜乐在 macOS 下的两种常见部署方式：

1. 普通部署：Homebrew + 本机 Node.js 运行
2. Docker 部署：Docker Desktop 统一运行

如果你使用的是 MacBook 或设计联调环境，普通部署通常更方便；如果你希望结构接近正式环境，推荐 Docker Desktop。

## 一、部署前准备

### 建议系统环境

- macOS 13 或更高版本
- Apple Silicon 或 Intel 均可

### 建议安装的软件

- Xcode Command Line Tools
- Homebrew
- Git
- Node.js 22+
- PostgreSQL
- Redis
- Docker Desktop（可选）

## 二、方案 A：普通部署

### 第 1 步：安装 Homebrew

如果系统还没有 Homebrew，可先安装：

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

### 第 2 步：安装 Node.js、PostgreSQL、Redis

```bash
brew install node@22 postgresql@17 redis
```

根据你的芯片与 shell，把 Node.js 加到 PATH。

### 第 3 步：启动 PostgreSQL 与 Redis

```bash
brew services start postgresql@17
brew services start redis
```

### 第 4 步：创建数据库

```bash
createdb umaguessinggame
```

如果你需要独立用户，请使用 PostgreSQL 自己的用户与权限管理方式创建。

### 第 5 步：拉取项目

```bash
git clone https://github.com/numakkiyu/UmaGuessingGame.git
cd UmaGuessingGame
cp .env.example .env
```

编辑 `.env`，至少确认：

- `DATABASE_URL`
- `REDIS_URL`
- `APP_BASE_URL`
- `ASSET_BASE_URL`
- `APP_SIGNING_SECRET`

### 第 6 步：安装依赖并准备数据

```bash
npm install
npm run prepare:data
npm run db:push
npm run db:seed
```

### 第 7 步：启动开发模式

```bash
npm run dev
```

### 第 8 步：启动生产模式

```bash
npm run build
npm run start
```

## 三、方案 B：Docker Desktop 部署

### 第 1 步：安装 Docker Desktop

安装完成后检查：

```bash
docker version
docker compose version
```

### 第 2 步：复制环境变量

```bash
cp .env.example .env
```

### 第 3 步：使用一键脚本

开发模式：

```bash
chmod +x ./scripts/docker-up-macos.sh
./scripts/docker-up-macos.sh dev
```

生产模式：

```bash
./scripts/docker-up-macos.sh
```

### 第 4 步：手动启动

开发编排：

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

生产编排：

```bash
docker compose -f docker-compose.yml up -d --build
```

### 第 5 步：停止容器

```bash
docker compose -f docker-compose.yml down
```

## 四、常见问题

### 1. Apple Silicon 拉镜像慢

可以提前配置镜像加速，或先手动拉取基础镜像。

### 2. `brew services` 没有启动成功

请检查：

- Homebrew 安装路径是否正确
- 当前 shell 的 PATH 是否已生效
- 端口是否被别的服务占用

### 3. Docker 启动后端口冲突

请确认：

- `3000`
- `5432`
- `6379`

没有被本机其他进程占用。

## 五、推荐结论

- 本机开发与设计联调：普通部署
- 想快速复现正式环境：Docker Desktop
- 要给公网玩家使用：仍建议迁移到 Linux 服务器，见 [Linux部署指南.md](./Linux部署指南.md)

