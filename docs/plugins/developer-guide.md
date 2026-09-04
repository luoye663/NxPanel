# 插件开发教程

本文通过一个最小插件说明 nxPanel Plugin API v1 的开发、打包和本地安装方法。开发者插件不需要官方签名，但只有管理员开启开发者模式并复核完整声明后才能安装。

## 1. 开发环境与目录

建议目录：

```text
hello-plugin/
├── backend/
│   └── plugin.wasm
├── ui/
│   ├── main.js
│   └── style.css
└── manifest.json
```

`.nxp` 是 gzip 压缩的 tar 包，`manifest.json` 必须位于包根目录。

## 2. 编写 WASM 后端

v1 必须导出无参数、返回 i32 的函数：

```text
nxp_health() -> i32
```

返回 `0` 表示健康，其他值表示不可启用。

以 Rust 为例：

```rust
#[unsafe(no_mangle)]
pub extern "C" fn nxp_health() -> i32 {
    0
}
```

构建时选择无 WASI 依赖的 WebAssembly target。nxPanel 不向模块提供 WASI、文件系统、环境变量、socket 或网络能力。若模块在实例化时导入这些能力，校验会失败。

运行时内存上限为 64 MiB；校验和启停生命周期调用的超时为 30 秒。

## 3. 编写隔离 UI

`ui/main.js` 需要接收宿主通过 `window.postMessage` 转交的 `MessagePort`：

```javascript
let hostPort
let sequence = 0
const pending = new Map()

window.addEventListener('message', (event) => {
  if (event.data?.type !== 'nxpanel:connect' || !event.ports[0]) return

  hostPort = event.ports[0]
  hostPort.onmessage = ({ data }) => {
    const callback = pending.get(data.id)
    if (!callback) return
    pending.delete(data.id)
    data.ok ? callback.resolve(data.result) : callback.reject(new Error(data.error?.message))
  }
  hostPort.start()

  document.querySelector('#nxpanel-plugin-root').textContent =
    `插件已连接，主题：${event.data.color_scheme}`
})

function request(type, payload) {
  if (!hostPort) return Promise.reject(new Error('宿主尚未连接'))
  const id = `req-${++sequence}`
  hostPort.postMessage({ id, type, payload })
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
}

window.helloNotify = function notify(message) {
  return request('notify', { level: 'success', message })
}

window.helloResize = function resize(height) {
  return request('resize', { height })
}
```

可用 Bridge 类型：

- `notify`：显示成功或错误通知，文本最多 500 字符；
- `confirm`：请求宿主显示确认框；
- `navigate`：只允许以 `/` 开头的面板内部路径；
- `theme`：读取当前颜色模式；
- `resize`：请求 360–2400 px 的 iframe 高度；
- `rpc`：仅允许 contribution 的 `rpc_methods` 白名单。

iframe 使用 `sandbox="allow-scripts"`，没有 `allow-same-origin`。插件 UI 不应尝试读取父页面 DOM、Cookie、localStorage 或 CSRF token，也不能自行调用管理员 API。

当前宿主以普通 `<script src>` 加载入口，因此入口文件应使用普通脚本语法；不要在顶层使用 `import` / `export`。需要打包依赖时，请先用前端构建工具输出单文件 IIFE。

通用 RPC 通过 `nxp_invoke` JSON ABI 调用插件，并受 contribution 的 `rpc_methods` 白名单约束。`native_waf` renderer 和 `waf.*` RPC 只供 nxPanel 内置 WAF 使用。

## 4. 编写 manifest

```json
{
  "schema_version": 1,
  "id": "org.example.hello",
  "name": "Hello Plugin",
  "version": "1.0.0",
  "publisher": "Example Team",
  "plugin_api_version": "v1",
  "panel_version": ">=1.0.0",
  "backend": "backend/plugin.wasm",
  "ui": {
    "script": "ui/main.js",
    "style": "ui/style.css"
  },
  "permissions": [],
  "network_domains": [],
  "providers": [],
  "contributions": [
    {
      "id": "hello-page",
      "point": "global_page",
      "label": "Hello",
      "route": "hello",
      "ui_entry": "ui/main.js",
      "renderer": "iframe",
      "rpc_methods": []
    }
  ],
  "files": [
    {
      "path": "backend/plugin.wasm",
      "sha256": "REPLACE_WITH_SHA256",
      "size": 123
    },
    {
      "path": "ui/main.js",
      "sha256": "REPLACE_WITH_SHA256",
      "size": 456
    },
    {
      "path": "ui/style.css",
      "sha256": "REPLACE_WITH_SHA256",
      "size": 78
    }
  ]
}
```

