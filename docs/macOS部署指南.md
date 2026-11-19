# macOS 运行与部署

macOS 适合开发、预演和小型局域网活动。长时间公网运行仍需稳定电源、网络、磁盘与服务管理；开发机器上的一次通过不代表持续运行能力

## 原生开发

安装 Go 1.26.11、Node.js 24、pnpm 10.33.0 与 Make，确认 `go version`、`node --version`、`pnpm --version`。在仓库根目录执行：

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

打开 `http://localhost:8080`。开发数据默认在 `.cache/runtime`，不要在多个终端重复启动同一数据目录。前端热更新与原生启动的区别见[快速开始](快速开始.md)

## Docker Desktop

先启动 Docker Desktop，确认 `docker info` 成功，再编译内容包：

```bash
make content-compile
make content-verify
cp .env.example .env
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

容器构建会生成前端和 Go 二进制，运行内容包来自宿主的 `content/` 只读挂载。默认命名卷位于 Docker 管理的本地 Linux 文件系统，不能换成 NAS 共享数据库目录

若遇镜像或依赖网络问题，先检查 Docker Desktop 代理与出站网络；允许显式使用 Dockerfile 支持的运行镜像与 Go 代理设置，见[配置参考](配置参考.md)，不要把未知镜像源写成默认配置

## 手机、平板和展示屏

在系统网络设置确认 Mac 的局域网地址，将 Origin 改为实际访问地址，允许 macOS 防火墙所需入站连接。原生运行示例：

```bash
UMA_ADDR=0.0.0.0:8080 \
UMA_PUBLIC_ORIGIN=http://192.168.1.20:8080 \
UMA_ALLOWED_ORIGINS=http://192.168.1.20:8080 \
make dev
```

容器使用 `.env` 设置相同公开地址。手机不使用 localhost，注意来宾 Wi-Fi 的客户端隔离、VPN 及 Mac 休眠对实时连接的影响。HTTP 局域网环境不保证提供安全上下文下的全部浏览器缓存能力
