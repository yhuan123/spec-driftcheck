# 插件业务价值 Spec 体系搭建 Playbook

> 读者：执行搭建的 AI agent 或工程师。每步都给出**做什么**与**完成判据**。
> 本流程源自 tektoncd-operator 插件的完整实践（8 组件仓库、107 REQ / 158 Scenario），方法论已验证。
> 工作量分布参考：脚手架 ~5%，起草+质量验证 ~80%，机制接入 ~15%。

## 体系一图流

```
建设期：脚手架 → 能力域划分 → 逐域起草 → 三层质量验证 → 覆盖审计 → 机制接入 → 试点验证
运行期：组件 PR 触碰锚点 → 机器人评论提醒（人裁决）+ 每日定时全量校验兜底
验收期：发版时 AI 按 spec 逐 Scenario 临时生成验收代码执行 → 报告归档
```

核心原则：**spec 是验收唯一权威，独立于实现**——严禁引用开发编写的 feature 文件/内部函数/UI 控件；机制是"漂移提醒"而非"自动更新"，spec 由人维护权威版本。

---

## 第 1 步：脚手架

```bash
driftcheck init --plugin-name <插件名> --spec-repo <owner/主仓库> [--out spec]
```

- 主仓库选插件的**交付单元仓库**（operator/chart 仓库），spec 跟随 release 分支天然版本对齐。
- **完成判据**：`driftcheck check --spec-dir spec --local-repo-root .` exit 0（脚手架天然绿）。

## 第 2 步：能力域划分

1. 收集输入：产品文档/官方能力面、各组件 README、与上游的差异（patches/自研代码）。
2. 把所有组件的能力**合并去重**，按"用户可感知的业务价值"切 N 个域（参考粒度：5~10 个）。
3. 每个域一句话用户价值 + 主要贡献组件，写入 DESIGN.md §3 与 INDEX.md。
4. 明确**范围边界**（哪些相邻插件能力不在本 spec 内）与横切关注（HA、RBAC、生命周期等——不单设域，融入相关 REQ）。
- **完成判据**：用户确认划分；每个域能回答"删掉这个域，用户失去什么"。

## 第 3 步：逐域起草

对每个能力域，复制 `capabilities/D1-example/` 改名，撰写 spec.md 三层：

- **业务价值层**（人写，产品/客户读）：2~4 句价值叙述 + 适用场景 + 与上游差异。
- **验收需求层**：从已验证资产**反向提炼**（现有测试 Scenario 只作行为素材，**用业务语言独立重写**，产出不得引用源文件）：
  - REQ 头：`### REQ-D<n>-<两位序号>: <标题> (P0|P1|P2[, planned])`，planned=功能未交付（注意：**无测试 ≠ 未交付**）；
  - 主句：EARS 句式（WHEN/WHILE/IF…THEN + SHALL），一个 REQ 只说一件事；
  - Scenario：`#### Scenario:` + `- GIVEN/WHEN/THEN`（AND 可选），一个 Scenario 一个判定；
  - 可量化：禁模糊词（合理/正常/适当/尽快/友好），时限/数量/状态值明确；CRD 字段写全限定路径。
- **验收门禁层**：默认 P0=100%、P1≥95%，按域覆盖。
- 同步填写 anchors.yaml（路径锚点选**变更频率与行为耦合度高**的：测试目录、patches、CRD types——误报率决定体系存亡）与 sync/repos.yaml。
- **完成判据**：每域 `driftcheck check` exit 0。

## 第 4 步：三层质量验证（核心纪律，每层独立）

| 层 | 执行者 | 内容 |
|---|---|---|
| 1. 起草自检 | 起草 agent | 对照格式契约逐条自查 |
| 2. 独立回源 review | **另起**一个 reviewer（不共享起草上下文） | 抽样或全量 REQ **回源逐字验证**：对照产品文档/代码确认行为描述真实、优先级合理、planned 标记准确。**不信任起草者的自报告** |
| 3. 机器全量校验 | `driftcheck check` | 结构/模糊词/锚点有效性 |

- 三层**全部通过**才算数；reviewer 发现的问题修复后须重跑第 3 层。
- **完成判据**：reviewer 出具逐条验证记录；check exit 0。

## 第 5 步：覆盖审计

对照**产品能力面**（不是对照已写的 spec）反向找漏：列出产品文档声明的每项能力 → 检查是否有 REQ 覆盖 → 补缺。

- 经验值：tekton 这轮审计补了 44 REQ（约 +40%），其中约 1/3 是"已交付但无测试覆盖"的功能——这份清单可反哺测试团队。
- 审计报告归档 `spec/reports/coverage-audit-<日期>.md`。
- **完成判据**：审计报告留档；补充后三层验证重跑通过。

