# spec-driftcheck

插件业务价值 spec 体系工具箱：脚手架生成、结构校验、PR 漂移提醒。
源自 tektoncd-operator 插件的完整实践（107 REQ / 158 Scenario，方法论已验证）。

## 三个子命令

```bash
# 生成 spec/ 脚手架（生成即通过 check）
driftcheck init --plugin-name gitlab --spec-repo AlaudaDevops/gitlab-chart

# 结构校验：REQ/Scenario 格式、模糊词 lint、anchors 路径与 CRD 有效性、
# 反向 CRD 哨兵（已定义但未被 anchors 登记的 CRD → crd-uncovered，豁免用 ignoreCRDs）
driftcheck check --spec-dir spec --work-dir /tmp/repos --local-repo-root .  # --format json 输出结构化 findings

# PR 漂移提醒：变更文件命中锚点 → 发/幂等更新 PR 评论（无 GITHUB_TOKEN 时只打印）
driftcheck notice --repo-name <repo-key> --changed-files changed.txt --spec-dir spec \
  --github-repo <owner/repo> --pr <n> --spec-link <spec 浏览地址>
```

## 工具镜像

```
ghcr.io/yhuan123/spec-driftcheck:latest   # 含 git + driftcheck 二进制，供 Tekton Task 使用
```

## 从零搭建一个插件的 spec 体系

- 流程权威：[docs/playbook.md](docs/playbook.md)（7 步，含每步完成判据；平台无关，任何 AI agent 可执行）
- Claude Code 用户：把 [skills/spec-bootstrap](skills/spec-bootstrap) 复制/软链到 `~/.claude/skills/` 后说"给 xxx 插件建 spec 体系"
- 漂移自动修复（Codex 起草修复 PR，人审合并）：见 playbook 第 6 步第 5 项

## 开发

```bash
make test    # go test ./...
make build   # bin/driftcheck
```
