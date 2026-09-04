# nxPanel Plugin SDK v1

> 本文件是安全模型和 manifest 的快速参考。完整中文教程请从 [插件文档索引](plugins/README.md) 开始。

nxPanel plugins are `.nxp` tar+gzip packages containing `manifest.json`, a WASM backend, and optional browser assets. Official packages are installed from the built-in TUF repository; unsigned local packages require the global developer mode and an explicit review.

## Security model

- Backends run in wazero without WASI, filesystem, environment, sockets, or host imports. Memory is limited to 64 MiB and lifecycle calls have a 30 second deadline.
- Browser code runs in an opaque-origin `sandbox="allow-scripts"` iframe. It communicates through a transferred `MessagePort`; the host checks every RPC method against the contribution allowlist.
- Plugins cannot send arbitrary Agent requests. Privileged features use compiled providers identified by a fixed provider ID and ABI.
- Package paths, expanded size, entry count, file sizes, and SHA-256 digests are verified before installation.

## Minimal manifest

```json
{
  "schema_version": 1,
  "id": "org.example.hello",
  "name": "Hello",
  "version": "1.0.0",
  "publisher": "Example",
  "plugin_api_version": "v1",
  "backend": "backend/plugin.wasm",
  "permissions": [],
  "files": [
    {"path":"backend/plugin.wasm","sha256":"<64 hex characters>","size":123}
  ]
}
```

The WASM module must export `nxp_health() -> i32`; zero means healthy. Enabling a plugin instantiates the module only after this check succeeds.

UI contributions use `global_page` or `site_detail_tab`, select `renderer: "iframe"`, and declare the exact `rpc_methods` the host may bridge. The built-in WAF uses `renderer: "native_waf"` and permission `native.waf.modsecurity`.

## Distribution trust

Official builds embed the metadata URL, targets URL and bootstrap TUF root. The client verifies root, timestamp, snapshot, targets, consistent snapshots, rollback/expiry, and target length and SHA-256. These trust settings cannot be replaced in the admin UI.

Developer mode accepts only a local `.nxp`, never a URL or custom repository. The inspect step returns a short-lived, single-use token and the package identity, SHA-256, permissions, network domains and Provider declarations for review. Developer packages remain marked `developer_unverified`; they cannot use `org.nxpanel.*` or overwrite an official ID.

Turning developer mode off does not stop existing developer plugins. It blocks new upload/install, local update, and re-enabling a stopped developer plugin. Disable and uninstall remain available.
