// app 包的 ID 生成工具
// 用于生成 request_id、site_id、operation_id 等唯一标识符
package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID 生成带前缀的唯一 ID
// 格式：{prefix}_{timestamp_hex}_{random_hex}
// 例如：site_18a3b2f_xxxxxx
func NewID(prefix string) string {
	ts := time.Now().Unix()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return fmt.Sprintf("%s_%x_%s", prefix, ts, hex.EncodeToString(b))
}

// NewRequestID 生成请求 ID
func NewRequestID() string {
	return NewID("req")
}

// NewSiteID 生成站点 ID
func NewSiteID() string {
	return NewID("site")
}

// NewOperationID 生成操作 ID
func NewOperationID() string {
	return NewID("op")
}

// NewSessionID 生成会话 ID（更长的随机数）
func NewSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func NewCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
