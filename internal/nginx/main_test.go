package nginx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// 初始化从项目 configs/templates 加载真实模板
	InitTemplates(filepath.Join("..", "..", "configs", "templates"))
	code := m.Run()
	os.Exit(code)
}
