# Nginx 模块管理

进入 **Nginx 管理 → 模块管理**（`/nginx?tab=modules`）。Agent 在线读取 NxPanel 主仓库 `main` 的 `catalog/nginx-modules/nxpanel-nginx-modules.json`。页面展示目录里的模块和本机检测到的模块；编辑或更换产物仓库不需要重编 Agent。刷新失败时，缓存仅供查看，不用于新安装。

| 位置 | 内容 |
| --- | --- |
| NxPanel 主仓库 `main` | Ed25519 签名目录：模块名称和版本、产物仓库及 Release tag、目标、`.so` 和两个脚本的大小及 SHA-256 |
| 独立模块产物仓库 | 模块源码或构建配方、`install.sh`、`uninstall.sh`；公开 GitHub Release 存放 `candidate.json` 和按内容哈希命名的 `.so`、`.sh` |
| 用户机器上的 Agent | 验签、匹配目标、逐文件校验、运行脚本、核验 Nginx 状态、保存卸载凭据；不内置 GeoIP2 的安装/卸载逻辑 |

只有系统、架构、运行时版本和 **Nginx 二进制 SHA-256 完全匹配**时才能安装。两个生产镜像均不预装 GeoIP2。Agent 验证已签名的 `.so` 和两个脚本后，才以 **root** 执行安装脚本。脚本不是沙箱代码：它可以安装系统包、修改配置并向 Nginx 发信号。签名代表发布者授权运行这些字节，不代表沙箱或安全审计。生产 Agent 不执行用户提交的 URL 或任意脚本。

GeoIP2 安装脚本先试载，只在缺少 `libmaxminddb` 时从系统现有可信源安装，不添加新源。它不自动下载 MMDB 或启用地域规则。卸载前需停用并同步地域规则，并移除自定义配置中的依赖。卸载脚本移除托管加载项，保留模块缓存、系统库、MMDB 和规则数据。两个操作都会检查配置并等待同一 master 的新 worker 接管；失败原因、脚本版本、SHA-256 和退出码可在操作记录查看。

已安装模块的卸载脚本和原始目录保存在 root 专属的 `receipts/`。GitHub 离线、目录下架或原始目录过期不阻止卸载；每次卸载仍会用当前受信公钥重新验签并检查 `.so`。凭据缺失、损坏或签名密钥已撤销时，面板不执行卸载。手工加载或静态编译的模块不是面板托管模块。

## 官方发布

### 1. 生成签名密钥

在受控机器上的 NxPanel 仓库根目录执行。目标目录必须不存在，放到 Git 仓库外：

```sh
umask 077
go run ./cmd/nxpanel-update-package -keygen "$HOME/nxpanel-module-release-1" -key-id module-release-1
```

复用热更新的 Ed25519 工具，**不复用热更新密钥**。`trust.pub` 是 `key_id:Base64公钥`，`signing.key` 是 `key_id:Base64私钥种子`。在 NxPanel 主仓库创建受保护的 `module-signing` 环境，仅允许 `main` 使用，设置必需审核人。用以下方式配置，不把私钥写入终端历史、构建日志、镜像或 Git：

```sh
gh variable set NXPANEL_MODULE_TRUST_KEYS < "$HOME/nxpanel-module-release-1/trust.pub"
gh secret set NXPANEL_MODULE_SIGNING_KEYS --env module-signing < "$HOME/nxpanel-module-release-1/signing.key"
```

在同一环境配置 `NXPANEL_MODULE_CATALOG_TOKEN`：仅对主仓库授予 Contents 和 Pull requests 写权限，不授予绕过分支保护的权限。官方 Agent 构建和镜像注入 `NXPANEL_MODULE_TRUST_KEYS`；本地生产构建可执行 `make build-agent NXPANEL_MODULE_TRUST_KEYS="$(cat path/to/trust.pub)"`。轮换时先发布同时信任新旧公钥的 Agent，再双签目录，处理仍依赖旧钥的卸载凭据后再撤销旧钥。生产目录每次发布必须使用更高的序号。

### 2. 在独立仓库准备候选产物

工作区示例位于 `local-module-repos/nginx-module-assets-example/`：这是独立 Git 仓库，被 NxPanel 主仓库忽略，不会随主仓库克隆。配置远程和发布 Release 由产物仓库维护者自行决定。示例已经提供 GeoIP2 的安装、卸载和脚本打包；`.so` 从与目标运行时完全匹配的构建中产生，不提交到源码 Git。

在示例 `module-definitions.json` 中设置实际的 `release.repository`（例如 `your-org/nginx-module-assets`）及 `release.tag`。目录布局如下，每个目标都有自己的 `.so` 和探测得到的 `target.json`：

```text
module-builds/
└── geoip2/
    ├── install.sh
    ├── uninstall.sh
    ├── debian12-amd64/
    │   ├── target.json
    │   └── ngx_http_geoip2_module.so
    └── alpine-amd64/
        ├── target.json
        └── ngx_http_geoip2_module.so
```

