// agent 包 — root 权限代理服务
//
// agent 以 root 权限运行，监听 Unix Socket，为 nxpanel-api 提供特权操作。
//
// 安全设计：
//   - 仅通过 Unix Socket 通信，不暴露公网端口
//   - 所有请求必须携带 agent token
//   - 所有文件路径必须在 allowlist 内
//   - 不提供任意命令执行能力
//   - 文件事务支持备份、原子写入和回滚
//
// 包内文件：
//   - server.go: HTTP 服务器，中间件注册
//   - routes.go: 路由注册
//   - auth.go: Token 认证中间件
//   - handlers.go: RPC handler 实现
//   - transaction.go: 文件事务（备份、原子写入、回滚）
//   - atomic.go: 原子文件写入
//   - path_policy.go: 路径安全策略
package agent
