# Contributing Guide

感谢你考虑参与 赛马娘猜猜乐。

本项目希望保持“可直接部署、可持续维护、对玩家友好”的开源质量，因此提交前请先快速阅读这份文档。

## 贡献方式

你可以通过以下方式参与：

- 提交 Bug 报告
- 提交功能建议
- 改进界面、交互或可访问性
- 补充测试、脚本或部署文档
- 修正数据、题库或素材台账问题

## 开始之前

1. 先搜索现有 Issue，避免重复提交。
2. 如果改动较大，先开一个 Issue 说明目标与范围。
3. Fork 仓库并从最新分支开始开发。

推荐分支命名：

- `feat/<topic>`
- `fix/<topic>`
- `docs/<topic>`
- `chore/<topic>`

## 本地开发

### 环境要求

- Node.js 22 或更高版本
- npm
- Docker 与 Docker Compose V2

### 初始化步骤

```bash
npm install
cp .env.example .env
npm run prepare:data
npm run db:push
npm run db:seed
```

如需容器化开发，请查看 [docs/Linux部署指南.md](./docs/Linux部署指南.md) 与 [docs/Windows部署指南.md](./docs/Windows部署指南.md)。

## 常用命令

```bash
npm run dev
npm run lint
npm run test
npm run build
npm run ci:check
```

如果你的改动影响数据链路，也请执行：

```bash
npm run prepare:data
```

## 提交规范

建议使用约定式提交前缀：

- `feat:`
- `fix:`
- `docs:`
- `refactor:`
- `test:`
- `chore:`
- `ci:`
- `build:`

示例：

- `feat: add friend room rematch confirmation flow`
- `fix: prevent active game answer from leaking on refresh`
- `docs: refresh deployment guide and project overview`

## Pull Request 要求

提交 PR 前请确认：

- 改动范围明确，避免把无关修改混在一起
- 本地已经运行必要的校验命令
- 涉及玩法、数据、配置或部署时，同步更新了对应文档
- 没有提交密钥、日志、缓存、构建产物或临时测试文件

PR 描述建议包含：

- 改动背景
- 解决方案
- 验证方式
- 是否有兼容性影响

## 文案与界面约定

- 玩家可见文案要自然、清晰，避免实现细节式表达
- UI 改动需要同时兼顾桌面端与移动端
- 不要在页面上暴露接口、配置或内部机制等开发术语

## 数据与资源约定

- 正式运行链路只依赖仓库内数据文件
- 不要引入未经确认的外部数据源
- 图片、资料来源和缓存策略改动，需要同步更新台账或说明文档

## Issue 与讨论建议

适合开 Issue 的情况：

- 可稳定复现的 Bug
- 明确的功能提案
- 部署兼容性问题
- 数据遗漏或题库规则异常

不适合直接提交代码的情况：

- 需求范围不明确
- 玩法规则尚未达成一致
- 需要先确认题库准入或资源来源

## License

向本仓库提交代码即表示你同意该贡献在项目的 [MIT License](./LICENSE) 下发布。

