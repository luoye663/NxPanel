# 安全与资源约束

## 入口、身份与防重放

- 隐藏入口由 `crypto/rand` 生成；自定义 `api.login_path` 是以 `/` 开头、长度受限的单段路径，仅允许字母、数字、`-`、`_`，不得使用 `/login`、`/setup`、`/api`、`/assets`、`/health`、`/auth` 等保留路径。`/api/v1/{gateSecret}` 的门控比较使用 `crypto/subtle.ConstantTimeCompare`，未命中返回 404。只在隐藏入口或有效 Session 的 SPA 回退中注入 gate；公开静态资源不携带 gate，`/health` 须显式开启。前端须用 gate 工具构造 API、SSE 和下载 URL。
- 业务路由保留 `RequireAuth`。CSRF 在身份装配后校验，已认证写请求从 `X-CSRF-Token` 读取并恒定时间比较哈希。无 Session 的初始化、登录及登录阶段 2FA 是引导流程，仍受 gate 和各自的限流或临时令牌约束，不能照搬为新写路由的豁免。保留安全响应头和 Cookie 的 `HttpOnly`、`SameSite`、HTTPS 下 `Secure` 语义。
- Session 的 IP 与 User-Agent 绑定默认开启，可分别由 `api.bind_session_ip`、`api.bind_session_ua` 控制。2FA 登录临时令牌始终绑定二者，与 Session 开关无关。创建与校验共用 UTF-8 安全归一化：IP 有 64 字节上限，超过 512 字节的 User-Agent 使用摘要。启用的绑定不匹配时应使凭证失效。管理员改密后必须使既有 Session 失效。
- 2FA 密码阶段不创建 Session。临时令牌有有效期、五次失败上限和每账号／全局容量上限，准入前清理过期项，成功消费一次后失效。TOTP 通过 `ConsumeTOTPCode` 以数据库 CAS 防止 90 秒内重放；`VerifyAndConsumeCode` 区分重放与无效码。恢复码以 `ConsumeRecoveryCode` CAS 原子消费，只接受一行被更新。启用 2FA 前的待确认密钥在服务端暂存并过期清理。
- Agent 只接受 `X-NxPanel-Agent-Token`，使用恒定时间比较；空 Token 禁止启动。所有加密随机读取须检查错误；不能用可预测值替代认证凭证。

## 公网入口与登录预算

- 只从 `api.trusted_proxies` 中的直接对端信任转发头。`X-Forwarded-For` 从右向左解析，跳过非法条目和可信代理，取第一个非可信 IP；全可信时用最左合法 IP，没有有效转发链时才在可信对端使用合法的 `X-Real-IP`。有效信任列表为空时使用直接对端。`X-Forwarded-Proto` 同样只信任可信对端，用于 HTTPS 判断和 HSTS。
- 分别保留 TCP listener 的全局／每对端活跃连接上限与提取真实 IP 后的 HTTP 有界令牌桶。登录保护在昂贵的 bcrypt 或 CAPTCHA 前限制输入，按 IP、规范化账号和进程全局累计失败；只在成功创建 Session 时清理相应 IP／账号预算，全局预算只按窗口过期。初始化和已认证敏感操作继续使用独立限流器。限流状态受锁保护，跟踪集合有界，清理随 context 停止；审计字段及保留量也须有界。
- CAPTCHA 达到配置的失败阈值时触发；公开配置只在当前请求需要验证时提供 provider 和站点 key。验证应非阻塞并发准入，对无效配置、非 2xx、畸形响应及上游错误 fail closed，不向浏览器披露 secret。
- 普通 POST/PUT/PATCH 请求保留 2 MiB 全局请求体上限，站点与全局文件上传走专用限制。客户端 `X-Request-ID` 只接受至多 128 字节、字符集 `[a-zA-Z0-9._-]` 的值，否则生成服务端 ID。不能为长请求统一抬高全局限额或读取超时。

## 文件、上传与归档

