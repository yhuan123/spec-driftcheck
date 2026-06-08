package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yhuan123/spec-driftcheck/internal/runner"
)

var testParams = Params{
	PluginName: "demo-plugin",
	SpecRepo:   "acme/demo-plugin",
	ToolImage:  "ghcr.io/yhuan123/spec-driftcheck:latest",
}

// TestRender_InitIsGreen 断言：脚手架生成后直接通过 driftcheck check（零 findings）。
func TestRender_InitIsGreen(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "spec")
	written, err := Render(specDir, testParams)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) == 0 {
		t.Fatal("应至少渲染一个文件")
	}
	findings, err := runner.Run(specDir, filepath.Join(root, "work"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("脚手架应零 findings, got %+v", findings)
	}
}

// TestRender_SubstitutesParams 断言：模板变量被替换且无残留占位符。
func TestRender_SubstitutesParams(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "spec")
	written, err := Render(specDir, testParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range written {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "{{.") || strings.Contains(string(data), "[[.") {
			t.Errorf("%s 含未渲染的模板占位符", f)
		}
	}
	task, err := os.ReadFile(filepath.Join(specDir, "sync/tasks/spec-drift-notice.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(task), "https://github.com/acme/demo-plugin.git") {
		t.Errorf("Task 应包含渲染后的 spec-repo-url: %s", task)
	}
}

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

// TestRender_RefusesOverwrite 断言：目标文件已存在时报错，不覆盖。
func TestRender_RefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "spec")
	if _, err := Render(specDir, testParams); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(specDir, testParams); err == nil {
		t.Fatal("重复渲染应报错拒绝覆盖")
	}
}

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
