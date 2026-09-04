# nxPanel 插件文档

本目录记录 nxPanel 插件系统 v1 的使用、开发和发布流程。

## 按角色阅读

- 面板管理员：先阅读 [插件系统使用教程](administrator-guide.md)。
- WAF 管理员：继续阅读 [Web 防火墙使用教程](waf-guide.md)。
- 插件开发者：阅读 [插件开发教程](developer-guide.md)。
- 前后端联调：查阅 [Plugin API v1 参考](api-reference.md)。
- 官方仓库维护者：阅读 [插件服务端与 TUF 发布教程](catalog-publishing-guide.md)。

## 当前版本能力边界

插件系统 v1 包含官方 TUF 仓库、`.nxp` 包校验、开发者模式、WASM 生命周期沙箱、权限确认、隔离 iframe、UI Bridge、动态菜单和站点详情贡献点。

官方插件中心只读取随发行版内置的官方接口和 TUF 信任根。管理员不能添加第三方市场；自行开发的插件只能在高风险开发者模式下从本地 `.nxp` 安装。

官方和开发者插件使用相同的权限 broker。无论来源，插件只能调用已注册、获批且受配额约束的能力；原生扩展只允许编译进 Agent 的固定 Provider。

商业插件不要求管理员预先填写 License Key。用户点击安装后，插件服务如返回“需要授权”，面板才展示设备验证码和官方登录地址；登录成功后自动继续原下载。一个面板可以保存多个插件服务账户，但切换账户必须由用户明确选择。

## 本地源码联调

插件服务端是独立项目，固定放在 `/root/nxPanle-plugin-server`。首次联调按以下顺序启动：

```bash
# 终端 1：初始化开发密钥与 TUF 仓库（可重复执行），再启动插件服务
cd /root/nxPanle-plugin-server
make dev-init
# 仅首次创建管理账户；请换成你自己的长密码和邮箱
PLUGIN_ADMIN_PASSWORD='change-this-development-password' \
  make admin-create EMAIL=admin@example.com NAME=Administrator
make dev

# 终端 2：把本地插件服务地址和开发 Root 注入 nxPanel API
cd /root/nxPanel
make run-api-plugin-dev \
  PLUGIN_SERVER_URL=http://127.0.0.1:18900 \
  PLUGIN_ROOT_FILE=/root/nxPanle-plugin-server/.dev/repository/metadata/1.root.json

# 终端 3：启动特权 Agent
cd /root/nxPanel
make run-agent

# 终端 4：启动 React 前端
cd /root/nxPanel
make dev-frontend
```

`run-api-plugin-dev` 会从 `PLUGIN_SERVER_URL` 推导 metadata、targets 和授权 API 地址，并读取 `PLUGIN_ROOT_FILE` 注入 bootstrap Root。开发调试只对 loopback 地址允许 HTTP；正式构建仍必须使用 HTTPS，且缺少任一官方插件服务参数时应失败。

启动后先在插件服务管理端创建测试账户和插件：免费插件应直接安装；商业插件应在点击安装后出现设备授权挑战，完成登录与批准后自动继续安装。开发 Root 只能用于本地调试，不能复制到正式发行构建。

## 相关实现

- 插件核心与 TUF 客户端：`internal/plugin/`
- 插件 API：`internal/api/plugin_handler.go`
- WAF 领域模型：`internal/waf/`
- WAF 控制面：`internal/wafcontrol/`
- WAF Agent Provider：`internal/agent/waf_*.go`
- 插件前端：`webapp-react/src/components/plugins/`、`webapp-react/src/pages/PluginCenterPage.tsx`
- 官方插件服务、管理后台与发布工具：`/root/nxPanle-plugin-server`