- Agent 必须用 [`PathPolicy`](../internal/agent/path_policy.go) 校验读写目标和符号链接目标，SSL 文件检查同样适用。白名单合并配置中的面板／数据／主配置父目录／日志目录、站点根和日志前缀、最小通用网站根目录以及 `agent.allowed_roots`；[`CleanAbsWithin`](../internal/security/path.go) 按目录边界匹配，新文件或多级目录须解析最近存在父目录的真实路径。保留对更新与模块状态目录的额外隔离，包括指向这些目录的别名和覆盖它们的祖先路径。
- 站点根目录、日志路径还要通过相应的 Nginx 前缀校验。读取操作被白名单拒绝时保留 `AGENT_DENIED` 和 `allowed_config` 指引的错误契约。导入扫描的字符串预检只生成 `warnings`、`*_ok` 等提示，不代替 Agent 的真实路径授权；导入详情持续展示 `ImportWarnings`，仅从唯一定位的 server 同步日志路径。
- 站点和全局上传在认证后获得专用读取 deadline/context，API 和 Agent 各自限量。Multipart 必须恰有一个 `path` 和一个 `file`，拒绝未知或重复 part；JSON/base64 先限制编码体，再限制解码后的精确字节数。API 完整验证后才流式转发，Agent 验证后才原子 rename 发布目标；错误和取消须清理临时文件并保持旧目标不变。
- ZIP、TAR、TAR.GZ 与站点备份／恢复共享归档预算：条目数、深度、输入、产物、实际解压字节、单条目大小、压缩比、超时均有上限。预检在写入前完成，实际复制仍计量；每个条目须满足解压目标边界和全局 `PathPolicy`，拒绝绝对路径、`..`、反斜杠及 symlink/hardlink 等非允许类型。保留源与目标重叠检查、有界复制、失败清理和文件描述符及时释放。
- 站点备份 API 用站点及备份 ID 查记录，不接收任意归档路径；Agent 校验归档元数据、站点归属与目标白名单。恢复前做当前状态快照，配置或证书恢复后校验 Nginx，失败尝试回滚并区分回滚失败。含证书私钥或认证依赖的备份按敏感文件处理。

## 长请求与数据预算

- Nginx 命令的 stdout/stderr 共享输出预算，超限终止进程组，仅保留有界诊断。日志读取限制单行、文件／样本或输出大小；历史切割日志只允许由当前站点日志推导的文件名。文件下载使用有界流和请求取消。
- SSE 共享进程级连接上限；事件、历史和心跳串行写，每帧使用滚动写 deadline，取消、超时或写失败即释放订阅和槽位。Hub 原子取得历史、实时订阅和关闭状态，历史有界，终态按 TTL 保留，由单个清理循环回收。系统指标共享一个采集循环，无订阅者时停止；前端换 scope 或卸载时关闭旧连接。
- 访问分析只扫描 API 从站点记录取得并经 Agent 白名单校验的日志路径。当前日志和切割文件共享字节、行数和时间预算，同时限制文件数、单行长度，不返回完整原日志。路径、IP、小时、异常和可选明细各有上限，聚合维度另受共享 distinct 上限；截断须有可解释的计数。Agent 客户端限制响应 envelope 大小，API 验证基数和元数据后才允许写库。
- 保留单站点扫描互斥、启动及准入前遗留 job 恢复、带 context 的分页查询和 CSV 导出。聚合、明细、job 终态和 cursor 在同一事务提交，cursor 最后写；失败或取消不提前推进游标。

## 敏感数据与配置输入

- 管理员密码用 bcrypt 存储，不在响应中返回密码或哈希。站点访问账户的 Nginx 认证文件属于敏感派生产物，不与面板管理员凭证混用。Token、CAPTCHA secret、账户私钥和密码不得进入普通 DTO、日志、审计或提交内容。
- 证书私钥只在既有的认证编辑和显式导出功能中提供：站点 SSL 内容返回证书和私钥 PEM，站点 SSL 与 ACME 订单下载导出的是**证书**私钥，含证书的备份也可能携带私钥。ACME **账户**私钥不得由业务 API 返回；通用管理员文件访问仍取决于 Agent 的路径与大小检查，数据库及备份本身也是敏感资产。
- 业务 YAML 回写由 Agent 整批验证字段白名单和值后执行，不能借普通文件写入绕过。所有结构化 Nginx 输入须按字段语义验证，拦截可改变指令或块结构的字符；上游、站点路径和 nginx.conf 参数的具体约束见 [Nginx 配置与事务](nginx-config.md)。签名信任和 updater／模块安装的权限边界见[部署与发布](deployment.md)。
