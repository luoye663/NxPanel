# Web 防火墙使用教程

nxPanel WAF 插件使用 ModSecurity v3 和 OWASP Core Rule Set 4。它不是纯 WASM 防火墙：WASM 插件负责生命周期和界面，流量检查由 Nginx 内的固定原生 Provider 完成。

## 1. 启用前提

以下文件必须由可信发行包预先安装：

```text
Nginx/OpenResty modules/
└── ngx_http_modsecurity_module.so

<nginx.panel_dir>/waf/
├── modsecurity.conf
└── rules/versions/4.25.0/
    ├── crs-setup.conf
    └── rules/*.conf
```

Provider 模块必须与当前 Nginx/OpenResty 的版本、configure arguments、二进制、操作系统、CPU 架构和 libc 指纹精确匹配。不兼容时 nxPanel 会阻止激活，不会在线编译或替换 Web Server。

当前源码仓库不附带正式签名目录、ModSecurity 二进制和 CRS 发行制品；这些应由正式 release/镜像提供。仅有源码时，插件中心出现 WAF 页面不代表数据面依赖已经安装。

## 2. 安装 WAF 插件

1. 在插件中心安装官方 WAF 插件；
2. 确认其请求 `native.waf.modsecurity` 权限；
3. 点击启用；
4. nxPanel 检测运行时指纹、定位内置模块、校验摘要和 ELF 架构；
5. Agent 写入受控 Provider 目录，在 `nginx.conf` 顶层插入 `load_module`；
6. 只有 `nginx -t` 和 reload 成功后才返回启用成功。

不要手工修改 `# nxpanel-waf-provider begin/end` 或 `#NXPANEL-WAF-START/END` 标记块。

## 3. 为站点启用 WAF

1. 打开“网站”并进入目标站点详情；
2. 进入插件贡献的“Web 防火墙”标签；
3. 打开启用开关；
4. 首次启用必须选择：
   - `DetectionOnly`：只观察和记录，不阻断；
   - `On`：按 CRS 异常评分阻断请求；
5. 保存并应用。

建议新站点先使用 `DetectionOnly` 运行一段时间，处理误报后再切换为 `On`。

应用时 Agent 会生成站点专属文件：

```text
<nginx.panel_dir>/waf/sites/<site_id>.conf
```

随后把固定 include 写进站点 `server` 上下文，执行文件事务、`nginx -t` 和 reload。测试或 reload 失败时会回滚配置。

## 4. 策略参数

### Paranoia Level

- PL1：推荐起点，误报相对较少；
- PL2：覆盖更多攻击变体，适合完成一轮调优后的站点；
- PL3–PL4：规则更激进，应经过充分观察和排除规则测试。

nxPanel 使用 CRS 4 的 `tx.blocking_paranoia_level`。

### 异常阈值

- 入站默认值：`5`；
- 出站默认值：`4`。

阈值越低越敏感。不要为了消除个别误报直接大幅提高全局阈值，应优先添加范围最小的排除规则。

### 请求和响应体限制

限制决定 ModSecurity 可以检查的内容大小。上传类站点需要结合业务请求大小配置；上限越大，内存和 CPU 压力也越大。

## 5. 排除规则

后端支持以下形式：

- 完整移除某个规则 ID；
- 仅对指定 URL 路径移除规则；
- 仅对指定参数名移除规则；
- 仅对指定 IP/CIDR 移除规则。

排除规则应遵循“规则 ID + 最小请求范围”的原则。避免在整个站点移除大类规则，也不要仅因为一次命中就添加排除。

## 6. 审计日志

启用站点 WAF 时会开启 Concurrent 审计日志，目录为：

```text
<nginx.panel_dir>/waf/audit/<site_id>/
```

目录权限为 `0750`，文件权限为 `0640`。审计事务可能包含请求头、Cookie、表单字段及部分响应内容，应把它视为敏感数据。

Agent 接口不接受任意路径，只接受站点 ID 和不可逆的 opaque event ID；单次预览最多 1 MiB，并跳过符号链接和非普通文件。

当前 UI 只展示结构化事件界面的基础骨架，全局 24 小时聚合和完整事务的二次认证查看尚未完成。不要把当前页面上的零统计理解为底层没有审计文件。

## 7. 保留和清理建议

建议默认策略为：保留 7 天或最多 5 GiB，先达到任一约束就清理最旧文件。Agent 的清理 RPC支持年龄与容量双条件，但当前尚未接入自动计划任务；在接入统一计划任务中心前，应由发行运维流程调用受控清理，而不是用面板插件执行任意 shell。

## 8. 故障处理

### nginx -t 报 include 文件不存在

确认 `modsecurity.conf`、`crs-setup.conf` 和 `rules/*.conf` 已由同一可信发行包安装，并且路径版本与策略中的规则版本一致。

### unknown directive "modsecurity"

模块没有被加载，或 Provider 与 Nginx 不兼容。检查主配置中的受控 `load_module` 块和 Agent 日志，不要手工绕过指纹检查。

### 启用后正常请求被拦截

立即切换回 `DetectionOnly`，根据审计记录确认命中规则和变量，再添加精确排除。不要直接停用插件；站点仍启用 WAF 时系统会拒绝停用插件。