宿主机构建可复用 `scripts/build-host-geoip2.sh`（官方 Nginx/OpenResty 包，支持 Debian 12、Ubuntu 24.04、Rocky Linux 9；Ubuntu 22.04 可做本地测试）。Alpine 镜像产物由独立仓库的 `modules/geoip2/build-alpine.sh` 在目标镜像的一次性容器中编译，校验上游源码摘要、试载并记录实际二进制指纹。实际兼容性以签名目录为准。若增加模块，增加一个描述条目及独立的脚本和目标；无需修改 Agent 的路由或生命周期执行器。

在独立仓库根目录执行（`NXPANEL_DIR` 指向主仓库检出目录）。它打包自包含脚本，计算 `.so`、`.sh` 的摘要，并输出未签名的候选清单，不需要私钥：

```sh
sh modules/geoip2/package-scripts.sh module-builds/geoip2
go -C "$NXPANEL_DIR" run ./cmd/nxpanel-module-catalog -prepare \
  -metadata "$PWD/module-definitions.json" -input "$PWD/module-builds" -output "$PWD/module-release"
sha256sum module-release/candidate.json
```

将 `candidate.json`、全部 `.sh` 和 `.so` 原样上传到配置中的公开 Release。新版本用新 tag，不替换已发布资产。保存 `candidate.json` 的 SHA-256 并审核两个脚本和构建来源。产物仓库没有生产签名权限。

### 3. 审核、验证并签发目录

在 NxPanel `main` 手动运行 **Publish Nginx modules**，填写 `repository`、`tag`、`candidate_sha256` 和 `runtime_images`。例如：

```json
[{"arch":"amd64","binary":"/usr/sbin/nginx","image":"ghcr.io/your-org/nginx-module-verifier@sha256:<完整镜像摘要>"}]
```

准备只供签发验证用的镜像：为 Alpine 目标从固定摘要的目标面板镜像派生，仅通过 `RUN apk add --no-cache libmaxminddb` 添加依赖；为宿主机目标在对应发行版镜像安装同一个官方 Nginx/OpenResty 软件包及依赖。验证镜像还须包含 `/bin/sh` 和 `wget`，无需放入 `.so` 或脚本。将验证镜像发布到 `ghcr.io` 或 `docker.io`，在工作流中按摘要引用。每个目标都必须匹配一个验证镜像，否则不允许签发。

无签名凭据的验证任务从公开 Release 下载已固定摘要的候选文件，在无网络、无生产挂载的临时容器内实测安装和卸载。GeoIP2 另验 MMDB 查询及有依赖时拒绝卸载。受审核保护的签名任务只读取验证任务的产物和重新计算摘要，**不执行**候选脚本。工作流创建目录更新 PR；合并到 `main` 才生效。新目录替换旧目录，需继续支持的模块和目标要保留；目录有效期最长 30 天。

## 脚本接口与恢复

`nginx_dynamic_so` 每个模块版本提供一个通用的 POSIX 安装脚本和一个卸载脚本；每个目标提供一个 `.so`。Agent 在独立的 root 工作目录通过固定 `/bin/sh` 运行，不继承 Agent token、代理变量或签名密钥。位置参数是当前实例的 `-p`、`-c`、可选 `-g`，原样传给 `nginx -t`。不要用 `systemctl restart` 或启动另一个实例。示例脚本用这些变量：

| 变量 | 用途 |
| --- | --- |
| `NXMODULE_PROTOCOL`、`NXMODULE_ACTION`、`NXMODULE_ID`、`NXMODULE_SYMBOL` | 脚本接口版本 `1`、`install`/`uninstall`、模块 ID 和动态符号 |
| `NXMODULE_OS`、`NXMODULE_ARCH`、`NXMODULE_RUNTIME` | 匹配的发行版、架构和运行时 |
| `NXMODULE_ASSET`、`NXMODULE_ASSET_SHA256`、`NXMODULE_DEST` | 已验证的本地 `.so`、摘要和预期安装路径；卸载时资产指向现有 `.so` |
| `NXMODULE_WORK`、`NXMODULE_STATE`、`NXMODULE_FRAGMENT` | 任务目录、状态根目录及托管加载片段的预期路径 |
| `NXMODULE_NGINX`、`NXMODULE_CONF`、`NXMODULE_CONF_ALIAS`、`NXMODULE_CONF_SHA256`、`NXMODULE_BIN_SHA256` | 运行时和主配置的规范路径、原 `-c` 路径及摘要；替换前必须确认配置未被并发修改 |
| `NXMODULE_PANEL_DIR`、`NXMODULE_MASTER_PID`、`NXMODULE_MASTER_START` | 面板配置目录、锁定的 master PID 及 `/proc` 启动时间 |

加载入口由脚本写到主配置开头，内容为 `# NXPANEL-MANAGED-MODULES` 和 `include <NXMODULE_STATE>/active/*.conf;` 两行。加载片段为 `# NXPANEL-DYNAMIC-MODULE <NXMODULE_BIN_SHA256> <NXMODULE_SYMBOL>` 和 `load_module "<NXMODULE_DEST>";` 两行。不要覆盖别的模块、手写配置、数据或共享库。

