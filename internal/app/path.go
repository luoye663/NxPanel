package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetExecutableDir 获取可执行文件所在目录的绝对路径
func GetExecutableDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("解析符号链接失败: %w", err)
	}

	return filepath.Dir(realPath), nil
}

// FindWebDir 查找前端静态文件目录
// 默认在可执行文件父目录的 web/ 子目录
func FindWebDir() (string, error) {
	exeDir, err := GetExecutableDir()
	if err != nil {
		return "", err
	}

	parentDir := filepath.Dir(exeDir)

	webDir := filepath.Join(parentDir, "web")
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
		return webDir, nil
	}

	webDistDir := filepath.Join(parentDir, "web", "dist")
	if _, err := os.Stat(filepath.Join(webDistDir, "index.html")); err == nil {
		return webDistDir, nil
	}

	return "", fmt.Errorf("前端目录不存在: %s/index.html 或 %s/index.html", webDir, webDistDir)
}

// FindConfigFile 查找配置文件
// 默认查找：可执行文件父目录/config/config.yaml
// 支持命令行参数覆盖
func FindConfigFile(configName string) (string, error) {
	if filepath.IsAbs(configName) {
		if _, err := os.Stat(configName); err != nil {
			return "", fmt.Errorf("配置文件不存在: %s", configName)
		}
		return configName, nil
	}

	if _, err := os.Stat(configName); err == nil {
		abs, _ := filepath.Abs(configName)
		return abs, nil
	}

	exeDir, err := GetExecutableDir()
	if err != nil {
		return "", fmt.Errorf("无法确定可执行文件目录: %w", err)
	}

	configPath := filepath.Join(filepath.Dir(exeDir), "config", filepath.Base(configName))

	if _, err := os.Stat(configPath); err != nil {
		return "", fmt.Errorf("配置文件不存在: %s (已查找: 当前目录/%s, %s)", configName, configName, configPath)
	}

	return configPath, nil
}

func FindTemplatesDir(configured string) string {
	if configured != "" {
		if filepath.IsAbs(configured) {
			if _, err := os.Stat(configured); err == nil {
				return configured
			}
		}

		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(configured)
			return abs
		}
	}

	exeDir, err := GetExecutableDir()
	if err != nil {
		return configured
	}

	tplDir := filepath.Join(filepath.Dir(exeDir), "config", "templates")
	if info, err := os.Stat(tplDir); err == nil && info.IsDir() {
		return tplDir
	}

	fallback := filepath.Join(filepath.Dir(exeDir), "configs", "templates")
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return fallback
	}

	return configured
}