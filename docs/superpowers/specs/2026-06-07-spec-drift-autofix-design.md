# spec 漂移自动修复（autofix）设计

> 日期：2026-06-07
> 状态：设计已确认，待实现
> 背景对话结论：用户接受 spec 定位从"独立验收权威"转为"忠实反映代码现状的镜像"；
> 自动更新 = 机器起草修复 PR 到主仓库，**合并权在人**（Dependabot 模式）。

## 1. 目标与非目标

**目标**：漂移被发现后，不再只输出红灯/报告，而是由 AI agent 自动起草修复
（anchors.yaml + spec.md 语义层都改），以幂等 PR 形式提交主仓库，人审合并。

**非目标（明确出范围）**：

- Tekton 集群内 agent 闭环（离线集群场景）——现有 `scheduled-drift-check.yaml`
  模板保留并注释标注"离线集群用"，本期不做 agent 化；
- `driftcheck fix` 确定性机械修复子命令——二期按需再做（agent 顺手能改
  anchors，check 重跑兜底，首发无必要）；
- PR 时 `spec-drift-notice` 提醒机制——保留不动，与本机制互补
  （实时提醒 + 日级自动修复兜底）。

## 2. 已确认的决策

| 问题 | 决策 |
|---|---|
| 痛点 | 不是"没人看见提醒"，而是要 spec 自动跟上代码 |
| 更新深度 | anchors.yaml + spec.md 都改（含 REQ 措辞、新 CRD 草稿 REQ、删废弃 REQ） |
| 执行者 | Codex（`openai/codex-action` exec 模式，secrets：`OPENAI_API_KEY`） |
| 落地方式 | 幂等 PR 到主仓库（`peter-evans/create-pull-request` 固定分支模式），人审合并 |
| 编排 venue | GitHub Actions（每日 cron + `workflow_dispatch`） |

## 3. 架构与数据流

```
.github/workflows/spec-drift-autofix.yaml
│
├─ 1. checkout 主仓库（含 spec/）
├─ 2. driftcheck 二进制来源：job 以工具镜像（ghcr.io/yhuan123/spec-driftcheck）为 container 运行，免下载
├─ 3. driftcheck check --format json > findings.json    ← driftcheck 唯一代码改动
│      └─ exit 0 → 全绿，workflow 成功结束
├─ 4. openai/codex-action (exec)
│      prompt = autofix-prompt 模板 + findings.json
│      任务：修 anchors.yaml + spec.md，只许动 spec/
├─ 5. 护栏硬检查（git diff --name-only：超出 spec/ 或触碰 drift-check.yaml → fail）
├─ 6. driftcheck check 重跑（确定性质量门，不信任 agent 自报告；仍红 → fail，不开 PR）
└─ 7. peter-evans/create-pull-request
       固定分支 spec-drift-autofix；有 open PR 则更新，无则创建（幂等）
```

核心模式：**generator-verifier 分离**。Codex 是不可信生成器，现有 `check`
是确定性验证器；PR 只在机器可判定层面全绿时出现，人审只关注语义。

## 4. 组件改动清单

| 组件 | 改动 |
|---|---|
| `internal/runner` / `internal/report` / `main.go` | `check` 新增 `--format json`（输出 findings 数组；空 findings 输出 `[]`；exit code 语义不变） |
| `internal/scaffold/templates/sync/workflows/spec-drift-autofix.yaml.tmpl` | 新增 GHA workflow 模板，`init` 渲染到 `spec/sync/workflows/`，人工复制到主仓库 `.github/workflows/`（沿用 Tekton Task 人工 apply 的交付模式） |
| `internal/scaffold/templates/sync/autofix-prompt.md.tmpl` | 新增 agent 修复任务书模板（语义纪律措辞由用户执笔，实现时留 TODO） |
| `docs/playbook.md` | 第 6 步机制接入增补 autofix 接入说明；新增试点验证条目 |

## 5. Prompt 护栏（两层）

原则：**能用确定性手段强制的绝不靠 prompt**。

| 护栏 | 强制方式 |
|---|---|
| 只许改 `spec/` 下文件 | workflow 硬检查（git diff 超范围即 fail） |
| 禁改 `sync/drift-check.yaml` | workflow 硬检查——agent 不得用 ignore/ignoreCRDs 给自己"修绿"，豁免是人的特权 |
| 格式契约（REQ 头/EARS/GWT/禁模糊词） | check 重跑兜底；prompt 附契约摘录提高一次通过率 |
| 语义纪律 | 仅 prompt + 人审（见下） |

语义纪律（prompt 内容，按 finding 类型）：

- `crd-defined`（CRD 改名/消失）：改名 → 同步更新 anchors + REQ 措辞；
  消失 → 删除对应 REQ，但 PR body 必须 ⚠️ 高亮"此能力疑似被移除，请确认是否误删"
  ——删除永远显眼，不静默；
- `crd-uncovered`（新 CRD）：anchors 登记 + 起草标 `planned` 的 REQ 草稿
  （带 `<!-- draft by autofix -->` 注释）；
- `anchor-path` 失配：探测改名/移动后的新路径更新 glob；找不到则删除并在 PR body 说明；
- PR body 由 agent 生成：findings 原表 + 逐条修复说明 + 高风险标记，写入文件供
  create-pull-request 使用。

## 6. 错误处理

| 故障 | 行为 |
|---|---|
| check 基础设施错误（克隆失败等） | step 3 非零且无 findings.json → workflow 红，与"有漂移"路径区分 |
| codex 修完仍不绿 | step 6 红 → 失败、不开 PR；日志保留前后两份 check 报告；不自动重试（失败率高再加） |
| secrets 缺失 | 首 step fail-fast |
| cron 与手动触发重叠 | `concurrency` group 取消旧 run |

## 7. 测试与验收

1. **Go 单测**：`--format json` 序列化（空 findings、ReqID 为空的 finding）、
   exit code 不变；
2. **试点验证**（四个判据全过才算落地）：
   - 人为制造漂移（临时从 anchors 删一个已登记 CRD）→ `workflow_dispatch` →
     PR 出现、内容正确、护栏检查通过；
   - 全绿状态跑 → 不开 PR（负样本）；
   - 漂移未修复连跑两次 → 同一 PR 被更新，不重复开（幂等）；
   - 漂移含"消失的 CRD" → PR body 出现 ⚠️ 高亮（高风险路径）。
