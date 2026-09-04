# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

nxPanel 面向自行管理 Linux Nginx/OpenResty 服务器的管理员。管理员需要在一个面板中完成站点、证书、代理、访问控制、日志、备份、计划任务和系统级扩展管理。

## Product Purpose

nxPanel 是一个采用非 root API 与 root Agent 双进程隔离的 Nginx 管理面板。产品目标是在保持可审计、可回滚和最小特权边界的前提下，让管理员安全地管理网站及在线扩展能力。

## Positioning

所有特权 Nginx 和文件操作都通过 Unix Socket 后的固定 Agent 能力执行；面板业务进程及在线插件不直接获得 root、任意命令或任意文件权限。Host 与 Docker 部署共享相同的配置、事务和安全模型。

## Operating Context

- 支持宿主机直跑以及包含 Nginx/OpenResty、Agent 和 API 的单容器部署。
- 写操作通过数据库状态、操作审计、Agent 文件事务、`nginx -t`、reload 和失败回滚完成。
- 管理界面是 React 19、Mantine 7 和 TanStack Query 构建的中文运维界面。
- “发现”中的在线插件统一由 nxPanel 官方接口提供，并通过发行版内置 TUF 根验证；管理员也可以在高风险开发者模式下安装本地 `.nxp`。
- 商业插件只在用户发起下载且插件服务提出授权挑战后登录；面板支持多个插件服务账户，但账户切换必须显式完成。

## Capabilities and Constraints

- 插件控制逻辑运行在非 root WASM 沙箱中；默认没有文件、网络、环境变量、进程或系统命令权限。
- 插件 UI 使用无同源权限的 sandbox iframe，并只能通过声明式 Bridge 调用获批能力。
- root Agent 只暴露编译期注册的固定 Provider，永不向插件提供通用 shell 或任意文件写入。
- v1 只信任随发行版内置的 nxPanel 官方 TUF 根和官方插件仓库，不开放第三方市场或自定义信任源。
- 开发者模式只允许选择本地 `.nxp`，不接受 URL 或自定义仓库；关闭模式不会停止现有开发者插件，但会禁止新安装、本地更新和重新启用。
- 开发者插件身份不受信任，但仍可在管理员逐项批准后使用全部已注册的受控能力；它们永远不能获得任意 shell、root 文件系统或自带原生模块加载能力。
- 首个正式插件是 ModSecurity v3 + OWASP CRS Web 防火墙；请求检测在 Nginx/OpenResty 数据面执行，WASM 只承担控制面逻辑。
- Host WAF Provider 只安装与运行时完整指纹匹配的构建；未知 ABI 必须阻止，不替换现有 Web 服务。
- WAF 站点首次启用必须明确选择观察或拦截模式，不做静默默认。
- 完整 WAF 审计日志默认保留 7 天或 5 GiB，以先到者为准，并允许管理员配置。
- 插件代码升级由管理员确认；同一选定频道内的签名 CRS 规则可自动更新并在失败时回滚。
- nxPanel 不接触插件服务账户密码，只保存加密、可撤销的 OAuth 设备授权令牌；无权益时不自动尝试其他账户。

## Evidence on Hand

仓库已包含双进程安全边界、Agent 文件事务、Nginx 配置 marker、计划任务中心、SSE、操作审计、GeoIP/访问限制实现和 React 管理界面。这些现有机制是插件平台实现与验收的事实基础。

## Product Principles

- 在线可扩展不能扩大 root 信任边界。
- 每次特权变更必须可验证、可审计、可回滚。
- 不兼容或无法证明兼容时安全失败，而不是猜测加载。
- 插件权限必须显式声明；升级新增权限必须重新确认。
- 运维界面优先展示真实状态、风险和恢复动作。

## Accessibility & Inclusion

新增管理界面须支持键盘操作、清晰焦点、语义化状态、移动端布局和中文错误反馈；不能仅依靠颜色表达安全状态。
