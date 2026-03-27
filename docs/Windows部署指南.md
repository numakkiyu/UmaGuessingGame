# Windows 部署指南

本文档介绍赛马娘猜猜乐在 Windows 环境下的两种主要部署方式：

1. 普通部署：项目本体直接运行在 Windows 上
2. Docker 部署：使用 Docker Desktop 统一拉起应用、数据库与缓存

如果你是第一次部署，推荐优先使用 Docker 方案。

## 一、适用场景

### 普通部署适合

- 本机开发
- UI 联调
- 局域网试玩
- 不想用容器跑应用本体

### Docker 部署适合

- 想快速启动整套环境
- 希望数据库、Redis 和应用统一管理
- 希望迁移到服务器时结构更接近正式环境

## 二、部署前准备

### 1. 软件准备

- Git for Windows
- Node.js 22+
- npm
- PostgreSQL
- Redis 可用实例
- 可选：Docker Desktop
- 可选：PM2

### 2. 关于 PostgreSQL 与 Redis

Windows 上最稳妥的正式方案通常是：

- PostgreSQL 使用原生安装包或远程数据库服务
- Redis 使用远程服务、WSL2 内 Redis，或 Docker Desktop 里的 Redis 容器

如果你只是本机开发测试：

- 可以先把 `DATABASE_URL`、`REDIS_URL` 配好
- 在非生产环境下，仓库内部分逻辑会在基础设施不可用时自动回退到本地内存 / 文件模式
- 但正式上线不要依赖这种回退方式

## 三、方案 A：普通部署

### 第 1 步：克隆仓库

```powershell
git clone https://github.com/numakkiyu/UmaGuessingGame.git
cd UmaGuessingGame
```

### 第 2 步：复制环境变量模板

```powershell
Copy-Item .env.example .env
```

然后编辑 `.env`，至少确认以下项目：

- `APP_ENV`
- `APP_BASE_URL`
- `ASSET_BASE_URL`
- `DATABASE_URL`
- `REDIS_URL`
- `APP_SIGNING_SECRET`
- `TURNSTILE_ENABLED`

### 第 3 步：安装依赖

```powershell
npm install
```

### 第 4 步：准备题库与搜索索引

```powershell
npm run prepare:data
```

### 第 5 步：初始化数据库

```powershell
npm run db:push
npm run db:seed
```

### 第 6 步：开发模式启动

```powershell
npm run dev
```

打开浏览器访问：

- [http://localhost:3000](http://localhost:3000)

### 第 7 步：生产模式启动

先构建：

```powershell
npm run build
```

再启动：

```powershell
npm run start
```

## 四、Windows 上如何长期运行

### 方案 1：直接开命令行窗口

适合临时测试，不适合长期托管。

### 方案 2：使用 PM2

先全局安装：

```powershell
npm install -g pm2
```

在项目目录执行：

```powershell
pm2 start npm --name umaguessinggame -- run start
pm2 save
```

常用命令：

```powershell
pm2 status
pm2 logs umaguessinggame
pm2 restart umaguessinggame
pm2 stop umaguessinggame
```

仓库中附带了一个 PM2 示例配置：

- [ecosystem.config.cjs.example](../ecosystem.config.cjs.example)

## 五、方案 B：Docker Desktop 部署

### 第 1 步：安装 Docker Desktop

安装完成后，确认以下命令可用：

```powershell
docker version
docker compose version
```

### 第 2 步：复制环境变量

```powershell
Copy-Item .env.example .env
```

### 第 3 步：一键启动开发环境

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\docker-up-windows.ps1 -Mode dev
```

### 第 4 步：一键启动生产编排

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\docker-up-windows.ps1
```

脚本会自动：

- 检查 `.env` 是否存在
- 如果不存在则从 `.env.example` 复制
- 按对应模式启动 Docker Compose
- 显示当前容器状态

### 第 5 步：手动方式启动

开发编排：

```powershell
docker compose -f docker-compose.dev.yml up -d --build
```

生产编排：

```powershell
docker compose -f docker-compose.yml up -d --build
```

### 第 6 步：停止容器

开发编排：

```powershell
docker compose -f docker-compose.dev.yml down
```

生产编排：

```powershell
docker compose -f docker-compose.yml down
```

## 六、常见问题

### 1. 端口被占用

如果 `3000`、`5432`、`6379` 已被占用，请先停止现有服务，或修改映射端口与 `.env`。

### 2. PowerShell 不允许执行脚本

可以临时使用：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\docker-up-windows.ps1
```

### 3. 数据库连接失败

请优先检查：

- `DATABASE_URL` 是否正确
- PostgreSQL 是否已启动
- 防火墙是否拦截
- 数据库用户名、密码、库名是否匹配

### 4. Redis 连接失败

请优先检查：

- `REDIS_URL` 是否正确
- Redis 是否真的在运行
- Windows 本机是否通过 Docker / WSL2 / 远程服务提供 Redis

## 七、推荐结论

- 如果你只是本机试玩或开发：优先用 Docker Desktop
- 如果你要做 UI 修改和调试：普通部署更灵活
- 如果你准备正式对外：建议转到 Linux 服务器部署，见 [Linux部署指南.md](./Linux部署指南.md)

