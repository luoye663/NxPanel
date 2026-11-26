# 架构与进程边界

## API、Agent 与部署形态

- `nxpanel-api` 以非 root 身份提供 Web API 和 React SPA，负责浏览器认证、SQLite 业务数据及操作审计。SQLite 文件位于 `data_dir`；数据库迁移由 API 启动流程执行。
- `nxpanel-agent` 以 root 身份执行 Nginx 命令、受控配置回写及文件、日志、证书操作。Agent 只在 Unix Socket 上提供 HTTP；Socket 权限为 `0660`，通过 `agent.socket_group` 授权 API 进程，RPC 仍须校验共享 Token。Agent 不处理浏览器 Session 或 CSRF。
- 宿主机由 systemd 管理；Docker 独享模式由 s6 管理同容器内的 API、Agent 和 Nginx/OpenResty。两种形态遵循同一权限边界，不能假设系统用户、二进制、主配置文件或 PID 路径相同。`nxpanel-updater` 是独立的更新组件，不参与日常 API → Agent 请求链路。部署细节见[部署与发布](deployment.md)。

## 请求与写入链路

浏览器请求先经过请求上下文和安全中间件，业务路由再受隐藏入口与身份校验保护。已认证写请求还须通过 CSRF。API handler 解析请求、调用 service，service 调用 repo 持久化业务状态；需要特权操作时经 `internal/agentclient` 调用 Agent，传递请求或任务 context。API 与 Agent 路由分别在 [`internal/api/routes.go`](../internal/api/routes.go) 和 [`internal/agent/routes.go`](../internal/agent/routes.go) 装配，二者使用各自的响应协议。

API 可写自身的 SQLite、运行日志及面板 TLS 资产。运行期面板 YAML 设置由 Agent 的白名单回写 RPC 持久化；Nginx 变更走独立文件事务；纯数据库写入不调用 Nginx。事务和应用状态见 [Nginx 配置与事务](nginx-config.md)，认证与路径边界见[安全与资源约束](security.md)。

## 装配与生命周期

进程入口位于 [`cmd/nxpanel-api`](../cmd/nxpanel-api/) 和 [`cmd/nxpanel-agent`](../cmd/nxpanel-agent/)。API 在 [`internal/api/dependencies.go`](../internal/api/dependencies.go) 分组装配 repo、Agent 客户端、业务 service 和任务处理器。依赖注册、启动迁移和遗留任务恢复先于调度启动，详见[统一计划任务](scheduled-tasks.md)。

API `Server` 持有根 context。与请求同寿命的读写使用请求 context；需要在响应结束后继续的业务，以根 context 为父级，使用有界队列或并发准入，收尾必须释放资源。幂等 `Close` 取消根 context、关闭 SSE，停止调度和后台组件，等待受管理的 goroutine 退出，再关闭 Agent transport。登录审计与计划任务历史由同一个维护循环启动时及定期清理；保留策略经过硬上下界钳制。

在线更新试运行走专用启动路径：只读加载 YAML，先准备运行依赖和调度状态，确认试运行后再启动正常调度与维护流程。更新组件的信任与回滚契约见[面板在线更新](function/web-update.md)。

## 前端与监听配置

- `webapp-react/` 是唯一生产前端源目录。Vite 生成 `web/dist`；[`web/`](../web/) 的 `StaticFS` 在普通构建中查找磁盘文件，在 `embed_web` 构建中读取嵌入产物。不要将其中一种加载方式当作所有部署的前提。
- Go 编译默认监听 `127.0.0.1:8888`；Vite 开发代理也使用该端口，但代理目标是 HTTP，联调时须与 API TLS 设置一致。宿主机示例和 Docker 容器面板使用 `18888`；实际监听由配置和环境变量决定，Docker 宿主机映射端口可另设。默认启用 TLS，不提供明文 HTTP 自动跳转。
