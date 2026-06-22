package app

import "testing"

func TestGenerateLoginPathValid(t *testing.T) {
	path := GenerateLoginPath()
	if err := ValidateLoginPath(path); err != nil {
		t.Fatalf("生成的登录路径应合法: %v, path=%q", err, path)
	}
	if len(path) != 17 {
		t.Fatalf("生成的登录路径长度应为 17，got=%d path=%q", len(path), path)
	}
	if path[:4] == "/nx-" {
		t.Fatalf("生成的登录路径不应包含固定 nx- 前缀: %q", path)
	}
}

func TestValidateLoginPath(t *testing.T) {
	valid := []string{"/nx-abc12345", "/Panel_123456"}
	for _, path := range valid {
		if err := ValidateLoginPath(path); err != nil {
			t.Fatalf("路径应合法 %s: %v", path, err)
		}
	}
	invalid := []string{"", "login", "/login", "/setup", "/api", "/assets", "/health", "/auth", "/short", "/has/slash", "/bad.path", "/bad\npath"}
	for _, path := range invalid {
		if err := ValidateLoginPath(path); err == nil {
			t.Fatalf("路径应非法: %q", path)
		}
	}
}
