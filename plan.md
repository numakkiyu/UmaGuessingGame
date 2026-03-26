# plan.md

## 当前进度

- [x] 每次开始任务先审查 `AGENTS.md`
- [x] 涉及页面布局时同步审查 `docs/ui-layout-guidelines.md`
- [x] 初始化 `UmaGuessingGame` 正式 Git 仓库
- [x] 建立 `Next.js + TypeScript + App Router + Tailwind CSS` 基础骨架
- [x] 完成 `Dockerfile`、`docker-compose.yml`、`docker-compose.dev.yml`、`.env.example` 和统一配置模块
- [x] 将五份上游文档同步进仓库 `docs/`，并清理为仓库相对路径
- [x] 建立标准目录、基础仓库文件和 `plan.md`
- [x] 整理 `data/characters`、`data/manual`、`data/assets` 基础结构
- [x] 实现题库构建脚本、搜索索引脚本和基础测试
- [x] 实现受控资源代理、文件缓存、素材清单和缓存索引
- [x] 实现 `Drizzle + PostgreSQL` 基础表结构、seed 脚本和单人模式 API 骨架
- [x] 补齐 Docker 首次启动数据库初始化链路
- [x] 实现移动端优先首页、搜索输入、最近猜测、结果表和结算弹窗
- [x] 将玩家可见文案调整为面向玩家和普通用户的口吻
- [x] 跑通 `npm run question-bank:build`
- [x] 跑通 `npm run search-index:build`
- [x] 增加 `racing-profile` 待补清单脚手架
- [x] 跑通 `npm run typecheck`
- [x] 跑通 `npm run test`
- [x] 跑通 `npm run lint`
- [x] 跑通 `npm run build`
- [x] 以仓库内数据为唯一正式来源，移除外站抓取脚本
- [x] 将 `question_bank_ready.json`、搜索索引与文档同步到当前真实状态
- [x] 补齐 GitHub Actions、Dependabot、Issue / PR 模板等仓库自动化
- [x] 重做首页关键 UI、分享入口和更贴近文档要求的成品化布局

## 当前产物状态

- `question_bank_candidates.json`: `118`
- `question_bank_ready.json`: `118`
- `question_bank_blocked.json`: `23`
- `public/search/characters.json`: `118`

## 当前非阻塞后续

- [ ] 继续补齐那 `15` 位基础字段缺失的已上线角色，扩大正式题库
- [ ] 持续预热本地图片缓存，降低首次命中时的回源概率
- [ ] 视需要补充更多端到端测试与部署监控
