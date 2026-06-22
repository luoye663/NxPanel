// nginx 包 — 配置文件 hash 工具
//
// 用于计算 Nginx 配置文件的 SHA256 hash，
// 支持漂移检测（drift detection）和乐观并发控制（expected_file_hash）。
package nginx

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashContent 计算内容的 SHA256 hash，返回十六进制字符串
// 用于实时文件漂移检测（drift detection）
func HashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}