## 第 6 步：机制接入

1. spec 合入主仓库 main（**后续步骤都依赖此步**：两个 Task 均 clone 主仓库默认分支取 spec）。
2. 各组件仓库 PAC namespace 建 secret：`kubectl create secret generic spec-drift-github-token --from-literal=token=<bot-token>`（bot 需各仓库 PR 评论权限）。
3. 各组件仓库 `.tekton/` 加 PipelineRun 引用 `spec-drift-notice` Task（模板在 `spec/sync/tasks/`，只改 `repo-name` param）。建议先 1 个仓库试点。
4. 主仓库所在集群 apply `spec/sync/tasks/scheduled-drift-check.yaml`（每日定时全量兜底）。
5. （可选，GitHub 托管仓库）启用漂移自动修复：复制 `spec/sync/workflows/spec-drift-autofix.yaml` 到主仓库 `.github/workflows/`，repo secrets 配置 `OPENAI_API_KEY`。每日全量 check 发现漂移后由 Codex 起草修复，经护栏（只许改 spec/、禁改 drift-check.yaml）与 check 质量门后，向固定分支 `spec-drift-autofix` 提幂等 PR，**合并权在人**。启用后集群侧 `scheduled-drift-check.yaml` 可不再部署（离线集群仍用它）。
- **完成判据**：见第 7 步试点。

## 第 7 步：试点验证

在试点仓库开一个 touch 锚点路径文件的 PR，验证：

1. 机器人评论出现，列出正确的能力域与关联 REQ；
2. 再 push 一次，评论**幂等更新**（同一条评论，不重复发）；
3. 改动不命中锚点的 PR **不**收到评论（负样本）。
4. （启用 autofix 时）人为制造漂移（如临时从某 anchors.yaml 删一个已登记 CRD 并合入 main）→ 手动 `workflow_dispatch` 触发 → 验证：修复 PR 出现且内容正确；全绿时触发 → 不开 PR；漂移未合并前连跑两次 → 同一 PR 被更新不重复；漂移含"消失的 CRD"→ PR body 出现 ⚠️ 高亮。
- **完成判据**：三项全过 → 推广其余组件仓库（每仓库只改 `repo-name`）。

---

## 本地自测速查（不依赖集群）

```bash
# 结构+锚点全量校验（兄弟仓库已本地 checkout 时用 --work-dir 指向其父目录免克隆）
driftcheck check --spec-dir spec --work-dir <repos-parent> --local-repo-root .

# notice 干跑（无 GITHUB_TOKEN 时只打印评论不发送）
echo "path/that/hits/anchor.yaml" > /tmp/changed.txt
driftcheck notice --repo-name <repo-key> --changed-files /tmp/changed.txt --spec-dir spec
```

## 新增能力的同步盲区与分层防御

锚点是白名单，**白名单不认识名单外的新事物**：新增文件落在已有 glob 内（目录级锚点）→ notice 已覆盖；全新目录/全新能力 → 实时机制无信号。防御分层：

1. **目录级 glob**（写锚点时就用 `dir/**` 而非具体文件）；
2. **反向 CRD 哨兵**（check 内建）：仓库中已定义但未被任何 anchors 登记的 CRD → `crd-uncovered` finding。新 CRD ≈ 新能力面，信号强、误报低；确认无需覆盖的 kind 加入 `drift-check.yaml` 的 `ignoreCRDs`；
3. **周期覆盖审计**（第 5 步）绑定发版流程作为前置项——新能力必然出现在产品文档/release notes，发现延迟 = 一个发版周期，对发版验收够用；
4. **流程前移**（治本）：新功能需求阶段先写 REQ（标 planned），spec 先于代码。

## 已知坑

- `planned` 判定看**功能是否交付**，不看有没有测试；
- anchors 路径写太宽会误报刷屏，写太窄会漏报——从测试目录/patches/CRD types 起步，按误报率迭代；
- 评论 bot 的 token 权限：对组件仓库需要 issues/PR 写权限；
- 定时任务依赖集群已有 ScheduledTrigger CRD（tektoncd-enhancement 提供）；无此 CRD 的集群可改用 CronJob 形态自行改写。
- autofix workflow 的 cron 是 UTC（`0 18 * * *` = 北京时间 02:00），改时区要换算；
- autofix 的质量门只覆盖机器可判定层（格式/锚点/CRD 存在性），**语义对错完全靠 PR 人审**——review 时重点看 ⚠️ 高亮的疑似移除项与 `<!-- draft by autofix -->` 草稿 REQ。
