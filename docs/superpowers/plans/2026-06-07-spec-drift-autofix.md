# spec 漂移自动修复（autofix）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 漂移检测后由 Codex 自动起草修复（anchors + spec.md），经确定性护栏与 check 质量门后提幂等 PR 到主仓库。

**Architecture:** generator-verifier 分离——GHA workflow 编排：`driftcheck check --format json` 产出 findings → `openai/codex-action` 修复 → workflow 硬护栏（改动范围）+ check 重跑（质量门）→ `peter-evans/create-pull-request` 幂等 PR。driftcheck 只新增 JSON 输出；workflow 与 prompt 以 scaffold 模板交付。

**Tech Stack:** Go 1.24（text/template、encoding/json）、GitHub Actions（openai/codex-action@v1、peter-evans/create-pull-request@v7）。

**设计文档：** `docs/superpowers/specs/2026-06-07-spec-drift-autofix-design.md`

---

## 背景速览（给零上下文执行者）

- 本仓库是 Go CLI `driftcheck`，三个子命令：`init`（渲染 spec 脚手架，模板在 `internal/scaffold/templates/`，经 `go:embed` 打包）、`check`（校验 spec 与代码一致性，输出 markdown 报告，有 findings 时 exit 1）、`notice`（PR 锚点提醒）。
- `check` 的 findings 类型：`spec-structure | fuzzy-word | anchor-path | crd-defined | crd-uncovered`（见 `internal/runner/runner.go` 的 `Finding`）。
- scaffold 模板用 Go text/template 默认 `{{ }}` 定界符；**GHA workflow 文件里的 `${{ }}` 会与之冲突**，本计划为 `sync/workflows/` 下的模板启用 `[[ ]]` 定界符。
- 工具镜像 `ghcr.io/yhuan123/spec-driftcheck` 是 alpine 基底、二进制在 `/usr/local/bin/driftcheck`、`CGO_ENABLED=0` 静态编译。
- 测试惯例：表驱动少、直接 `t.TempDir()` 搭真实目录树（见 `internal/runner/runner_test.go` 的 `mustWrite`）；运行 `make test`（= `go test ./...`）。

---

### Task 1: `check --format json`

**Files:**
- Modify: `internal/runner/runner.go:19-24`（Finding 加 json tag）
- Modify: `internal/report/report.go`（新增 RenderJSON）
- Create: `internal/report/report_test.go`
- Modify: `main.go:74-94`（runCheck 加 --format）

- [ ] **Step 1: 写失败测试**

创建 `internal/report/report_test.go`：

```go
package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yhuan123/spec-driftcheck/internal/runner"
)

// TestRenderJSON_Empty：零 findings 输出 "[]"（不是 "null"，下游 jq 依赖数组语义）。
func TestRenderJSON_Empty(t *testing.T) {
	out, err := RenderJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("want [], got %q", out)
	}
}

// TestRenderJSON_Fields：字段名与 drift-check.yaml 的 reqId 惯例一致，可逆序列化。
func TestRenderJSON_Fields(t *testing.T) {
	out, err := RenderJSON([]runner.Finding{
		{Capability: "D1-demo", ReqID: "REQ-D1-01", Check: "crd-defined", Detail: "未找到 CRD"},
		{Capability: "", ReqID: "", Check: "crd-uncovered", Detail: "NewKind 未登记"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("输出应是合法 JSON 数组: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	first := got[0]
	for k, want := range map[string]string{
		"capability": "D1-demo", "reqId": "REQ-D1-01",
		"check": "crd-defined", "detail": "未找到 CRD",
	} {
		if first[k] != want {
			t.Errorf("first[%q] = %q, want %q", k, first[k], want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/report/ -v`
Expected: FAIL，`undefined: RenderJSON`

- [ ] **Step 3: 最小实现**

`internal/runner/runner.go` 给 Finding 加 json tag（保留原注释）：

```go
// Finding 是一处漂移。
type Finding struct {
	Capability string `json:"capability"`
	ReqID      string `json:"reqId"` // anchors/CRD 级问题为空；fuzzy-word 可留空（见 Detail）
	Check      string `json:"check"` // spec-structure | fuzzy-word | anchor-path | crd-defined | crd-uncovered
	Detail     string `json:"detail"`
}
```

