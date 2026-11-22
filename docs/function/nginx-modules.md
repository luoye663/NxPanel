# Nginx 模块管理

当前以 **GeoIP2** 为例。主仓库的 `main` 提供已签名的模块列表；每个模块仓库独立发布 GitHub Release。面板打开列表时不访问各模块仓库，点击安装后才读取所选模块的 Release，验证签名、兼容性、脚本和 `.so`。普通用户不需要生成密钥或手动运行脚本。

- [二进制安装](#二进制安装宿主机) · [Docker 安装](#docker-安装) · [卸载和排查](#卸载和排查)
- [本地联调](#本地联调) · [官方发布](#官方发布)

> Agent 验签并核对模块文件后，以 root 运行安装脚本。脚本会检查并平滑重载 Nginx；缺少 `libmaxminddb` 时，从当前系统配置的软件源安装，不添加新源。

## 二进制安装（宿主机）

1. 按 [二进制部署说明](../../README.md#二进制模式安装-nxpanel) 安装支持模块管理的 NxPanel。确认宿主机上的 Nginx 或 OpenResty 已运行，并已被面板检测到。
2. 登录面板，打开 **Nginx 管理 → 模块管理**（`/nginx?tab=modules`），点击 **刷新在线目录**。
3. 找到 **GeoIP2**，点击 **安装**。默认选择最新正式版，也可加载并选择历史正式版；等面板预检通过再确认。安装成功后到 **GeoIP 设置**安装 MMDB。安装模块本身不会下载数据库或启用地域规则。

## Docker 安装

1. 按 [Docker 部署说明](../../README.md#docker-部署) 启动 NxPanel。已有 Compose 配置时，在其目录按所用镜像执行一条：

   ```sh
   docker compose --profile nginx up -d
   # 如果使用 OpenResty，改为：docker compose --profile openresty up -d
   ```

2. 登录容器里的面板，打开 **Nginx 管理 → 模块管理**，点击 **刷新在线目录**。
3. 点击 **GeoIP2 → 安装**，选择版本，等预检通过再确认。模块和所需依赖装在容器内，不装到宿主机。成功后如需地域规则，再到 **GeoIP 设置**安装 MMDB。

**镜像不预装 GeoIP2，删除并重建容器也不会保留它。** 重建前先停用并同步依赖模块的规则，处理手写配置中的 GeoIP2 指令。新容器启动后，重新安装模块，再恢复这些规则和配置。

## 卸载和排查

1. 先停用并同步依赖 GeoIP2 的地域规则，移除手写 Nginx 配置中的 GeoIP2 指令。参见 [GeoIP2 升级与回滚](../access-policy.md#geoip2-升级与回滚)。
2. 到 **模块管理**点击 **卸载**。面板只卸载它管理的模块，不删除 MMDB、规则、运行库或缓存的 `.so`。凭据有效时可以离线卸载；手工加载、静态编译的模块不由面板卸载。已安装模块不会自动升级，换版本需先卸载，再选择要安装的正式版本。

| 遇到的情况 | 怎么处理 |
| --- | --- |
| 在线目录不可用 | 点击**刷新在线目录**。检查 Agent 能否访问 GitHub 主仓库、索引和签名是否一同发布。缓存只供查看，不能用于新安装。 |
| 找得到模块，但所选版本不可安装 | 检查该正式 Release 的签名是否可信、是否提供与本机系统、架构、Nginx/OpenResty 版本和二进制 SHA-256 完全匹配的构建。不会自动回退到其他版本。 |
| Nginx 已运行，但模块页提示无法核验实例或挂载路径 | 在「状态管理」确认面板实例和 worker。用 systemd 部署时保留 `PrivateTmp`、`ProtectSystem` 等安全加固，检查 unit 的主进程、启动配置，以及 Agent 和 Nginx 是否看到同一份主配置和模块目录。 |
| 模块加载目录与当前 Agent 的模块源不匹配 | 本地测试源使用 `/var/lib/nxpanel-nginx-modules-test/active/*.conf`，正式源使用 `/var/lib/nxpanel-nginx-modules/active/*.conf`。切换前用安装时的 Agent 版本和模块源卸载已安装模块（测试源需保留原公钥），不要直接改写 `include` 路径或移动模块文件。 |
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

1. 在 NxPanel 源码根目录生成测试源。此命令只构建模块和签名测试目录，不启动任何服务；安装模块前，另行启动测试 VM 上面板管理的 Nginx/OpenResty。以下以 OpenResty 为例，路径替换成实际二进制。其他运行时版本可能需要设置 `MODULE_RUNTIME_SHA256`。

   ```sh
   make module-fixture MODULE_NGINX_BIN=/usr/local/openresty/nginx/sbin/nginx
   ```

   如果已有模块仓库生成的 `.work/bundle/`，可改用 `go run ./cmd/nxpanel-module-catalog -candidate -test-fixture -sequence "$(date +%s)" -input /path/to/.work/bundle -output tmp/my-module-fixture`，无需重新编译 `.so`。

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

**模块仓库发版本，NxPanel 主仓库只登记模块及其公钥。** GeoIP2 的构建配置、源码脚本和发布工作流都在独立模块仓库，不复制进主仓库。

### 发布新版本

1. 在模块仓库用 `publish-targets.json` 列出要支持的 Nginx/OpenResty 目标。Ubuntu 和 Alpine 的 `.so` 放在**同一个 Release**，不另发 Docker 镜像。新增系统、包版本或架构时，添加匹配的目标配方；不存在匹配配方的环境不会被标为支持。
2. 在模块仓库 **Actions → Publish GeoIP2 module → Run workflow** 选择默认分支，只填一个新 tag，例如 `v3.4.0`。Actions 在各目标的临时容器内编译，打包后用实际运行时测试安装、查询和卸载。无需预先上传文件或创建 prerelease。
3. 全部目标通过后审核签名任务。Actions 上传 `.so`、安装／卸载脚本及签名清单，再发布正式版。已登记的模块无需改主仓库；新模块先完成下方登记。

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

<details>
<summary>维护发布模板或从旧版切换</summary>

开发模板未附工具版本时，发布会在构建前停止。兼容工具合入 NxPanel `main` 后，模板维护者将已审核、已发布的完整 40 位提交 SHA 作为单独一行写入 `.github/nxpanel-tools.sha`，随 GeoIP2 和通用模板一同交付。两个模板使用同一个提交；构建、验证和签名都读取此固定版本，不用分支名、标签或占位值。

在测试仓库确认下面的签名回写和 PR 检查正常后，旧部署可删除主仓库的 `NXPANEL_MODULE_CATALOG_TOKEN` Secret、撤销仅为此用途创建的令牌，并删除模块仓库的 `NXPANEL_TOOLS_REF` 变量。现有密钥和已安装模块不需要迁移。

</details>

### 登记新模块或修改登记信息

1. **开一个 PR。** 在 NxPanel 主仓库从最新 `main` 建分支，只改 [`catalog/nginx-modules/modules.json`](../../catalog/nginx-modules/modules.json)，填 `id`、`name`、`description`、`repository`（`owner/repo`）、`symbol` 和 `keys`（经可信渠道取得的模块公钥）。向 `main` 开非草稿 PR，不填 Release tag，不手写签名。外部投稿由维护者在主仓库开分支，签名流程不向 fork 回写。
2. **按 PR 编号签名。** 主仓库 **Actions → Sign Nginx module index → Run workflow** 选择 `main`，只填 `pr_number`（例如 `123`）。审批人核对准备任务记录的提交、仓库地址和公钥，再批准签名。Actions 将 `modules.sig.json` 提交到原 PR，不另开 PR。
3. **运行检查并合并原 PR。** 回到 PR，有仓库写权限的维护者在合并区点击 **Approve workflows to run**。这是批准运行回写后的检查，不是再次签名。等最新提交通过 **Nginx module index** 和其他必过检查后再审核合并。如果审批期间 PR 或 `main` 有更新，签名会停止；更新分支后按原 PR 编号重新签名。

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
