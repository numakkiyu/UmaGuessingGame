# Linux 部署指南

本文档覆盖赛马娘猜猜乐在 Linux 环境下最常见的四种部署方式：

1. 普通命令行部署
2. Docker Compose 部署
3. 宝塔面板部署
4. 1Panel 部署

如果你准备做正式公网服务，推荐优先考虑：

- Docker Compose
- PM2 + Nginx
- 面板环境中的反向代理 + Node 服务

## 一、部署前统一准备

### 系统建议

- Ubuntu 22.04 / 24.04 LTS
- Debian 12
- 其他支持 Node.js 22、Docker、PostgreSQL、Redis 的发行版

### 最低准备

- Git
- Node.js 22+
- npm
- PostgreSQL
- Redis
- 开放站点使用的端口或反向代理能力

### 拉取代码

```bash
git clone https://github.com/numakkiyu/UmaGuessingGame.git
cd UmaGuessingGame
cp .env.example .env
```

## 二、方案 A：普通命令行部署

这套方式适合你希望直接控制 Node.js、Nginx、PM2 和系统服务。

### 第 1 步：安装 Node.js 22

推荐使用 `nvm` 管理 Node.js 版本：

```bash
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
source ~/.bashrc
nvm install 22
nvm use 22
node -v
npm -v
```

### 第 2 步：安装 PostgreSQL 与 Redis

以 Ubuntu 为例：

```bash
sudo apt update
sudo apt install -y postgresql redis-server
```

启动并设置开机自启：

```bash
sudo systemctl enable postgresql
sudo systemctl start postgresql
sudo systemctl enable redis-server
sudo systemctl start redis-server
```

### 第 3 步：创建数据库

```bash
sudo -u postgres psql
```

在 PostgreSQL 控制台中执行：

```sql
CREATE DATABASE umaguessinggame;
CREATE USER umaguessinggame WITH PASSWORD '请换成你自己的强密码';
GRANT ALL PRIVILEGES ON DATABASE umaguessinggame TO umaguessinggame;
\q
```

然后在 `.env` 中写入：

```env
DATABASE_URL=postgresql://umaguessinggame:你的密码@127.0.0.1:5432/umaguessinggame
REDIS_URL=redis://127.0.0.1:6379
APP_ENV=production
APP_BASE_URL=https://你的域名
ASSET_BASE_URL=https://你的域名
APP_SIGNING_SECRET=请填写一段足够长的随机字符串
```

### 第 4 步：安装依赖并准备数据

```bash
npm install
npm run prepare:data
npm run db:push
npm run db:seed
```

### 第 5 步：构建应用

```bash
npm run build
```

### 第 6 步：直接启动

```bash
npm run start
```

如果只是临时验证，这一步已经足够。

### 第 7 步：使用 PM2 托管

安装 PM2：

```bash
npm install -g pm2
```

快速启动：

```bash
pm2 start npm --name umaguessinggame -- run start
pm2 save
pm2 startup
```

或者使用仓库内示例配置：

```bash
cp ecosystem.config.cjs.example ecosystem.config.cjs
pm2 start ecosystem.config.cjs
pm2 save
pm2 startup
```

常用命令：

```bash
pm2 status
pm2 logs umaguessinggame
pm2 restart umaguessinggame
pm2 stop umaguessinggame
```

### 第 8 步：Nginx 反向代理

安装 Nginx：

```bash
sudo apt install -y nginx
```

创建配置文件：

```bash
sudo nano /etc/nginx/sites-available/umaguessinggame.conf
```

示例配置：

```nginx
server {
    listen 80;
    server_name your-domain.com;

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

启用配置：

```bash
sudo ln -s /etc/nginx/sites-available/umaguessinggame.conf /etc/nginx/sites-enabled/umaguessinggame.conf
sudo nginx -t
sudo systemctl reload nginx
```

### 第 9 步：配置 HTTPS

如果你使用域名，建议再安装 Certbot：

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d your-domain.com
```

## 三、方案 B：Docker Compose 部署

这套方式最接近仓库原生结构，也是最推荐的公网部署方案之一。

### 第 1 步：安装 Docker 与 Compose

确认以下命令可用：

```bash
docker version
docker compose version
```

### 第 2 步：配置 `.env`

```bash
cp .env.example .env
```

至少确认：

- `APP_ENV=production`
- `APP_BASE_URL`
- `ASSET_BASE_URL`
- `APP_SIGNING_SECRET`
- `TURNSTILE_SITE_KEY`
- `TURNSTILE_SECRET_KEY`

### 第 3 步：使用一键脚本

生产模式：

```bash
chmod +x ./scripts/docker-up-linux.sh
./scripts/docker-up-linux.sh
```

开发模式：

```bash
./scripts/docker-up-linux.sh dev
```

### 第 4 步：手动启动

```bash
docker compose -f docker-compose.yml up -d --build
```

生产编排默认只对宿主机暴露应用的 `3000` 端口，`postgres` 和 `redis` 仅在 Compose 内部网络中提供给应用使用。这样更适合公网服务器、宝塔和 1Panel 场景，也能避免与宿主机已有的数据库或 Redis 服务冲突。

### 第 5 步：查看状态与日志

```bash
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs -f app
```

### 第 6 步：停止服务

```bash
docker compose -f docker-compose.yml down
```

