# Nginx 模块发布与联调

当前以 **GeoIP2** 为例。NxPanel 主仓库维护[根签授权清单](signing-trust.md)，各模块在自己的 GitHub 仓库发布签名源码、清单和安装／卸载脚本。模块服务同步这些材料，所有正式 `.so` 由服务按需编译、试载、签名并缓存。Agent 只访问模块服务，独立验证根授权、源码清单、构建签名和文件摘要；生产服务器不需要编译工具。

- [模块商店使用指南](../nginx-module-store-usage.md) · [模块服务部署指南](../nginx-module-store-deployment.md)

## 本地联调

**只用测试 VM 或临时容器。** 临时测试签名仅由 `module_dev` Agent 接受，不需要生产根密钥。独立示例仓库默认放在 `local-module-repos/nginx-module-assets-example/`，不会随主仓库克隆；可用 `NXPANEL_MODULE_EXAMPLE` 指定绝对路径。测试源使用 `/var/lib/nxpanel-nginx-modules-test/active/*.conf`；正式源使用 `/var/lib/nxpanel-nginx-modules/active/*.conf`。切换前用安装时的 Agent 和签名源卸载模块，不直接移动文件或改写 `include`。

### Docker 联调

在 NxPanel 源码根目录构建开发镜像。测试 OpenResty 时换用 `docker/Dockerfile.openresty`。将 `MODULE_RUNTIME_SHA256` 设置为已核验的对应运行时源码归档摘要。

```sh
docker build -f docker/Dockerfile.nginx --build-arg MODULE_AGENT_BUILD_TAGS=module_dev -t nxpanel:module-dev .
EXAMPLE=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
mkdir -p "$PWD/tmp/module-alpine-fixture"
docker run --rm --entrypoint /bin/sh \
  -v "$EXAMPLE/modules/geoip2/build-alpine.sh:/build-module.sh:ro" \
  -v "$PWD/scripts/nginx-module-smoke:/smoke:ro" \
  -v "$PWD/tmp/module-alpine-fixture:/out" nxpanel:module-dev \
  /build-module.sh "$MODULE_RUNTIME_SHA256" /out /smoke
bash scripts/test-nginx-modules-docker.sh nxpanel:module-dev tmp/module-alpine-fixture --manual
```

打开脚本打印的地址，确认模块管理显示「本地测试源」后试装卸。去掉 `--manual` 运行自动联调。测试容器需要访问 Alpine 软件源来安装缺少的运行库。

### 二进制联调（Linux VM）

1. 在主仓库根目录构建本机候选包；已有匹配候选包时，用其目录替换 `-input`。以 OpenResty 为例，将 `MODULE_NGINX_BIN` 换成实际二进制；没有匹配配方时提供已核验的源码摘要 `MODULE_RUNTIME_SHA256`。

   ```sh
   EXAMPLE=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
   MODULE_NGINX_BIN=/usr/local/openresty/nginx/sbin/nginx \
     bash "$EXAMPLE/prepare-release.sh" "$PWD" luoye663/NxPanel-NginxModel-GeoIP2 v3.4.1 local-host
   go run ./cmd/nxpanel-module-catalog -candidate -test-fixture -sequence "$(date +%s)" \
     -input "$EXAMPLE/.work/local-host/bundle" -output tmp/my-module-fixture
   ```

2. 在一个终端用 `python3 -m http.server 18081 --bind 127.0.0.1 --directory tmp/my-module-fixture/feed` 提供临时源。正式 Agent 不接收这个临时源。
3. 确认 `configs/config.yaml` 的 Nginx、socket 和数据目录都指向测试环境，并启动测试 Nginx/OpenResty。在另一终端以 root 启动测试 Agent：

   ```sh
   NXPANEL_MODULE_TEST_FEED=http://127.0.0.1:18081/modules.json \
   NXPANEL_MODULE_TEST_KEYS="$(cat tmp/my-module-fixture/trust.pub)" \
   make run-agent BUILD_TAGS=module_dev
   ```

4. 以非 root 账号运行 `make build-frontend` 和 `make run-api`，测试 API 只监听回环地址。重新生成测试密钥前，先卸载旧测试模块并归档其状态，或重建测试实例。

## 官方发布

### 首次配置与登记

1. 按[根签名指南](signing-trust.md)初始化主仓库。给每个模块独立生成密钥，例如 `go run ./cmd/nxpanel-update-package -keygen "$HOME/nxpanel-geoip2-release-key" -key-id geoip2-1`。
2. 在主仓库 `trust.json` 的 `modules` 中登记模块的 `id`、`name`、`description`、`repository`、`symbol` 和 `keys`。GeoIP2 示例：`geoip2`、`luoye663/NxPanel-NginxModel-GeoIP2`、`ngx_http_geoip2_module`，以及对应的完整公钥。增加序号，离线签发并提交公开的 `trust.json` 和 `trust.sig`。
3. 在模块仓库设置 Actions 变量 `NXPANEL_ROOT_PUBLIC_KEY`（NxPanel 根公钥）及受保护的 `module-release-signing` 环境 Secret `NXPANEL_MODULE_RELEASE_SIGNING_KEYS`（该模块私钥）。值都填 `key-id:BASE64` 完整一行，不填路径。签名环境只允许默认分支且要求审批。
4. 将模块仓库的 `.github/nxpanel-tools.sha` 固定到主仓库已审核且支持根签授权的提交，填完整 40 位小写 SHA。先合入工具和工作流，再发布首版清单，最后更新模块仓库的工具 SHA。签名任务读取主仓库当前公开授权，但不执行该分支的代码。

### 发布新版本

在模块仓库运行 **Publish GeoIP2 source release**，填新 tag。签名任务核验候选摘要、授权、仓库和 tag，再上传签名清单、源码归档和安装／卸载脚本。不要编译或上传 `.so`；清单声明了预编译资产的 Release 不会被服务收录。

在本机可用 `bash prepare-release.sh /path/to/nxpanel owner/repo v3.4.0 source` 生成纯源码候选包。`.work/` 不提交。若离线签名，审核 `candidate.json` 的 SHA-256，不在签名机执行候选脚本，再运行：

```sh
NXPANEL_ROOT_PUBLIC_KEY="$(cat /path/to/root/trust.pub)" \
NXPANEL_MODULE_RELEASE_SIGNING_KEYS="$(cat /path/to/module/signing.key)" \
  go run ./cmd/nxpanel-module-catalog -candidate -trust-dir /path/to/signed-policy \
    -input /path/to/verified/bundle -output /path/to/verified/bundle
```

上传 Release 清单、签名和清单列出的文件到新 tag 的草稿，再发布正式版。正常发版本不需要改主仓库或使用根私钥；新登记、换钥和撤销才更新[授权清单](signing-trust.md)。
