# Nginx 模块发布与联调

当前以 **GeoIP2** 为例。主仓库的 `main` 提供已签名的模块列表；模块仓库在 GitHub 发版，由官方模块服务同步。面板只访问该服务，不直连 GitHub，Agent 验证原发布者签名、脚本和 `.so`。源码 Release 可在缺少成品时申请按需构建；生产服务器不安装编译工具。

- [模块商店使用指南](../nginx-module-store-usage.md) · [模块服务部署指南](../nginx-module-store-deployment.md)
- [本地联调](#本地联调) · [官方发布](#官方发布)

## 本地联调

**只用测试 VM 或临时容器。** 测试签名仅由 `module_dev` Agent 接受，不需要生产密钥。独立示例仓库默认放在 `local-module-repos/nginx-module-assets-example/`，不会随主仓库克隆；放在其他位置时，请设置 `NXPANEL_MODULE_EXAMPLE` 为它的绝对路径。

测试源使用 `/var/lib/nxpanel-nginx-modules-test/active/*.conf`，正式源使用 `/var/lib/nxpanel-nginx-modules/active/*.conf`。切换前用安装时的 Agent 和签名源卸载已安装模块，不要直接改写 `include` 路径或移动模块文件。

### Docker 联调

1. 在 NxPanel 源码根目录构建开发镜像。测试 OpenResty 时换成 `docker/Dockerfile.openresty`。

   ```sh
   docker build -f docker/Dockerfile.nginx --build-arg MODULE_AGENT_BUILD_TAGS=module_dev -t nxpanel:module-dev .
   ```

2. 将 `MODULE_RUNTIME_SHA256` 设置为已核验的对应 Nginx/OpenResty 源码归档摘要，再用临时容器编译模块。

   ```sh
   EXAMPLE=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
   mkdir -p "$PWD/tmp/module-alpine-fixture"
   docker run --rm --entrypoint /bin/sh \
     -v "$EXAMPLE/modules/geoip2/build-alpine.sh:/build-module.sh:ro" \
     -v "$PWD/scripts/nginx-module-smoke:/smoke:ro" \
     -v "$PWD/tmp/module-alpine-fixture:/out" nxpanel:module-dev \
     /build-module.sh "$MODULE_RUNTIME_SHA256" /out /smoke
   ```

3. 启动面板和本地测试源。打开命令打印的地址，到 **模块管理**确认显示「本地测试源」，再安装或卸载。去掉 `--manual` 则运行自动联调。测试容器需要能访问 Alpine 软件源，才能安装缺少的运行库。

   ```sh
   bash scripts/test-nginx-modules-docker.sh nxpanel:module-dev tmp/module-alpine-fixture --manual
   ```

### 二进制联调（Linux VM）

1. 在 NxPanel 源码根目录调用模块仓库构建本机候选包，再用 NxPanel 工具生成本地签名测试源。命令不启动面板服务；安装模块前，另行启动测试 VM 上面板管理的 Nginx/OpenResty。以下以 OpenResty 为例，路径替换成实际二进制；没有匹配的发布配方时，设置 `MODULE_RUNTIME_SHA256` 为已核验的运行时源码归档摘要。

   ```sh
   EXAMPLE=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
   MODULE_NGINX_BIN=/usr/local/openresty/nginx/sbin/nginx \
     bash "$EXAMPLE/prepare-release.sh" "$PWD" luoye663/NxPanel-NginxModel-GeoIP2 v3.4.1 local-host
   go run ./cmd/nxpanel-module-catalog -candidate -test-fixture -sequence "$(date +%s)" \
     -input "$EXAMPLE/.work/local-host/bundle" -output tmp/my-module-fixture
   ```

   如果已有匹配本机的候选包，跳过构建，将 `-input` 指向现有候选包目录即可。

2. 在一个终端启动本地文件服务器。将 `FIXTURE` 改成上一步生成的目录（里面有 `feed/` 和 `trust.pub`）。

   ```sh
   FIXTURE=tmp/my-module-fixture
   python3 -m http.server 18081 --bind 127.0.0.1 --directory "$FIXTURE/feed"
   ```

3. 确认 `configs/config.yaml` 的 Nginx、socket 和数据目录都指向测试环境。在另一个终端以 root 启动测试 Agent；`FIXTURE` 使用和上一步相同的值。

   ```sh
   FIXTURE=tmp/my-module-fixture
   NXPANEL_MODULE_TEST_FEED=http://127.0.0.1:18081/modules.json \
   NXPANEL_MODULE_TEST_KEYS="$(cat "$FIXTURE/trust.pub")" \
   make run-agent BUILD_TAGS=module_dev
   ```

4. 用非 root 账号运行 `make build-frontend` 和 `make run-api`，测试 API 只监听回环地址。打开模块页，确认显示「本地测试源」后再点击安装并选择版本。生成新测试密钥时使用新的 VM／容器，或先卸载旧测试模块，再归档旧测试索引和测试状态。旧单一目录协议只在开发阶段使用过，切换前卸载旧测试模块或重建测试实例；新版不迁移旧凭据。

## 官方发布

**模块仓库独立发版，NxPanel 主仓库维护发布工具和模块索引。** GeoIP2 的构建目标配置、安装／卸载脚本和发布工作流都在独立模块仓库。已登记的模块发新版不需要改主仓库。

配置服务地址、发行版公钥和构建授权见[模块服务部署指南](../nginx-module-store-deployment.md)。

### 发布新版本

**优先发布签名源码。** 在示例模块仓库运行 **Publish GeoIP2 source release**，填新 tag；审核签名任务后，工作流发布源码归档、清单和安装／卸载脚本。无需为每个运行时编译成品。启用前先固定兼容的 `.github/nxpanel-tools.sha` 并部署支持源码清单的模块服务和 Agent。

**需要预编译成品时**，继续使用原工作流：

1. 在模块仓库用 `publish-targets.json` 固定构建环境，并可为同一 `.so` 声明多条 `compatibility` 规则。每条包含 `os_id`、`arch`、`runtime`，以及 `os_version` 和 `runtime_version` 的 `{min, max}` 闭区间。区间直接匹配本机报告的数字版本，不绑定构建环境或构建指纹；实际 ABI 仍由 Nginx 试载检查。不同产物的兼容范围不应重叠。所有 `.so` 放在**同一个 Release**，不另发 Docker 镜像。
2. 在模块仓库 **Actions → Publish GeoIP2 module → Run workflow** 选择默认分支，只填一个新 tag，例如 `v3.4.0`。Actions 在各目标的临时容器内编译，打包后用实际运行时测试安装、查询和卸载。无需预先上传文件或创建 prerelease。
3. 全部目标通过后审核签名任务。Actions 上传 `.so`、安装／卸载脚本及签名清单，再发布正式版。新模块可以先发布，但完成下方登记后，面板才会列出它。

含 `compatibility` 的清单需要支持范围匹配的 Agent；先升级 Agent，再用更新过的发布工具和新 tag 发布。旧清单、签名和离线卸载凭据继续可用。运行时换包前仍建议先卸载托管模块，避免 Nginx 启动时加载不兼容的旧模块。

**本地构建**：在独立模块仓库执行下列命令。脚本源码在 `modules/geoip2/`，要提交；`.work/builds/` 是编译中间目录，`.work/bundle/` 是唯一候选包，两者都不提交。命令不签名、不上传。

```sh
bash prepare-release.sh /path/to/nxpanel owner/repo v3.4.0
```

### 首次配置

**每个仓库只填一项公钥变量和一项私钥 Secret。** 填文件里的完整一行 `key-id:BASE64`，不填文件名或路径。公钥文件可留在本机，私钥不入 Git。索引、各模块和面板热更新使用不同密钥；已有的热更新变量不用改。

#### NxPanel 主仓库：只配一次

在可信的 NxPanel 源码根目录生成索引密钥：

```sh
go run ./cmd/nxpanel-update-package -keygen "$HOME/nxpanel-module-index-key" -key-id module-index-1
```

| 配置位置 | 名称 | 填什么、有什么用 |
| --- | --- | --- |
| Actions 仓库变量 | `NXPANEL_MODULE_TRUST_KEYS` | 索引 `trust.pub` 的内容。校验索引签名，新构建的 Agent 也会嵌入此公钥。 |
| `module-signing` 环境 Secret | `NXPANEL_MODULE_SIGNING_KEYS` | 索引 `signing.key` 的内容。给整个模块列表签名。 |

签名环境 `module-signing` 只允许 `main`，并要求审批。回写 PR 使用 Actions 自带的临时 `GITHUB_TOKEN`，不用创建个人令牌。先把 [签名工作流](../../.github/workflows/nginx-modules.yml) 和初始空索引合入 `main`，再将 **Nginx module index** 设为 `main` 的必过检查，要求合并前分支与 `main` 保持最新，并在签名回写后重新审核最新提交。

#### 模块仓库：每个模块配一次

在可信的 NxPanel 源码根目录生成模块密钥，以 GeoIP2 为例：

```sh
go run ./cmd/nxpanel-update-package -keygen "$HOME/nxpanel-geoip2-release-key" -key-id geoip2-1
cat "$HOME/nxpanel-geoip2-release-key/trust.pub"
```

| 配置位置 | 名称 | 填什么、有什么用 |
| --- | --- | --- |
| Actions 仓库变量 | `NXPANEL_MODULE_RELEASE_TRUST_KEYS` | 模块 `trust.pub` 的内容。检查签名密钥是否匹配；同一行也填到主仓库该模块条目的 `keys`。 |
| `module-release-signing` 环境 Secret | `NXPANEL_MODULE_RELEASE_SIGNING_KEYS` | 模块 `signing.key` 的内容。给模块 Release 签名。 |

签名环境 `module-release-signing` 只允许默认分支，并要求审批。用维护者提供的完整模板（工作流和 `.github/nxpanel-tools.sha`）；工具版本已固定，**不用在 Actions 填 SHA**。

#### 固定发布工具版本

发布工作流检出两份代码：模块仓库使用触发 Actions 的提交，决定发布哪份模块代码；NxPanel 主仓库使用 `.github/nxpanel-tools.sha` 指定的提交，提供宿主机编译、安装验证和发布清单签名工具。固定 SHA 让构建、验证和签名使用同一份经过审核的工具，不受 `main` 后续更新影响。它不是 GeoIP2 的 Release tag，也不决定用户安装哪个 NxPanel 版本。

如果出现 `Expected a checked-in .github/nxpanel-tools.sha file`，先检查触发发布的默认分支提交是否包含该文件。开发模板可能还未附带它，工作流会在编译或创建 tag 前停止。首次初始化按以下顺序处理：

1. **先合入工具。** 在 NxPanel 主仓库审核独立 Release 协议和兼容的发布工具，运行 `go test ./cmd/nxpanel-module-catalog ./internal/nginxmodule`，通过 CI 后合入远端 `main`。首次合入时保持索引为空，模块另开 PR 登记。不要使用尚未包含这些工具的 `main` 提交。
2. **再提交 SHA。** 从合入后的 `main` 选取包含上述工具的已审核提交，把完整 40 位小写十六进制 SHA 单独一行写入模块仓库的 `.github/nxpanel-tools.sha`，提交并推送到默认分支。GeoIP2 和通用模板使用同一个工具提交；不要填模块仓库的 SHA、分支名、标签或占位值。
3. **重跑发布。** 在模块仓库默认分支重新运行 **Publish GeoIP2 module**。如果原 tag 没被其他操作占用，可以继续用。等构建、打包和安装／卸载验证通过后再审批签名，并确认正式 Release 的资产齐全。不需要修改工作流或更换签名密钥。

<details>
<summary>从旧版切换</summary>

在测试仓库确认下面的签名回写和 PR 检查正常后，旧部署可删除主仓库的 `NXPANEL_MODULE_CATALOG_TOKEN` Secret、撤销仅为此用途创建的令牌，并删除模块仓库的 `NXPANEL_TOOLS_REF` 变量。现有密钥和已安装模块不需要迁移。

</details>

### 登记新模块或修改登记信息

1. **开一个 PR。** 在 NxPanel 主仓库从最新 `main` 建分支，只改 [`catalog/nginx-modules/modules.json`](../../catalog/nginx-modules/modules.json)，填 `id`、`name`、`description`、`repository`（`owner/repo`）、`symbol` 和 `keys`（经可信渠道取得的模块公钥）。向 `main` 开非草稿 PR，不填 Release tag，不手写签名。外部投稿由维护者在主仓库开分支，签名流程不向 fork 回写。
2. **按 PR 编号签名。** 主仓库 **Actions → Sign Nginx module index → Run workflow** 选择 `main`，只填 `pr_number`（例如 `123`）。审批人核对准备任务记录的提交、仓库地址和公钥，再批准签名。Actions 将 `modules.sig.json` 提交到原 PR，不另开 PR。
3. **运行检查并合并原 PR。** 回到 PR，有仓库写权限的维护者在合并区点击 **Approve workflows to run**。这是批准运行回写后的检查，不是再次签名。等最新提交通过 **Nginx module index** 和其他必过检查后再审核合并。如果审批期间 PR 或 `main` 有更新，签名会停止；更新分支后按原 PR 编号重新签名。

以空索引首次登记 GeoIP2 为例，`modules.json` 的完整内容如下：

```json
[
  {
    "id": "geoip2",
    "name": "GeoIP2（HTTP）",
    "description": "使用 MaxMind MMDB 查询客户端国家或地区。",
    "repository": "owner/geoip2",
    "symbol": "ngx_http_geoip2_module",
    "keys": "geoip2-1:BASE64_PUBLIC_KEY"
  }
]
```

提交前，将 `repository` 替换为实际模块仓库的 `owner/repo`，将 `keys` 替换为经可信渠道取得的模块 `trust.pub` 完整一行（`key-id:BASE64`），不要填写私钥。如果索引已有其他模块，只在数组中追加新条目或修改对应的条目，不要覆盖整份索引。

已登记的模块发新版不需要操作主仓库。更换模块密钥时，可以在 `keys` 用逗号分隔新旧公钥；删除旧公钥会撤销相应 Release 和卸载凭据的信任，非紧急情况先提醒用户卸载。

<details>
<summary>离线签发</summary>

在无私钥的机器生成候选包，用同一命令加 `verify` 验证本机架构；有多个架构时，用同一份完整 `.work/bundle/` 在各架构上验证。把已验证的包和已审核的 `candidate.json` 摘要移交离线机，离线机不运行候选脚本。

```sh
(
  set +x
  set -eu
  umask 077
  BUNDLE=/path/to/verified/bundle
  MODULE_KEYS="$HOME/nxpanel-geoip2-release-key"
  printf '%s  %s\n' "$CANDIDATE_SHA256" "$BUNDLE/candidate.json" | sha256sum -c
  NXPANEL_MODULE_RELEASE_TRUST_KEYS="$(cat "$MODULE_KEYS/trust.pub")" \
  NXPANEL_MODULE_RELEASE_SIGNING_KEYS="$(cat "$MODULE_KEYS/signing.key")" \
    go run ./cmd/nxpanel-module-catalog -candidate -input "$BUNDLE" -output "$BUNDLE"
)
```

在可信 NxPanel 源码根目录运行上例，或用预编译的 `nxpanel-module-catalog` 代替 `go run`。只带回清单、签名和匹配的 `.so`、`.sh`，上传到新 tag 的草稿 Release，再转正式版。若也离线签索引，分别向签名进程提供 `NXPANEL_MODULE_TRUST_KEYS`、`NXPANEL_MODULE_SIGNING_KEYS` 和（如有）`NXPANEL_UPDATE_TRUST_KEYS`，用 `-sign-index -input modules.json -output /path/to/signed-index -sequence "$SEQUENCE"`；序号须大于当前 `main`。只把签好的两份索引文件带回开 PR，私钥留在离线机。

</details>
