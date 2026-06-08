// Package scaffold 实现 init 子命令：渲染 spec/ 目录脚手架。
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:templates
var templatesFS embed.FS

// Params 是模板渲染变量。
type Params struct {
	PluginName string // 插件名（如 gitlab）
	SpecRepo   string // spec 所在主仓库 owner/repo
	ToolImage  string // driftcheck 工具镜像
}

// SpecRepoURL 返回主仓库的 https 地址（不含 .git 后缀）。
func (p Params) SpecRepoURL() string {
	return "https://github.com/" + p.SpecRepo
}

// Render 把全部模板渲染到 outDir（如 spec/）。目标文件已存在时报错，绝不覆盖。
func Render(outDir string, p Params) ([]string, error) {
	var written []string
	err := fs.WalkDir(templatesFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, "templates/")
		dst := filepath.Join(outDir, strings.TrimSuffix(rel, ".tmpl"))
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s 已存在，拒绝覆盖", dst)
		}
		raw, err := templatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		t := template.New(rel)
		if strings.HasPrefix(rel, "sync/workflows/") {
			// GHA workflow 含 ${{ }}，与默认定界符冲突，改用 [[ ]]。
			t = t.Delims("[[", "]]")
		}
		tmpl, err := t.Parse(string(raw))
		if err != nil {
			return fmt.Errorf("解析模板 %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := tmpl.Execute(f, p); err != nil {
			return fmt.Errorf("渲染 %s: %w", rel, err)
		}
		written = append(written, dst)
		return nil
	})
	return written, err
}
