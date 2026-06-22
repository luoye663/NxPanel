// agentclient 包 — API 调用 Agent 的客户端
//
// 通过 Unix Socket HTTP 调用 agent RPC 接口。
// 供 nxpanel-api 进程使用。
//
// 包内文件：
//   - client.go: HTTP 客户端，封装各 RPC 调用
//   - transport.go: Unix Socket 传输层
//   - types.go: 请求/响应类型定义
package agentclient