`internal/report/report.go` 末尾追加：

```go
// RenderJSON 输出 findings 的 JSON 数组（零 findings 输出 []，供下游 jq 消费）。
func RenderJSON(findings []runner.Finding) (string, error) {
	if len(findings) == 0 {
		return "[]", nil
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	return string(data), err
}
```

并在 import 块加入 `"encoding/json"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/report/ -v`
Expected: PASS（2 个测试）

- [ ] **Step 5: main.go 接入 --format**

`main.go` 的 `runCheck` 改为（仅展示函数体变化，flag 定义区与校验区）：

```go
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	specDir := fs.String("spec-dir", "", "spec 目录（含 capabilities/ 与 sync/）")
	workDir := fs.String("work-dir", "/tmp/driftcheck-repos", "跨仓库克隆工作目录")
	localRoot := fs.String("local-repo-root", "", "local 仓库（tektoncd-operator）根目录")
	format := fs.String("format", "text", "输出格式：text|json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specDir == "" || *localRoot == "" {
		return fmt.Errorf("--spec-dir 与 --local-repo-root 必填")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("未知 --format %q（支持 text|json）", *format)
	}
	findings, err := runner.Run(*specDir, *workDir, *localRoot)
	if err != nil {
		return err
	}
	if *format == "json" {
		out, err := report.RenderJSON(findings)
		if err != nil {
			return err
		}
		fmt.Println(out)
	} else {
		fmt.Print(report.Render(findings))
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	return nil
}
```

注意：exit code 语义不变（有 findings → exit 1，与 format 无关）；非法 format 在跑校验**之前**报错。

- [ ] **Step 6: 全量回归 + 手工冒烟**

Run: `make test`
Expected: 全部 PASS

