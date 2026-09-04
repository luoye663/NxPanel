# Plugin API v1 参考

本页描述当前前端与 API 的实际接口。所有路径都位于隐藏入口下：

```text
/api/v1/{gateSecret}
```

接口需要已登录 session；`POST` 和 `DELETE` 还需要有效的 `X-CSRF-Token`。插件 iframe 不应直接调用这些接口，应通过宿主提供的 `MessagePort` Bridge 请求获准的 RPC。

## 统一响应

```json
{
  "request_id": "req_xxx",
  "success": true,
  "data": {},
  "error": null
}
```

失败时 `success` 为 `false`，`data` 为 `null`，`error` 包含 `code` 和 `message`。

## 目录和生命周期

### GET `/plugins/catalog`

返回签名目录中的插件，同时合并本机安装状态：

```json
{
  "items": [
    {
      "id": "org.example.hello",
      "name": "Hello Plugin",
      "summary": "示例插件",
      "publisher": "Example",
      "version": "1.0.0",
      "compatible": true,
      "permissions": [],
      "required_providers": [],
      "installed_version": "1.0.0",
      "state": "enabled",
      "health": "ready"
    }
  ]
}
```

### POST `/plugins/catalog/refresh`

从内置官方地址刷新并验证完整 TUF metadata 和目录 target。

### GET `/plugins/repository/status`

返回官方仓库是否已配置、是否可用、是否正在使用可信缓存，以及 metadata 过期和最近刷新状态。

### GET `/plugins`

返回已安装插件。`permissions` 是给界面展示的权限描述，`approved_permissions` 是实际批准的权限名数组。每项还包含 `source`（`official | developer`）、`verification_status`（`tuf_verified | developer_unverified`），以及可选的 `source_conflict`。

### GET `/plugins/{plugin_id}`

返回一个已安装插件；不存在时返回 404。

### POST `/plugins/{plugin_id}/install`

### POST `/plugins/{plugin_id}/update`

两个接口当前使用相同安装事务，请求体：

```json
{
  "version": "1.0.0",
  "approved_permissions": []
}
```

为兼容早期前端，请求也接受 `permissions`，但新调用方应使用 `approved_permissions`。

### POST `/plugins/{plugin_id}/enable`

实例化 WASM、运行健康检查并启用贡献点。WAF 插件还会触发固定原生 Provider 的指纹检查、安装和激活。

### POST `/plugins/{plugin_id}/disable`

关闭 WASM 实例。存在已启用 WAF 站点时，停用 WAF 插件会被拒绝。

### DELETE `/plugins/{plugin_id}`

只能卸载已经停用的插件。当前前端可能带 `purge_data` 查询参数，但后端 v1 不读取该参数；插件 KV 和事件数据默认保留。

## 开发者模式

### GET `/plugins/developer-mode`

返回全局开发者模式状态和正在运行的开发者插件数量。

### PUT `/plugins/developer-mode`

```json
{"enabled":true,"acknowledge_risk":true}
```

开启时必须明确确认风险。关闭不会停止正在运行的开发者插件。

### POST `/plugins/developer/packages/inspect`

上传 `multipart/form-data`，字段名为 `file` 且只能包含一个本地 `.nxp`。成功返回一次性 `upload_token`、有效期、文件名、包 SHA-256、`manifest`、`unverified` 和 `verification_status`。`manifest` 包含 ID、名称、声明发布者、版本、权限、网络域名和 Provider。

### POST `/plugins/developer/packages/install`

```json
{"upload_token":"opaque-token","approved_permissions":["plugin.kv"]}
```

服务端会重新校验暂存包和完整权限集合。token 15 分钟过期、只能使用一次，服务重启后失效。

## 商业插件授权

免费插件安装不调用授权接口。商业插件首次安装若没有可用绑定，安装接口返回 HTTP `409` 和 `PLUGIN_AUTHORIZATION_REQUIRED`；这不是面板登录失效，前端不得清理管理员 Session。

- `GET /plugins/authorizations`：返回多个已保存账户的授权 ID、脱敏账户信息、状态和到期时间，不返回令牌；
- `POST /plugins/authorizations/device`：请求 `plugin_id` 和 `version`，返回本地 `attempt_id`、`user_code`、可信登录地址、过期时间和轮询间隔；
- `POST /plugins/authorizations/device/{attempt_id}/poll`：返回 `pending | slow_down | authorized | denied | expired`；
- `PUT /plugins/{plugin_id}/authorization`：请求 `authorization_id`，显式切换当前插件使用的账户并立即校验权益；
- `DELETE /plugins/authorizations/{authorization_id}`：撤销远端令牌并删除本地密文，不停用已安装插件。

授权成功后前端重试原安装请求。当前绑定账户没有权益时返回 HTTP `403`、`PLUGIN_ENTITLEMENT_DENIED`，`details.reason` 为 `not_granted | expired | instance_limit_reached | account_disabled`。客户端不得自动尝试其他已保存账户。

## Contributions

### GET `/plugins/contributions`

只返回已启用插件的贡献点：

```json
{
  "items": [
    {
      "id": "org.example.hello:hello-page",
      "plugin_id": "org.example.hello",
      "point": "global_page",
      "label": "Hello",
      "route": "hello",
      "ui_entry": "ui/main.js",
      "renderer": "iframe",
      "rpc_methods": []
    }
  ]
}
```

支持的 `point`：

- `global_page`：侧栏和全局插件路由；
- `site_detail_tab`：网站详情标签。

## 隔离 UI

### GET `/plugins/{plugin_id}/ui/{entry}`

返回由 nxPanel 生成的 HTML 宿主页。`entry` 必须严格等于 manifest 的 `ui.script`。

### GET `/plugins/{plugin_id}/assets/{asset}`

仅返回当前启用版本 manifest 中声明的 UI script 或 style。响应带 `nosniff` 和受限 CSP。

宿主页加载后，父页面会传入一个 MessagePort：

```javascript
window.addEventListener('message', (event) => {
  if (event.data?.type !== 'nxpanel:connect') return
  const port = event.ports[0]
})
```

Bridge 请求统一为：

```json
{"id":"1","type":"notify","payload":{"level":"success","message":"完成"}}
```

Bridge 响应统一为：

```json
{"id":"1","ok":true,"result":null}
```

## RPC

### POST `/plugins/{plugin_id}/rpc`

请求体：

```json
{
  "method": "method.name",
  "payload": {},
  "context": {"site_id": "site_xxx"}
}
```

普通插件的方法通过稳定的 `nxp_invoke` JSON ABI 调用；宿主仍会按 contribution 的 `rpc_methods` 白名单、1 MiB 请求/响应上限和调用超时执行。插件不能借此绕过 Host capability 权限检查。

内置 WAF 方法：

- `waf.overview`
- `waf.events.list`
- `waf.rules.check`
- `waf.site.get`
- `waf.site.save`
