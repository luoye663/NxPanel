# Nginx 配置与事务

## 托管站点的 Marker

Marker 是成对的 `#NXPANEL-<NAME>-START/END` 注释，`SITE` 的起始行携带 `site_id`。名称是持久化配置契约，应复用 [`internal/nginx/markers_constants.go`](../internal/nginx/markers_constants.go) 中的常量。`RequiredSiteMarkers()` 定义当前基线：`SITE`、`LISTEN`、`SERVER-NAME`、`ROOT`、`LOG`、`REWRITE`、`DOCUMENT`。站点记录保留 Marker 版本以支持迁移。

已有托管站点的功能编辑先读取当前文件，用 `ValidateRequiredMarkers` 检查所依赖的基础块，再用 `ReplaceMarkerBlock`／`ApplyMarkerPatches` 修改所属块。缺失或重复的必需块应报错，不能通过整站重渲染修复。可选块用 `EnsureMarkerBlock`／`ApplyOptionalMarkerPatches` 按需注入；已有块应保留位置，创建及迁移不预生成未使用的空块。

注入顺序对应 [`internal/nginx/marker.go`](../internal/nginx/marker.go) 的锚点：`LISTEN → SSL → FORCE-HTTPS → SERVER-NAME → ROOT → REWRITE → HOTLINK → ACCESS-LIMIT → DOCUMENT → ACME-CHALLENGE → MAIN-LOCATION → EXTRA-LOCATIONS → LOG`。`FORCE-HTTPS`、`EXTRA-LOCATIONS` 依赖其前置功能块；`ACCESS-LIMIT` 无 `HOTLINK` 时锚定 `REWRITE`，`MAIN-LOCATION` 无 ACME 块时锚定 `DOCUMENT`。插入不重排存量配置。

| 写入者 | 管理范围 |
|---|---|
| 站点基础设置、文档增强 | 基础设置改 `SERVER-NAME`、`LISTEN`、`ROOT`、`LOG`；文档增强改 `ROOT` 和 `DOCUMENT`（`autoindex`、`error_page`）。 |
| SSL、默认站点 | SSL 改 `LISTEN`、`SSL`、`FORCE-HTTPS`；默认站点只改 `LISTEN` 的 `default_server` 语义。 |
| 反向代理 | `MAIN-LOCATION` 管 `/`，`EXTRA-LOCATIONS` 管其他路径；反代还维护自己的认证文件、缓存配置和缓存目录。 |
| Rewrite、旧访问限制与防盗链 | Rewrite 改自己的 include 文件，不改站点 Marker；旧访问限制和地域规则使用 `ACCESS-LIMIT`，旧防盗链使用 `HOTLINK`，各维护所属 include。 |
| 统一访问策略 | 接管后使用 `ACCESS-LIMIT` 挂载策略，并清空 `HOTLINK` 管理块。旧写入口不得覆写待确认或已生效的统一策略。反代与其关联认证规则一起应用或同步，见[统一访问规则编排](access-policy.md)。 |
| ACME、站点维护 | HTTP-01 按需注入 `ACME-CHALLENGE`，可调整面板管理的 `FORCE-HTTPS`，不得改写用户重定向。维护功能使用自己的 `MAINTENANCE` 块及专用解析器和前置检查。 |

`nginx.Render()` 仅在首次创建站点时生成完整配置。模板由 `InitBuiltinTemplates` 加载：显式配置的外部目录优先，其次是更新根目录的 `templates-local`（存在时），否则使用嵌入的 [`configs/templates`](../configs/templates/)；所选模板集缺失或解析失败应报错。新增模板要同步 loader 和渲染调用，并测试产物。

完整配置编辑保存用户提交的全文，保留适用的 Marker／站点身份校验、漂移检测和元数据同步。显式 Marker 迁移是旧站转换入口，不代替功能更新。导入站点可能不含 Marker，已有功能通过专用解析器唯一定位目标 server 或 include；不得用文件第一个 server 猜测。日志路径在导入详情按需同步，保存全文时也重新提取。禁止通过功能更新覆盖用户自定义内容或其他功能的块。

## 文件事务、回写与执行器

- Nginx 配置变更在 service 创建 Operation 并关联请求，再通过 Agent 文件事务应用。Agent 串行化事务，先校验路径、操作及适用的哈希前置条件，再备份和原子修改。已有 `ExpectedHash`、只读 `check` 与明确允许的 symlink 目标解析不能退化为 API 侧检查，避免检查后到写入前发生漂移。
- 文件变更执行 `nginx -t`，按站点或系统变更语义 reload。确定的失败进入回滚，恢复原始文件权限并移除事务新建文件；回滚失败或无法确认时不得报告已恢复。保留备份路径、操作终态和有界诊断；普通事务备份在数量和年龄都超过条件时清理，需留存的迁移备份与清单使用独立目录。站点备份恢复另见[安全与资源约束](security.md)。
- 使用 Agent 检测或配置的 Nginx/OpenResty 执行路径，要求绝对路径并拒绝特殊字符；浏览器检测请求不接受自选二进制。test、dump、reload、reopen 使用同一运行时，在配置了 `conf_path` 时传 `-c`。仅已有的 PID／未运行类 reload 失败允许受限启动回退；显式生命周期控制按运行时身份匹配 systemd/s6，不能用通用回退吞掉配置失败。
- `ConfirmReload` 和维护切换等需要 worker 代际确认的流程，保留修改前的代际探测及 reload 后的确认。信号已发送但切换未确认时保留配置，进入显式对账，不当作应用成功或确定回滚。要求确认的事务不能用启动回退替代确认。
- `/internal/v1/config/write-back` 写入面板 YAML，不是 Nginx 文件事务，不触发 `nginx -t` 或重启调度器。Agent 先验证整批字段和值，调用方按需热重载安全设置。Nginx 检测同步有效的 web 用户；保存 nginx.conf 或常用参数成功后，从 `user` 指令通过回写 RPC 同步非空、非 root 的 `nginx.web_user`／`nginx.web_group`，供站点文件归属使用。
- 结构化字段按语义校验，包括 `index_files`、路径、后缀模式、IP/CIDR。不得允许分号、花括号、引号、换行等突破现有指令边界；主配置参数插入保留换行清洗。用户显式提交的完整配置有独立校验链路，不能降低结构化字段校验。

## Desired 与 Applied 对账

- Upstream 以数据库为 desired source，确定性渲染 `conf.d/nxpanel-upstreams.conf`，Agent 成功后记录 applied hash/path；`status` 用于区分待应用或文件漂移，`sync` 重放当前 desired。名称创建后不可变，不能删除仍被引用的 upstream。
- Proxy 先保存 desired，按当前站点反代集合重建自己的 Marker 和依赖文件。读接口返回 desired，不代表文件已应用；站点 `sync` 用当前 desired 修复。保留写串行和审计，区分持久化失败、Agent 未确认和应用后审计失败。超时、断连等结果不确定时保留 desired，返回 `sync_required`，由现有同步入口对账。
- 托管反代只能按 upstream ID 引用，由服务端生成 `proxy_pass`。结构化校验 upstream 成员、算法及 keepalive，高级指令仅允许 upstream 块内有限语句，不允许花括号或 `include`，最终经 `nginx -t`。HTTPS 上游 SNI 只接受纯 hostname/IP；启用验证时 CA 须是 Agent 白名单内的安全绝对路径，不得位于网站根目录。
