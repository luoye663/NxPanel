# Nginx 模块管理

当前以 **GeoIP2** 为例。NxPanel 从主仓库读取签名目录，再从独立产物仓库的 GitHub Release 下载模块和安装脚本。普通用户在面板操作即可，不需要生成签名密钥或手动运行脚本。

- [二进制安装](#二进制安装宿主机) · [Docker 安装](#docker-安装) · [卸载和排查](#卸载和排查)
- [本地联调](#本地联调) · [官方发布](#官方发布)

> Agent 验签并核对模块文件后，以 root 运行安装脚本。脚本会检查并平滑重载 Nginx；缺少 `libmaxminddb` 时，从当前系统配置的软件源安装，不添加新源。

## 二进制安装（宿主机）

1. 按 [二进制部署说明](../../README.md#二进制模式安装-nxpanel) 安装支持模块管理的 NxPanel。确认宿主机上的 Nginx 或 OpenResty 已运行，并已被面板检测到。
2. 登录面板，打开 **Nginx 管理 → 模块管理**（`/nginx?tab=modules`），点击 **刷新在线目录**。
3. 找到 **GeoIP2**，点击 **安装** 并确认。等页面显示安装成功，再到 **GeoIP 设置**安装 MMDB。安装模块本身不会下载数据库或启用地域规则。

## Docker 安装

1. 按 [Docker 部署说明](../../README.md#docker-部署) 启动 NxPanel。已有 Compose 配置时，在其目录按所用镜像执行一条：

   ```sh
   docker compose --profile nginx up -d
   # 如果使用 OpenResty，改为：docker compose --profile openresty up -d
   ```

2. 登录容器里的面板，打开 **Nginx 管理 → 模块管理**，点击 **刷新在线目录**。
3. 点击 **GeoIP2 → 安装**。模块和所需依赖装在容器内，不装到宿主机。成功后如需地域规则，再到 **GeoIP 设置**安装 MMDB。

**镜像不预装 GeoIP2，删除并重建容器也不会保留它。** 重建前先停用并同步依赖模块的规则，处理手写配置中的 GeoIP2 指令。新容器启动后，重新安装模块，再恢复这些规则和配置。

## 卸载和排查

1. 先停用并同步依赖 GeoIP2 的地域规则，移除手写 Nginx 配置中的 GeoIP2 指令。参见 [GeoIP2 升级与回滚](../access-policy.md#geoip2-升级与回滚)。
2. 到 **模块管理**点击 **卸载**。面板只卸载它管理的模块，不删除 MMDB、规则、运行库或缓存的 `.so`。凭据有效时可以离线卸载；手工加载、静态编译的模块不由面板卸载。

| 遇到的情况 | 怎么处理 |
| --- | --- |
| 在线目录不可用 | 点击**刷新在线目录**。检查 Agent 能否访问 GitHub，以及是否已有正式签名目录。缓存只供查看，不能用于新安装。 |
| 没有「安装」按钮 | 目录必须包含与当前系统、架构、Nginx/OpenResty 版本及二进制 SHA-256 完全匹配的构建。不要拿相近版本的 `.so` 替代。 |
| 提示仍有依赖或缺少卸载凭据 | 先处理仍引用模块的规则或配置。凭据缺失、损坏或其签名公钥已撤销时，面板不会执行卸载。 |
| 提示「需人工检查」 | 查看操作记录和 Nginx 日志。备份当前配置、`/var/lib/nxpanel-nginx-modules/script-task.json` 和记录中的工作目录。对比 `*.before`，确认配置和运行状态恢复后再归档记录，不要强行重试。 |

## 本地联调

**只用测试 VM 或临时容器。** 测试签名仅由 `module_dev` Agent 接受，不需要生产密钥。独立示例仓库默认放在 `local-module-repos/nginx-module-assets-example/`，不会随主仓库克隆；放在其他位置时，请设置 `NXPANEL_MODULE_EXAMPLE` 为它的绝对路径。

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

1. 在 NxPanel 源码根目录生成测试源。以下以 OpenResty 为例，路径替换成测试 VM 上的实际二进制。其他运行时版本可能需要设置 `MODULE_RUNTIME_SHA256`。

   ```sh
   make module-fixture MODULE_NGINX_BIN=/usr/local/openresty/nginx/sbin/nginx
   ```

   如果已有打包好的候选产物，可改用 `go run ./cmd/nxpanel-module-catalog -candidate -test-fixture -sequence "$(date +%s)" -input /path/to/module-release -output tmp/my-module-fixture`，无需重新编译 `.so`。

2. 在一个终端启动本地文件服务器。将 `FIXTURE` 改成上一步生成的目录（里面有 `feed/` 和 `trust.pub`）。

   ```sh
   FIXTURE=tmp/my-module-fixture
   python3 -m http.server 18081 --bind 127.0.0.1 --directory "$FIXTURE/feed"
   ```

3. 确认 `configs/config.yaml` 的 Nginx、socket 和数据目录都指向测试环境。在另一个终端以 root 启动测试 Agent；`FIXTURE` 使用和上一步相同的值。

   ```sh
   FIXTURE=tmp/my-module-fixture
   NXPANEL_MODULE_TEST_FEED=http://127.0.0.1:18081/nxpanel-nginx-modules.json \
   NXPANEL_MODULE_TEST_KEYS="$(cat "$FIXTURE/trust.pub")" \
   make run-agent BUILD_TAGS=module_dev
   ```

4. 用非 root 账号运行 `make build-frontend` 和 `make run-api`，测试 API 只监听回环地址。打开模块页，确认显示「本地测试源」后再点击安装。测试目录 24 小时后过期；重新生成后，用新公钥重启测试 Agent。

## 官方发布

以下只面向发布者。**独立仓库上传文件，NxPanel 主仓库签发目录。**

### 1. 首次配置签名密钥

在 NxPanel 源码根目录运行。选择 Git 仓库以外、尚不存在的目标目录；**不复用热更新密钥**。

```sh
umask 077
KEY_DIR="$HOME/nxpanel-module-release-1"
go run ./cmd/nxpanel-update-package -keygen "$KEY_DIR" -key-id module-release-1
```

1. 在 NxPanel 主仓库创建 GitHub Actions 环境 `module-signing`，仅允许 `main` 使用并设置审核人。
2. 在 NxPanel 主仓库配置 Actions 变量 `NXPANEL_MODULE_TRUST_KEYS`，填 `trust.pub` 的内容。官方 Agent 和镜像构建会嵌入该公钥；自建 Agent 可在构建时传入同名变量。
3. 在 `module-signing` 环境配置两个 Secret：`NXPANEL_MODULE_SIGNING_KEYS` 填 `signing.key` 的内容，`NXPANEL_MODULE_CATALOG_TOKEN` 填仅有主仓库 Contents 和 Pull requests 写权限的令牌。私钥不要提交到 Git，也不要放到产物仓库或镜像。

### 2. 打包并上传独立仓库产物

1. 在独立仓库的 `module-definitions.json` 填实际的 `release.repository` 和 `release.tag`。
2. 为每个目标生成匹配的 `.so` 和 `target.json`，放入 `module-builds/geoip2/<目标名>/`。宿主机可用 [`scripts/build-host-geoip2.sh`](../../scripts/build-host-geoip2.sh)，Alpine 容器用独立仓库的 `modules/geoip2/build-alpine.sh`。具体构建命令见独立仓库的 `modules/geoip2/README.md`。
3. 在独立仓库根目录执行 `sh prepare-release.sh /path/to/nxpanel`。它会打包安装、卸载脚本，并打印 `candidate.json` 的 SHA-256，无需私钥。
4. 将 `module-release/` 中的 `candidate.json`、`.sh` 和 `.so` 上传至对应的公开 GitHub Release。每次发布使用新 tag，不覆盖旧资产。

### 3. 验证并签发目录

1. 为每个目标准备含**同一 Nginx/OpenResty 二进制**、运行依赖、`/bin/sh` 和 `wget` 的验证镜像。Alpine 可从目标面板镜像派生并安装 `libmaxminddb`；验证镜像不用预装模块。按镜像摘要引用，不用 `latest`。
2. 在 NxPanel `main` 运行 **Publish Nginx modules**，填写 `repository`、`tag`、`candidate_sha256` 和 `runtime_images`。最后一项示例：

   ```json
   [{"arch":"amd64","binary":"/usr/sbin/nginx","image":"ghcr.io/your-org/nginx-module-verifier@sha256:<镜像摘要>"}]
   ```

3. 验证和签名通过后，审核并合并工作流创建的 PR。目录合入 `main` 后，面板才会读到新内容。新目录会替换旧目录，须带上仍需支持的目标，并在 30 天内续发。