Run 冒烟（脚手架天然绿）:
```bash
go build -o /tmp/dc . && cd $(mktemp -d) && /tmp/dc init --plugin-name demo --spec-repo acme/demo && /tmp/dc check --spec-dir spec --local-repo-root . --format json; cd -
```
Expected: 输出 `[]`，exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/runner/runner.go internal/report/report.go internal/report/report_test.go main.go
git commit -m "feat(check): --format json 输出结构化 findings"
```

---

### Task 2: scaffold 支持 workflow 模板（`[[ ]]` 定界符）+ autofix workflow 模板

**Files:**
- Modify: `internal/scaffold/scaffold.go:30-64`（Render 按路径切换定界符）
- Create: `internal/scaffold/templates/sync/workflows/spec-drift-autofix.yaml.tmpl`
- Modify: `internal/scaffold/scaffold_test.go`（修已有断言 + 新增 workflow 断言）

**前置说明（必读）**：GHA 表达式 `${{ secrets.X }}` 含 `{{`，会被 Go text/template 默认定界符吞掉。方案：`sync/workflows/` 前缀下的模板用 `[[ ]]` 作 Go 模板定界符，GHA 语法原样直出。同理，已有测试 `TestRender_SubstitutesParams` 用 `strings.Contains(data, "{{")` 探测未渲染残留——workflow 渲染产物里合法存在 `${{`，该断言必须收紧为探测 `{{.` 与 `[[.`。

- [ ] **Step 1: 实现前先核对 action 输入名**

用 WebFetch 核对两个 action 的当前输入参数名（计划中的写法基于 2026-01 知识，以官方 README 为准）：
- `https://github.com/openai/codex-action`（关注：API key 入参名、prompt 文件入参、sandbox/写权限配置）
- `https://github.com/peter-evans/create-pull-request`（关注：`branch`/`body-path`/`delete-branch` 是否仍是 v7 语义）

若与下文模板不符，按官方 README 修正模板后再继续。

- [ ] **Step 2: 写失败测试**

`internal/scaffold/scaffold_test.go` 追加：

```go
// TestRender_WorkflowKeepsGHAExpressions：workflow 模板用 [[ ]] 定界符渲染，
// GHA 的 ${{ }} 原样保留，Go 模板变量被替换。
func TestRender_WorkflowKeepsGHAExpressions(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "spec")
	if _, err := Render(specDir, testParams); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(specDir, "sync/workflows/spec-drift-autofix.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "${{ secrets.OPENAI_API_KEY }}") {
		t.Error("GHA secrets 表达式应原样保留")
	}
	if !strings.Contains(s, testParams.ToolImage) {
		t.Error("ToolImage 应被渲染")
	}
	if strings.Contains(s, "[[") {
		t.Error("不应残留 [[ ]] 模板占位符")
	}
}
```

同文件修改 `TestRender_SubstitutesParams` 的残留检测（第 51 行附近）：

```go
		if strings.Contains(string(data), "{{.") || strings.Contains(string(data), "[[.") {
			t.Errorf("%s 含未渲染的模板占位符", f)
		}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/scaffold/ -v -run TestRender_WorkflowKeepsGHAExpressions`
Expected: FAIL，读不到 `sync/workflows/spec-drift-autofix.yaml`

- [ ] **Step 4: Render 支持按路径切换定界符**

`internal/scaffold/scaffold.go` 中把：

```go
		tmpl, err := template.New(rel).Parse(string(raw))
```

改为：

```go
		t := template.New(rel)
		if strings.HasPrefix(rel, "sync/workflows/") {
			// GHA workflow 含 ${{ }}，与默认定界符冲突，改用 [[ ]]。
			t = t.Delims("[[", "]]")
		}
		tmpl, err := t.Parse(string(raw))
```

- [ ] **Step 5: 创建 workflow 模板**

创建 `internal/scaffold/templates/sync/workflows/spec-drift-autofix.yaml.tmpl`：

```yaml
# spec 漂移自动修复：每日全量 check → Codex 起草修复 → 护栏与质量门 → 幂等 PR。
# 接入：复制本文件到主仓库 .github/workflows/，并在 repo secrets 配置 OPENAI_API_KEY。
# 设计：generator-verifier 分离——agent 是不可信生成器，driftcheck check 是确定性验证器。
name: spec-drift-autofix
on:
  schedule:
    - cron: "0 18 * * *" # 02:00 Asia/Shanghai
  workflow_dispatch: {}
concurrency:
  group: spec-drift-autofix
  cancel-in-progress: true
permissions:
  contents: write
  pull-requests: write
jobs:
  autofix:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: extract driftcheck binary
        # 工具镜像是 alpine 基底，不能作 container job（JS actions 的 node 不兼容 musl），
        # 故提取静态二进制到 runner。
        run: |
          docker create --name dc [[.ToolImage]]
          sudo docker cp dc:/usr/local/bin/driftcheck /usr/local/bin/driftcheck
          docker rm dc
          driftcheck 2>&1 | head -1 || true
      - name: drift check
        id: check
        run: |
          set +e
          driftcheck check --spec-dir spec --work-dir /tmp/driftcheck-repos \
            --local-repo-root . --format json > /tmp/findings.json
          code=$?
          set -e
          # 基础设施错误（克隆失败等）：非零退出且无 findings 输出 → 直接红，与漂移路径区分。
          if [ "$code" -ne 0 ] && [ ! -s /tmp/findings.json ]; then
            echo "driftcheck 基础设施错误，详见上方日志" >&2
            exit "$code"
          fi
          echo "drifted=$([ "$code" -ne 0 ] && echo true || echo false)" >> "$GITHUB_OUTPUT"
          cat /tmp/findings.json
      - name: compose prompt
        if: steps.check.outputs.drifted == 'true'
        run: |
          cat spec/sync/autofix-prompt.md > /tmp/prompt.md
          printf '\n## 本次漂移 findings（JSON）\n\n```json\n' >> /tmp/prompt.md
          cat /tmp/findings.json >> /tmp/prompt.md
          printf '\n```\n' >> /tmp/prompt.md
      - name: codex autofix
        if: steps.check.outputs.drifted == 'true'
        uses: openai/codex-action@v1
        with:
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          prompt-file: /tmp/prompt.md
          sandbox: workspace-write
      - name: guardrails
        if: steps.check.outputs.drifted == 'true'
        # 确定性护栏，不信任 prompt 约束：改动只许落在 spec/ 内，且禁改豁免配置。
        run: |
          changed=$(git diff --name-only)
          echo "agent 改动文件："; echo "$changed"
          outside=$(echo "$changed" | grep -v '^spec/' || true)
          if [ -n "$outside" ]; then
            echo "护栏违规：改动超出 spec/：" >&2; echo "$outside" >&2; exit 1
          fi
          if echo "$changed" | grep -q '^spec/sync/drift-check.yaml$'; then
            echo "护栏违规：禁止修改 spec/sync/drift-check.yaml（豁免是人的特权）" >&2; exit 1
          fi
      - name: verify green
        if: steps.check.outputs.drifted == 'true'
        # 确定性质量门：agent 修完必须全绿，否则不开 PR。
        run: |
          rm -rf /tmp/driftcheck-repos
          driftcheck check --spec-dir spec --work-dir /tmp/driftcheck-repos --local-repo-root .
      - name: ensure PR body
        if: steps.check.outputs.drifted == 'true'
        # agent 按任务书把修复说明写到 /tmp/pr-body.md；缺失时退化为原始 findings。
        run: |
          if [ ! -s /tmp/pr-body.md ]; then
            {
              echo '## spec 漂移自动修复'
              echo
              echo '⚠️ agent 未生成修复说明，请对照原始 findings 逐项审查改动：'
              echo
              echo '```json'
              cat /tmp/findings.json
              echo '```'
            } > /tmp/pr-body.md
          fi
      - name: create or update PR
        if: steps.check.outputs.drifted == 'true'
        uses: peter-evans/create-pull-request@v7
        with:
          branch: spec-drift-autofix
          title: "spec: 漂移自动修复（autofix）"
          commit-message: "spec: 漂移自动修复（autofix）"
          body-path: /tmp/pr-body.md
          delete-branch: true
```

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/scaffold/ -v`
Expected: 全部 PASS（含已有 3 个测试——`TestRender_InitIsGreen` 顺带验证新模板不破坏"生成即绿"）

- [ ] **Step 7: Commit**

```bash
git add internal/scaffold/scaffold.go internal/scaffold/scaffold_test.go internal/scaffold/templates/sync/workflows/spec-drift-autofix.yaml.tmpl
git commit -m "feat(scaffold): autofix workflow 模板（codex + 护栏 + 幂等 PR）"
```

---

### Task 3: autofix-prompt 模板（语义纪律）

**Files:**
- Create: `internal/scaffold/templates/sync/autofix-prompt.md.tmpl`
- Modify: `internal/scaffold/scaffold_test.go`（断言渲染产物）

**执行检查点**：本模板的"按 finding 类型的处置规则"一节是用户指定要亲自把关的领域内容。下文给出完整草稿（不是占位符），但**提交前必须暂停，请用户审改该节措辞**，按用户版本落盘。

- [ ] **Step 1: 写失败测试**

`internal/scaffold/scaffold_test.go` 追加：

```go
// TestRender_AutofixPrompt：prompt 模板渲染且含关键纪律。
func TestRender_AutofixPrompt(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "spec")
	if _, err := Render(specDir, testParams); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(specDir, "sync/autofix-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"demo-plugin", "drift-check.yaml", "planned", "/tmp/pr-body.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("prompt 应含 %q", want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/scaffold/ -v -run TestRender_AutofixPrompt`
Expected: FAIL，文件不存在

- [ ] **Step 3: 创建模板（草稿全文）**

创建 `internal/scaffold/templates/sync/autofix-prompt.md.tmpl`：

```markdown
# spec 漂移自动修复任务书

你是 {{.PluginName}} 插件业务价值 spec 体系的维护 agent。当前目录是主仓库
（{{.SpecRepo}}）的工作副本，`spec/` 下是 spec 体系。本任务书末尾附有本次
`driftcheck check` 发现的漂移 findings（JSON 数组）。你的任务：修改 `spec/`
下的文件消除全部漂移，使 `driftcheck check --spec-dir spec --local-repo-root .`
重新通过（exit 0）。

## 硬性约束（违反任何一条，修复会被流水线整体拒绝）

1. 只允许改动 `spec/` 目录下的文件；
2. 禁止修改 `spec/sync/drift-check.yaml`——不得用 ignore/ignoreCRDs 豁免来"修绿"；
3. 修完把修复说明写入 `/tmp/pr-body.md`（格式见文末）。

## spec 格式契约（摘录）

- REQ 头：`### REQ-D<n>-<两位序号>: <标题> (P0|P1|P2[, planned])`，
  `planned` = 功能尚未交付；
- 每个 REQ 至少一个 `#### Scenario:`，每个 Scenario 必须有
  `- GIVEN` / `- WHEN` / `- THEN` 行（`- AND` 可选）；
- REQ 主句用 EARS 句式（WHEN/WHILE/IF…THEN + SHALL），一个 REQ 只说一件事；
- GWT 行禁用模糊词：合理、正常、适当、尽快、友好——时限/数量/状态值必须明确；
- CRD 字段写全限定路径。

## 按 finding 类型的处置规则

<!-- 本节措辞由维护者把关，实现时经用户审改后落盘 -->

- `crd-defined`（anchors 登记的 CRD 在仓库中找不到）：先在仓库中找改名痕迹
  （相似 kind、git 历史、CRD manifest 目录）；确认改名 → 同步更新该能力域
  anchors.yaml 与 spec.md 中相关 REQ 的措辞；确认消失 → 删除对应 REQ 与
  anchors 登记，并在 `/tmp/pr-body.md` 用 ⚠️ 高亮"该能力疑似被移除，请人工
  确认不是误删"；
- `crd-uncovered`（仓库新 CRD 未被 anchors 登记）：在语义最接近的能力域登记
  该 CRD，并基于 CRD manifest 的字段起草一个标 `planned` 的新 REQ（含至少
  一个完整 GWT Scenario），REQ 标题下加一行 `<!-- draft by autofix -->`；
  没有语义接近的能力域时，登记到 anchors 后在 pr-body 中说明"建议人工评估
  是否新建能力域"；
- `anchor-path`（glob 无匹配文件）：在仓库中探测目录改名/移动后的新位置并更
  新 glob（保持目录级 `dir/**` 粒度）；确认路径已不存在 → 删除该 glob 并在
  pr-body 说明；
- `spec-structure` / `fuzzy-word`：按格式契约直接修复措辞，不改变 REQ 语义。

## /tmp/pr-body.md 格式

```
## spec 漂移自动修复

### 本次漂移
<findings 的 markdown 表格：能力域 | REQ | 类型 | 详情>

### 逐条修复说明
<每条 finding 一行：做了什么、为什么>

### ⚠️ 需要人工重点确认
<删除类改动逐条列出；无则写"无">
```

修完务必自验：`driftcheck check --spec-dir spec --local-repo-root .` 须 exit 0。
```

注意：本模板在 `sync/` 下但**不在** `sync/workflows/` 下，用默认 `{{ }}` 定界符（内容无 GHA 表达式）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/scaffold/ -v`
Expected: 全部 PASS

- [ ] **Step 5: ⏸ 用户检查点——语义纪律审改**

暂停，把"按 finding 类型的处置规则"一节呈现给用户审改（用户领域判断优先），按用户版本更新模板与（若措辞影响断言）测试，重跑 `go test ./internal/scaffold/`。

- [ ] **Step 6: Commit**

```bash
git add internal/scaffold/templates/sync/autofix-prompt.md.tmpl internal/scaffold/scaffold_test.go
git commit -m "feat(scaffold): autofix-prompt 修复任务书模板"
```

---

### Task 4: playbook 与 README 增补

**Files:**
- Modify: `docs/playbook.md`（第 6 步、第 7 步、已知坑）
- Modify: `README.md`

- [ ] **Step 1: playbook 第 6 步追加接入项**

在 `docs/playbook.md` 第 6 步（机制接入）列表末尾（第 74 行 `4. 主仓库所在集群 apply …` 之后）插入：

```markdown
5. （可选，GitHub 托管仓库）启用漂移自动修复：复制 `spec/sync/workflows/spec-drift-autofix.yaml` 到主仓库 `.github/workflows/`，repo secrets 配置 `OPENAI_API_KEY`。每日全量 check 发现漂移后由 Codex 起草修复，经护栏（只许改 spec/、禁改 drift-check.yaml）与 check 质量门后，向固定分支 `spec-drift-autofix` 提幂等 PR，**合并权在人**。启用后集群侧 `scheduled-drift-check.yaml` 可不再部署（离线集群仍用它）。
```

- [ ] **Step 2: playbook 第 7 步追加 autofix 试点判据**

在第 7 步（试点验证）的列表（第 80-83 行三项）之后追加：

```markdown
4. （启用 autofix 时）人为制造漂移（如临时从某 anchors.yaml 删一个已登记 CRD 并合入 main）→ 手动 `workflow_dispatch` 触发 → 验证：修复 PR 出现且内容正确；全绿时触发 → 不开 PR；漂移未合并前连跑两次 → 同一 PR 被更新不重复；漂移含"消失的 CRD"→ PR body 出现 ⚠️ 高亮。
```

- [ ] **Step 3: playbook 已知坑追加两条**

在"已知坑"小节（第 108 行起）列表末尾追加：

```markdown
- autofix workflow 的 cron 是 UTC（`0 18 * * *` = 北京时间 02:00），改时区要换算；
- autofix 的质量门只覆盖机器可判定层（格式/锚点/CRD 存在性），**语义对错完全靠 PR 人审**——review 时重点看 ⚠️ 删除类改动与 `<!-- draft by autofix -->` 草稿 REQ。
```

- [ ] **Step 4: README 增补**

`README.md` 在"三个子命令"代码块中 `driftcheck check` 注释行后补充 `--format json` 说明，将：

```bash
driftcheck check --spec-dir spec --work-dir /tmp/repos --local-repo-root .
```

改为：

```bash
driftcheck check --spec-dir spec --work-dir /tmp/repos --local-repo-root .  # --format json 输出结构化 findings
```

并在"从零搭建一个插件的 spec 体系"小节末尾追加一行：

```markdown
- 漂移自动修复（Codex 起草修复 PR，人审合并）：见 playbook 第 6 步第 5 项
```

- [ ] **Step 5: Commit**

```bash
git add docs/playbook.md README.md
git commit -m "docs: playbook/README 增补 autofix 接入与试点判据"
```

---

### Task 5: 端到端验证

**Files:** 无新增（验证性任务）

- [ ] **Step 1: 全量测试**

Run: `make test`
Expected: 全部 PASS

- [ ] **Step 2: 脚手架端到端冒烟**

```bash
go build -o /tmp/dc . && tmp=$(mktemp -d) && cd "$tmp" && \
/tmp/dc init --plugin-name gitlab --spec-repo AlaudaDevops/gitlab-chart && \
/tmp/dc check --spec-dir spec --local-repo-root . && \
cat spec/sync/workflows/spec-drift-autofix.yaml && cat spec/sync/autofix-prompt.md; cd -
```

Expected：init 列出新增的 `spec/sync/workflows/spec-drift-autofix.yaml` 与 `spec/sync/autofix-prompt.md`；check exit 0；workflow 内 `${{ }}` 完好、镜像名已渲染；prompt 内 `gitlab` 已渲染。

- [ ] **Step 3: workflow 语法静态检查（可选但建议）**

若本机有 `actionlint`（`brew install actionlint`）：

```bash
mkdir -p "$tmp/.github/workflows" && cp "$tmp/spec/sync/workflows/spec-drift-autofix.yaml" "$tmp/.github/workflows/" && actionlint -no-color "$tmp/.github/workflows/spec-drift-autofix.yaml"
```

Expected: 无错误输出（shellcheck 级别的 warning 酌情修复）

- [ ] **Step 4: 真实环境试点（需用户参与，不在本仓库完成）**

按 playbook 第 7 步第 4 项在目标插件仓库执行四判据试点。此步骤超出本仓库范围，作为交付后跟进项告知用户。

---

## Self-Review 记录

- **Spec 覆盖**：设计 §4 组件清单四项 ↔ Task 1（--format json）、Task 2（workflow 模板）、Task 3（prompt 模板）、Task 4（playbook/README）；§5 护栏 ↔ workflow guardrails step + prompt 硬性约束；§6 错误处理 ↔ drift check step 的基础设施错误分流、verify green、concurrency、fail-fast（secrets 缺失由 codex-action 自身报错，足够显式）；§7 测试 ↔ Task 1/2/3 单测 + Task 5 试点判据。无缺口。
- **占位符**：prompt 模板"语义纪律"节给出完整草稿 + 用户检查点（设计已确认的分工，非 TBD）。
- **类型一致性**：`RenderJSON(findings []runner.Finding) (string, error)` 在 Task 1 定义、main.go 调用一致；json tag `reqId` 与测试断言一致；模板路径 `sync/workflows/` 前缀与 Render 的 HasPrefix 判断、测试读取路径一致。