- `0`：脚本完成；Agent 独立确认原 master、新 worker、配置和模块状态后才记成功。
- `10`：未修改 Nginx 配置或已恢复。Agent 复核配置和模块状态；GeoIP2 缺少依赖或仍被引用时可返回此状态。
- 其他退出码、超时或状态不符：显示「需人工检查」。Agent 限制输出和运行时间（脚本最长 5 分钟），不自动重试，也不把进程组限制当作 root 沙箱。

失败未确认时，维护窗口内先备份 `/var/lib/nxpanel-nginx-modules/script-task.json` 和其中 `directory` 指向的工作目录，以及当前配置。根据脚本保留的 `*.before` 比较修改，确认没有脚本或包管理进程仍在运行，检查模块加载项、`nginx -t`、重载结果、实际 worker 及规则状态。只有人工确认恢复后，才归档任务记录并重新检查模块状态。不要删除记录来强行重试。

**删除容器不保留模块**。状态目录不挂数据卷；保留的主配置受管 include 可以为空，但直接引用模块的站点配置仍会阻止 Nginx 启动。重建前停用并同步依赖；若已无法启动，先关闭对外流量，在维护窗口备份并临时移出依赖项，安装匹配模块后再恢复并检查规则。升级 Nginx 二进制前也建议先卸载依赖模块，升级后再安装匹配构建。

## 本地安装联调

只用一次性容器或独立测试 VM。`module_dev` 构建允许同时设置 `NXPANEL_MODULE_TEST_FEED`（完整目录 URL）和 `NXPANEL_MODULE_TEST_KEYS`（测试公钥），使用独立的测试签名域和 `/var/lib/nxpanel-nginx-modules-test`。正式构建忽略这些变量。不要把测试配置或测试密钥用在生产。

### Docker

从 NxPanel 仓库根目录运行。示例仓库不在默认路径时，先设置 `NXPANEL_MODULE_EXAMPLE` 指向其检出目录。根据镜像实际运行时版本，设置已核验的上游源码归档 `MODULE_RUNTIME_SHA256`：

```sh
docker build -f docker/Dockerfile.nginx --build-arg MODULE_AGENT_BUILD_TAGS=module_dev -t nxpanel:module-dev .
EXAMPLE=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
mkdir -p "$PWD/tmp/module-alpine-fixture"
docker run --rm --entrypoint /bin/sh \
  -v "$EXAMPLE/modules/geoip2/build-alpine.sh:/build-module.sh:ro" \
  -v "$PWD/scripts/nginx-module-smoke:/smoke:ro" \
  -v "$PWD/tmp/module-alpine-fixture:/out" nxpanel:module-dev \
  /build-module.sh "$MODULE_RUNTIME_SHA256" /out /smoke
bash scripts/test-nginx-modules-docker.sh nxpanel:module-dev tmp/module-alpine-fixture
bash scripts/test-nginx-modules-docker.sh nxpanel:module-dev tmp/module-alpine-fixture --manual
```

也可改用 `docker/Dockerfile.openresty` 和对应的源码摘要。编译只发生在一次性容器内。联调脚本从输出目录读取两个模块并签发本地测试源；测试面板不挂载构建目录，也必须能访问现有 Alpine 软件源以安装缺少的运行库。测试覆盖坏签名、任一脚本或 `.so` 篡改、安装与依赖拒绝、离线卸载及容器重建。`--manual` 启动同样的测试源和面板，打开打印的地址点击安装或卸载。

### Linux VM / 自有候选产物

`make module-fixture MODULE_NGINX_BIN=/usr/local/openresty/nginx/sbin/nginx` 读取测试 VM 的运行时指纹，在 Docker 编译试载并打印本地测试源目录；不同运行时版本可能需要设置官方源码摘要 `MODULE_RUNTIME_SHA256`。已有 `-prepare` 输出时可直接重签原始文件，测试私钥自动生成且不落盘：

```sh
go run ./cmd/nxpanel-module-catalog -candidate -test-fixture -sequence "$(date +%s)" \
  -input /path/to/module-release -output tmp/my-module-fixture
python3 -m http.server 18081 --bind 127.0.0.1 --directory tmp/my-module-fixture/feed
```

另一个终端运行测试 Agent（root）。确保 Nginx 二进制、`nginx.conf`、socket、数据目录等指向测试环境，API 用非 root 账号且只监听回环地址：

```sh
NXPANEL_MODULE_TEST_FEED=http://127.0.0.1:18081/nxpanel-nginx-modules.json \
NXPANEL_MODULE_TEST_KEYS="$(cat tmp/my-module-fixture/trust.pub)" \
make run-agent BUILD_TAGS=module_dev
```

再执行 `make build-frontend` 和 `make run-api`。打开模块页确认显示「本地测试源」，点击安装后才会修改测试 Nginx。测试目录 24 小时后过期；重新生成时使用新公钥重启开发 Agent。仍按完整目标指纹匹配，不支持用相近版本替代。
