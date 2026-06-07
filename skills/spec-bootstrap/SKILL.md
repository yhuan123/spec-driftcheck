---
name: spec-bootstrap
description: 为一个插件从零搭建业务价值 spec 体系（spec 文档 + 漂移提醒机制）。当用户说"给 xxx 插件建 spec 体系"、"搭建业务价值 spec"、"spec bootstrap"、"复制 tekton 的 spec 流程"时使用。
---

# spec-bootstrap：插件业务价值 Spec 体系搭建

本 skill 是薄壳：**唯一权威流程是 playbook**，按它执行，本文件只做编排约定。

## 第 0 步：加载 playbook

按以下顺序定位（取第一个存在的）：

1. `${CLAUDE_PLUGIN_ROOT}/docs/playbook.md`（作为 acceptance-spec 插件安装时，playbook 随插件分发）；
2. 本仓库副本 `docs/playbook.md`（直接在 spec-driftcheck 仓库内工作时）；
3. 拉取 `https://raw.githubusercontent.com/yhuan123/spec-driftcheck/main/docs/playbook.md`。

## 第 1 步：收集 3 个输入（一次问清）

1. **插件名**（如 gitlab）；
2. **spec 主仓库**（owner/repo，插件的交付单元仓库）；
3. **组件仓库清单**与**产品能力面文档入口**（官方文档/README/产品手册链接或路径）。

## 第 2 步：按 playbook 第 1~7 步执行

编排纪律（不可省略）：

- **每步结束 = 人工 gate**：展示产出与完成判据结果，用户确认后才进下一步；
- 工具获取：优先用镜像 `ghcr.io/yhuan123/spec-driftcheck:latest` 内的二进制；本地开发可 `go install github.com/yhuan123/spec-driftcheck@latest`；
- 起草（playbook 第 3 步）可按能力域并行派 subagent，但**三层质量验证（第 4 步）的独立 reviewer 必须与起草 agent 隔离上下文**，回源逐字验证，不信自报告；
- 机制接入（第 6 步）的推送/开 PR/集群 apply 是**外向操作**，逐项征得用户明确授权；
- 进度汇报格式：`[进度] 步骤 X/7: xxx`。

## 失败处理

任何步骤的完成判据不满足：停下，1-2 句报告原因，等用户指示。不自动重试、不擅自换方案。
