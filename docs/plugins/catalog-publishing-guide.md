# 插件服务端与 TUF 发布教程

官方插件仓库、插件授权服务、用户与权益管理以及发布工具不属于 nxPanel 面板进程。它们位于独立项目 `/root/nxPanle-plugin-server`，正式产品名和 Go module 使用 `nxpanel-plugin-server`。nxPanel 只保留 TUF 客户端、下载授权客户端和插件运行时。

## 服务端职责

`nxpanel-plugin-server` 同时提供：

- 公共 TUF metadata、targets 和 `catalog/v1/index.json`；
- 免费/商业插件的下载解析；
- OAuth 设备授权、访问令牌刷新与撤销；
- 多用户、多插件权益和实例数量控制；
- 插件上传、即时发布、下架与审计管理端；
- `pluginctl` TUF 密钥仪式、发布和验证工具。

第一版使用 SQLite 和本地文件，只支持单服务实例。正式环境应把数据库、TUF metadata、不可变 targets 和在线密钥密文一起备份；master key、离线 Root 私钥不得进入备份包、Git 或容器镜像。

## 本地启动

```bash
cd /root/nxPanle-plugin-server
make dev-init
make dev
```

`make dev-init` 生成只供本机使用的 `.dev` master key、测试在线密钥和 TUF Root；SQLite 数据库会在首次执行 `make dev` 或 `make admin-create` 时创建。该命令可重复执行，不会覆盖已有密钥或仓库。`make dev` 默认监听 `127.0.0.1:18900`。完整 nxPanel 联调命令见 [README](README.md#本地源码联调)。正式服务必须拒绝加载 `.dev` Root。

## 发布插件

在管理端上传 `.nxp` 前，维护者应核对展示信息、面板兼容范围、权限、Provider 和访问级别。当前 v1 在确认上传后立即校验并发布，不设草稿审批阶段；服务端会检查归档预算、路径、manifest、文件摘要和 WASM：

- `free`：下载解析不要求账户；
- `licensed`：只有具备有效插件权益且未超过实例数的账户可下载。

发布时生成新的 Targets、Snapshot 和 Timestamp 版本。不能用相同版本覆盖不同内容；下架只从后续目录中移除插件，不删除仍可能被在线客户端引用的 target。

CLI 的具体参数以独立项目 `pluginctl --help` 为准，职责包括：

```text
pluginctl tuf dev-init
pluginctl tuf root-init
pluginctl tuf online-key generate
pluginctl tuf root-sign
pluginctl tuf rotate-online
pluginctl tuf rotate-root
pluginctl repository publish
pluginctl repository verify
```

Root 采用离线 2-of-3 Ed25519 阈值；Targets、Snapshot、Timestamp 使用相互独立的在线密钥。默认过期时间为 Root 365 天、Targets 30 天、Snapshot 7 天、Timestamp 24 小时。Root 轮换必须生成连续版本，并同时满足旧 Root 和新 Root 的签名阈值。

## 正式构建配置

GitHub 正式发布前，在仓库的 Actions variables 中配置以下四个公开构建参数；二进制发布包和 nginx、OpenResty 镜像使用同一组值：

- `OFFICIAL_PLUGIN_METADATA_URL`：TUF metadata 基础地址；
- `OFFICIAL_PLUGIN_TARGETS_URL`：TUF targets 基础地址；
- `OFFICIAL_PLUGIN_SERVICE_URL`：插件服务基础地址；
- `OFFICIAL_PLUGIN_ROOT_B64`：可信 TUF bootstrap Root JSON 的单行 Base64。

Release 工作流和 `make release` 都以正式构建模式运行，缺少任一参数会在构建前失败。本地普通二进制或 Docker 构建允许不配置官方仓库，此时官方目录不可用；需要验证正式构建门禁时可显式传入上述参数并设置 `RELEASE_BUILD=1`。

## 下载和授权协议

nxPanel 点击安装后调用 `POST /api/v1/downloads/resolve`，携带插件、版本、TUF target、随机 `instance_uuid`、面板版本和运行时摘要，不上传站点或域名。

- 免费插件直接返回下载地址；
- 商业插件无访问令牌时返回 `authorization_required`；
- 令牌无效或过期时要求重新授权；
- 没有权益、权益过期、账户停用或实例已满时返回稳定拒绝原因；
- 校验通过后返回绑定账户、插件、版本、target 和实例的短时下载 grant。

商业插件登录使用 OAuth 设备授权。nxPanel 只展示验证码和插件服务登录地址，用户密码仅提交到插件服务。插件下载完成后，nxPanel 仍必须按可信 TUF Targets 验证长度和 SHA-256；下载 grant 只决定资格，不能替代信任链。

## 正式运维检查

- 离线 Root 私钥分开保管，并有异地离线备份；
- 在线角色密钥密文可恢复，环境 master key 单独托管；
- 每次发布前执行全仓库验证并测试已接受旧 Root 的客户端；
- 演练在线密钥泄露后的 Root 更新、撤销与重新发布；
- 恢复数据库和仓库后确认 metadata 版本没有回滚；
- 定期检查即将过期的 Timestamp、Snapshot、Targets 和 Root；
- 审计插件访问级别、权益、实例撤销、设备撤销、密钥轮换和下载拒绝。
