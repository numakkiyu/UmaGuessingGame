# Linux 部署 V2

推荐使用 Docker Engine 与 Compose v2，应用、题包和数据目录的关系见[技术架构](技术架构说明.md)。SQLite 运行卷必须落在宿主本地文件系统，不要挂载网络共享

## 准备

安装 Git、Docker Engine、Compose 插件；从源码生成题包还需要 Go 1.26.1。若在宿主构建前端，则另需 Node.js 24、pnpm 10.33.0 与 Make。具体安装方式以发行版及工具官方文档为准

```bash
docker version
docker compose version
go version
```

在仓库根目录编译题包并创建配置：

```bash
make content-compile
make content-verify
cp .env.example .env
```

## 局域网活动

修改 `.env` 中的访问地址为主机真实 IP：

```dotenv
UMA_CONFIG=/etc/umaguess/lan.json
UMA_HOST_PORT=8080
UMA_PUBLIC_ORIGIN=http://192.168.1.20:8080
UMA_ALLOWED_ORIGINS=http://192.168.1.20:8080
UMA_SECURE_COOKIES=false
```

`UMA_CONFIG` 在容器内解析。仅向活动网络开放端口，在场地网络用真实手机和展示屏验证，不要将该 HTTP 示例直接用于公网

```bash
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 app
```

## 公网与反向代理

使用域名、HTTPS 和 cloud 配置，例：

```dotenv
UMA_CONFIG=/etc/umaguess/cloud.json
UMA_DEPLOYMENT=internet
UMA_PUBLIC_ORIGIN=https://uma.example.com
UMA_ALLOWED_ORIGINS=https://uma.example.com
UMA_SECURE_COOKIES=true
```

反向代理应传递真实 Host，禁用实时 SSE 路径的响应缓冲，并设置足够的空闲超时。需要 WebSocket 时提供 Upgrade 转发。限制管理与指标端点，避免客户端绕过代理直接访问应用端口

默认 Compose 会将端口绑定到宿主接口。若只通过本机代理访问，应在自己的部署副本/覆盖配置中改为回环绑定，并用 `docker compose config --quiet` 检查最终配置。不要只依赖一条代理规则就认为公开的容器端口也被保护

仅在确有需要时设置 `UMA_TRUST_PROXY=true`，并配置容器看到的真实代理 CIDR，不能填通配网段。完整变量见[配置参考](配置参考.md)

## 数据、权限与运维

容器以 UID/GID 65532 运行，默认命名卷保存运行数据与备份，题包和配置只读挂载。如果改为 bind mount，应预先设置最小必要写权限，不要通过特权容器或 `chmod 777` 绕过权限问题

```bash
curl --fail http://localhost:8080/health/ready
docker compose exec app /app/uma-server backup --dir /backups
```

反向代理对外验证还需使用实际 HTTPS 域名完成入房、作答、重连与结算。健康检查不能代替这些流程。数据库单进程持有，不执行 `docker compose up --scale app=2` 共享运行卷

更新、密钥与回滚见[维护指南](维护与更新指南.md)和[安全策略](../SECURITY.md)
