# 赛马娘猜猜乐 · UmaGuessingGame

<div align="center">
  <img src="web/public/assets/ui/branding/official-logo-jp.png" alt="赛马娘" width="220">
  <p>猜角色、拼记忆、聊默契，也可以和朋友办一场学园娱乐赛</p>
</div>

UmaGuessingGame 是《赛马娘》主题的非官方同人网页游戏，由 BHCN STUDIO 维护。V2 保留经典「弗一把」的逐步推理，加入卡牌、排序、合作解谜和娱乐赛事等玩法，支持单人练习、邀请码联机及主持人组织的活动

代码采用 [MIT 许可](LICENSE)。角色形象、标识、图片、声音和资料的权利归各自权利人，代码许可不授予这些资源的使用权，详见[资料来源与许可边界](docs/第三方依赖与资料来源.md)

## 游玩方式

| 方式 | 适合场景 | 使用说明 |
| --- | --- | --- |
| 单人 | 自己练习、熟悉角色 | 支持的玩法准备好资料后可继续练习；清除浏览器数据会影响未导出的记录 |
| 多人 | 好友约局 | 创建房间并分享邀请码，其他玩家输入相同的码加入 |
| 活动团队 | 聚会、社团活动、娱乐赛 | 主持人安排队伍、回合及大屏展示，可转交主持权限 |

是否可以开局，由部署设置、玩法人数要求和题库状态共同决定。关闭的入口不代表玩法代码被删除。房间中的六位编号用于分辨同名玩家，不是密码，也不代表实名身份

详见[玩法与规则](docs/玩法与规则.md)

## V2 架构

| 部分 | 当前方案 |
| --- | --- |
| 前端 | React + TypeScript + Vite，Tailwind CSS，Radix 交互组件与项目 UI 组件 |
| 服务端 | Go `net/http`，模块化单体，统一提供页面、API 和实时通知 |
| 存储 | SQLite WAL，单个服务进程拥有一个数据目录 |
| 题库 | JSON 输入编译为带校验和的内容包，按玩法分别准入 |
| 接口 | [OpenAPI](api/openapi.json) 与[实时消息契约](api/realtime.schema.json) |
| 部署 | Docker 多阶段构建，非 root 运行，无需生产 Node、PostgreSQL 或 Redis |

版本以 [go.mod](go.mod)、[前端依赖](web/package.json)和锁文件为准。V2 是当前重构系列的名称，不代表已经完成正式发布或通过所有设备与容量验收

## 快速运行

需要 Go 1.26.1 或兼容的新版本、Node.js 24、pnpm 10.33.0 和 GNU Make。Windows 建议使用 WSL2，或按[Windows 指南](docs/Windows部署指南.md)直接运行命令

在仓库根目录执行：

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

打开 <http://localhost:8080>。`make dev` 默认将运行数据写入 `.cache/runtime`，直接运行 Go 则默认写入 `data/runtime`。服务不自动加载 `.env`；Compose 与直接运行的配置方式见[配置参考](docs/配置参考.md)

容器部署前同样需要生成内容包：

```bash
make content-compile
make content-verify
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

这会启动本机访问用的默认配置。手机访问、公网 HTTPS、反向代理和 Origin 设置见[快速开始](docs/快速开始.md)与各平台部署指南。SQLite 的运行数据必须放在本地磁盘，不能放在 NFS/SMB 共享目录

## 玩法目录

当前有 15 个玩法引擎及相应页面；能否游玩以当前内容包和 `/api/v1/modes` 的结果为准

| 类型 | 玩法 |
| --- | --- |
| 角色知识 | 经典弗一把、适性交叉占格、档案排序、血统 |
| 观察与记忆 | 学园记忆翻牌、听音辨马 |
| 历史与关联 | 历史赛况复原、同场之路 |
| 编队与策略 | 公开编队、回合制策略赛道、档案交易 |
| 聚会与合作 | 团队情报拼图、默契问答、创意企划、假档案发布会 |

听音辨马与假档案发布会受内容审校限制，不能仅凭已有代码就宣传为已开放。题量、缺图和数据缺口应查看本次编译报告，不能用旧统计代替

## 开发与检查

```bash
make help
make test
make test-race
make web-typecheck
make web-lint
make web-copy-check
make web-test
pnpm --dir web run api:check
go run ./cmd/content contract --spec api/openapi.json --routes
```

完整检查前先完成前端构建、嵌入和内容编译。`make check` 会重新构建前端并校验嵌入目录，修改前端后应先同步嵌入产物；`make ci` 另含契约检查。完整说明见[测试与质量检查](docs/测试与质量检查.md)

## 文档

- [文档索引](docs/文档索引.md)：按玩家、部署者、贡献者选择阅读入口
- [技术架构](docs/技术架构说明.md)：模块边界、数据流与安全约束
- [维护与更新](docs/维护与更新指南.md)：备份、更新、回滚和故障排查
- [V2 迁移指南](docs/V2迁移指南.md)：旧环境到当前架构的切换边界
- [贡献指南](CONTRIBUTING.md)、[安全策略](SECURITY.md)、[行为准则](CODE_OF_CONDUCT.md)

公开仓库保留构建所需的数据快照、来源记录、人工审校数据和可重复使用的测试工具，临时工作资料不属于构建与部署的依赖

## 当前边界

- 浏览器中的单人练习可被玩家修改，不能据此证明比赛成绩可信；正式计分使用服务端裁决
- 匿名身份依赖会话凭据，浏览器指纹与 IP 只能辅助限制滥用，不能证明“一人一号”
- 简体中文、繁体中文和日语资源需要持续进行地区用词与实机复核，具备翻译文件不等于母语审校完成
- 容量配置是保护上限，不是实测承载承诺；上线活动前应使用目标设备和网络进行演练
- 资料来自社区维护的快照，可能存在遗漏、推定值及更新延迟；不要把缺失值当作事实