约束：

- `id` 使用小写反向域名格式，例如 `org.example.hello`；
- `version` 使用语义化版本；
- `plugin_api_version` 当前只能为 `v1`；
- `backend` 必须是包内安全相对路径且以 `.wasm` 结尾；
- `files` 必须精确列出除 `manifest.json` 外的所有普通文件；
- 包不能包含符号链接、硬链接或其他特殊文件；
- contribution point 只能是 `global_page` 或 `site_detail_tab`；
- renderer 只能是 `iframe` 或保留给内置功能的 `native_waf`。

## 5. 权限

解析器当前识别以下权限名：

```text
panel.sites.read
plugin.kv
plugin.events
plugin.secrets
http.fetch
scheduled_tasks.manage
operations.write
notifications.show
native.waf.modsecurity
```

“可被 manifest 识别”不代表对应 Host 方法已经注册。当前通用 broker 已注册 `kv.get`、`kv.put`、`kv.delete`、`events.publish` 和 `http.fetch`，分别要求 `plugin.kv`、`plugin.events` 和 `http.fetch`。其他权限只在相应受控能力接入后生效。不要声明未使用的权限。

`panel_version` 当前只作为元数据保存，尚未由安装服务执行版本范围判定；发布者必须在官方目录生成阶段完成兼容性测试，不能依赖客户端字段自动阻止不兼容安装。

## 6. 生成摘要和打包

在插件目录执行：

```bash
sha256sum backend/plugin.wasm ui/main.js ui/style.css
wc -c backend/plugin.wasm ui/main.js ui/style.css
```

把摘要和字节数填入 `manifest.json`，然后以确定顺序打包：

```bash
tar --sort=name \
  --owner=0 --group=0 --numeric-owner \
  --mtime='UTC 2026-01-01' \
  -czf org.example.hello-1.0.0.nxp \
  manifest.json backend/plugin.wasm ui/main.js ui/style.css

sha256sum org.example.hello-1.0.0.nxp
wc -c org.example.hello-1.0.0.nxp
```

压缩包默认限制：压缩后不超过 64 MiB、展开后不超过 256 MiB、条目不超过 10,000。

## 7. 本地验证与安装

推荐先用 Go 测试复用正式校验器：

```go
manifest, err := plugin.VerifyPackage(
    context.Background(),
    "org.example.hello-1.0.0.nxp",
    "<package sha256>",
    t.TempDir(),
    plugin.DefaultPackageLimits,
)
```

项目内可运行：

```bash
go test ./internal/plugin
go test ./internal/api
```

完整集成测试应创建临时签名目录，启动 API，执行安装、启用、贡献点读取、iframe 加载、停用和卸载流程。

手工测试时，进入“插件中心 → 开发者”，明确开启开发者模式，然后选择本地 `.nxp`。检查页会显示包 SHA-256、插件 ID、声明发布者、版本、权限、网络域名和 Provider。核对后再次确认安装。

开发者安装不接受 URL 或自定义仓库，且有以下 ID 规则：

- 不能使用官方保留前缀 `org.nxpanel.*`；
- 不能覆盖当前官方目录中的插件 ID；
- 官方目录后来出现同 ID 时，开发者插件会标记来源冲突，不能更新或重新启用。

关闭开发者模式不会停止正在运行的开发者插件，但会禁止上传、本地更新和重新启用。测试完成后应停用并卸载不再需要的开发者插件。

## 8. 版本升级原则

- 任何文件变化都必须更新对应摘要和 size；
- 发布新包必须提高插件语义化版本；
- 更新若新增或删除权限，管理员必须重新确认；
- 不要用相同版本覆盖已经发布的包；
- 不要降低目录 `version`；
- ABI 不兼容变更应发布新的 `plugin_api_version`，不能静默改变 v1 行为。

## 9. 权限与 Provider 边界

官方插件和开发者插件通过同一个能力 broker。开发者插件可以申请所有已注册能力，但 manifest 声明、管理员批准和每次调用的运行时检查必须同时满足。这个规则不提供任意 shell、root 文件系统、未登记网络访问或自带原生 `.so`；需要 root 的功能必须由 nxPanel 预先编译和注册固定 Provider。