## 四、方案 C：宝塔面板部署

这套方式适合偏可视化管理的运维场景。推荐使用：

- 宝塔面板 + Nginx
- Node 项目 / PM2 托管应用
- PostgreSQL 与 Redis 使用本机服务或 Docker 管理器

### 推荐结构

- 域名通过宝塔网站管理
- 应用监听 `127.0.0.1:3000`
- 宝塔 Nginx 反向代理到 `3000`
- PostgreSQL 和 Redis 独立运行

### 第 1 步：安装宝塔面板

先完成宝塔面板安装与基础安全设置，再进入面板。

### 第 2 步：安装运行环境

在宝塔中建议准备：

- Nginx
- PM2 管理器或 Node 项目管理器
- Docker 管理器

### 第 3 步：上传或拉取代码

把仓库放到例如：

```text
/www/wwwroot/UmaGuessingGame
```

然后进入项目目录执行：

```bash
npm install
cp .env.example .env
npm run prepare:data
npm run db:push
npm run db:seed
npm run build
```

### 第 4 步：托管应用

#### 方式 A：Node 项目管理器

- 启动命令：`npm run start`
- 工作目录：项目根目录
- 监听端口：`3000`

#### 方式 B：PM2 管理器

```bash
pm2 start npm --name umaguessinggame -- run start
pm2 save
```

### 第 5 步：在宝塔创建站点

- 绑定你的域名
- 不需要 PHP 运行环境

### 第 6 步：设置反向代理

在站点设置中添加反向代理：

- 目标地址：`http://127.0.0.1:3000`
- 开启 WebSocket 支持

### 第 7 步：配置 HTTPS

使用宝塔面板的 SSL 功能申请证书并开启强制 HTTPS。

### 第 8 步：PostgreSQL 与 Redis 处理方式

宝塔环境下建议以下两种方案任选其一：

- 使用系统级 PostgreSQL / Redis 服务
- 使用宝塔 Docker 管理器启动 `postgres:17-alpine` 与 `redis:7-alpine`

只要 `.env` 中的连接地址正确即可。

如果你直接使用仓库内生产 `docker-compose.yml`，其中自带的 `postgres` 和 `redis` 默认不会占用宿主机 `5432` / `6379`。宝塔只需要反向代理应用的 `3000` 端口即可。

## 五、方案 D：1Panel 部署

1Panel 更适合容器化与现代面板运维场景。推荐两种思路：

1. 用 1Panel 的容器 / 编排功能直接跑仓库内 Compose
2. 用 1Panel 管理站点与反向代理，应用由 PM2 或容器提供

### 推荐结构

- 1Panel 负责网站、反向代理、证书和容器
- 应用服务监听 `3000`
- PostgreSQL 与 Redis 通过容器或独立服务运行

### 第 1 步：准备目录

把代码放到例如：

```text
/opt/UmaGuessingGame
```

### 第 2 步：准备环境变量

```bash
cp .env.example .env
```

根据域名和服务地址修改 `.env`。

### 第 3 步：使用 1Panel 编排方式

如果你想完全容器化，最简单的办法是：

- 在 1Panel 中进入容器或编排功能
- 使用仓库内 `docker-compose.yml`
- 指定项目目录
- 启动编排

等价命令仍然是：

```bash
docker compose -f docker-compose.yml up -d --build
```

### 第 4 步：使用 1Panel 网站反向代理

如果你希望域名、HTTPS 和转发都交给 1Panel 处理：

- 在 1Panel 创建网站
- 配置反向代理到 `http://127.0.0.1:3000`
- 打开 WebSocket 支持
- 申请并启用 HTTPS 证书

### 第 5 步：非容器 Node 方式

如果你的应用不是直接跑在 Compose 中，也可以：

```bash
npm install
npm run prepare:data
npm run db:push
npm run db:seed
npm run build
pm2 start npm --name umaguessinggame -- run start
```

然后仍然让 1Panel 的网站反向代理到 `3000`。

## 六、上线前检查清单

- 已修改 `APP_BASE_URL`
- 已修改 `ASSET_BASE_URL`
- 已设置强随机的 `APP_SIGNING_SECRET`
- 已配置正式的 `DATABASE_URL`
- 已配置正式的 `REDIS_URL`
- 已配置正式的 `TURNSTILE` 密钥
- 已执行 `npm run build`
- 已执行数据库初始化
- 已确认端口、域名、证书和防火墙

## 七、常见问题

### 1. 页面能打开，但开始新局失败

优先检查：

- 数据库是否初始化成功
- `question_bank_ready.json` 是否存在且非空
- PostgreSQL 与 Redis 是否可连通

### 2. 图片加载慢

优先检查：

- `ASSET_PROXY_ENABLED`
- 缓存目录是否可写
- 反向代理是否正确转发静态资源

### 3. 打开 Turnstile 后无法开始

优先检查：

- `TURNSTILE_SITE_KEY`
- `TURNSTILE_SECRET_KEY`
- 站点域名是否与你在 Cloudflare 后台配置的一致

## 八、推荐结论

- 想最快上线：Docker Compose
- 想传统 Node 运维：PM2 + Nginx
- 想使用面板：宝塔或 1Panel 都可，但仍建议理解反向代理、环境变量和数据库初始化链路

