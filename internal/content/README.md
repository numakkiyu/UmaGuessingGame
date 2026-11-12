# 内容编译与发布

`internal/content` 与 `cmd/content` 将 `data/` 下的只读输入编译为带 manifest 和校验和的内容包。运行服务读取已发布包，不依赖资料采集工具、内部计划或临时审核文件

## 输入

- `data/wiki/`：角色、技能、原型及结构化快照
- `data/normalized/` 等现有规范化资料：按实际构建器读取范围使用
- `data/manual/`：字段修订、独立规则、问题清单与人工审校台账
- `data/assets/`：有来源记录的受控素材，正式头像位于 `data/assets/characters/avatars/`

编译器从随仓库维护的头像目录读取素材，不读取私人工作目录。文件必须真实存在并校验，清单中的 cached 标记不是文件存在性证明

## 命令

从仓库根目录运行：

```bash
go run ./cmd/content build --root . --dry-run
go run ./cmd/content build --root . --out content
go run ./cmd/content verify
go run ./cmd/content current
go run ./cmd/content sections
```

`build --evidence <path>` 可额外输出本次构建报告；本地报告写入忽略目录，不提交会话和运行证据。`verify --dir` 接受具体包目录，不是包含 current 的父目录；省略 `--dir` 时解析当前包，使用其他发布根时传 `--out`

## 输出

```text
content/
  current
  pack-<hash>/
    manifest.json
    public/
      ...公开题库、练习资料、索引和受控素材
    private/
      ...秘密答案、布局与仅服务端使用的数据
```

实际 section 列表由 `sections` 输出。包内路径是发布契约，公开图像路径须与实际文件一致。服务端不将整个包目录作为公共文件根，私有文件不依赖“前端不显示”保护

发布先写 staging，校验完成后改名并更新 current 指针，失败保留旧包。不要在已发布目录覆盖文件；需要更新时重新编译，保留旧包用于恢复和回滚。同样输入与编译时钟应产生可重复结果，测试显式控制时间，不假定不同时间构建一定同一 hash

## 内容准入

引擎支持一种玩法不代表已有合格题目。各模式分别检查需要的字段、实体、素材、关系和审校状态，不能通过伪造审校台账或将未知字段填 0 达到开放条件

当前引擎覆盖 15 种玩法，常规包中听音与假档案仍受素材/语义审校约束。准确状态与题量读本次 manifest、质量报告及运行实例的 modes API，不在本文件维护会过期的硬编码统计

重要约束包括：经典比较向量无法区分的角色不进入唯一答案竞赛题；历史名次缺口不重新编号；需图片的题目排除缺图条目；记忆牌面不通过公开角色真名泄露布局；赛事边必须有来源；情报拼图满足可解性要求；规则题使用明确版本的规则输入

音频的可解码与语义审校是不同检查。真实事实的来源 URL 与人工确认也不能互相替代，未审校内容不能声称已经核实

## 接口与测试

`Pack` 为玩法提供 `Section(name)`、`Version()` 及按权限使用的私有内容。稳定 section 名称与字段由构建器和相应规则引擎共同维护，修改时同时验证读写两端

```bash
go test ./internal/content/... -count=1
go test ./internal/e2e/... -count=1
make content-verify
```

测试应覆盖清单完整性、篡改拒绝、原子发布、资料缺口、公开私有隔离、资源路径和可重复输出。编译或测试成功不表示资料事实、难度、平衡和翻译已经人工审核

构建使用与数据来源见[资料快照](../../data/wiki/README.md)、[配置参考](../../docs/配置参考.md)和[第三方来源](../../docs/第三方依赖与资料来源.md)
