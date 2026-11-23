# Nginx 模块服务部署指南

本文面向运营官方模块服务的维护者。只想在面板安装模块，请阅读[模块商店使用指南](nginx-module-store-usage.md)，不需要部署构建服务。

**服务地址没有写死的默认域名。** Agent 启动时未填写 `NXPANEL_MODULE_STORE_URL`，会使用发行版构建时内置的地址；构建时也未填写，则模块服务未配置，不会回退到 GitHub。详见[服务地址的默认行为](#服务地址的默认行为)。

## 1. 准备构建主机和源码

在**专用 Linux 构建主机**上部署四个容器。构建服务所在主机不是用户运行 NxPanel 和网站的生产服务器。

| 容器 | 职责 | 所需权限 |
| --- | --- | --- |
| `api` | 提供目录、Release、构建状态和下载；鉴权后入队 | 仅在宿主机 `127.0.0.1:19092` 暴露 HTTP，由 HTTPS 反代访问；不访问 Docker Socket 或签名私钥 |
| `sync` | 从 GitHub 同步签名索引、正式 Release、原始脚本、源码和预编译成品 | 可访问 GitHub；GitHub 只读令牌可选 |
| `worker` | 获取可信运行时和源码，在独立临时容器中编译、试载模块 | 唯一挂载构建主机 Docker Socket 的服务；编译和试载容器均不挂载该 Socket |
| `signer` | 为试载通过的 `.so` 签发构建结果 | 无网络；唯一挂载构建私钥的服务，不签发安装脚本 |

准备 Docker Engine、Buildx、Compose v2、模块服务域名及其 TLS 证书，以及已经审核、相互兼容的 NxPanel 主仓库和构建服务源码版本。构建服务是独立 Git 仓库，工作区默认放在 `local-module-repos/nginx-module-builder/`，**不会随 NxPanel 主仓库克隆**。先从维护方取得独立仓库，再检出已审核的提交。用于发布和部署的 NxPanel Agent 也必须支持模块服务和源码 Release。生成密钥和授权令牌的可信机器还需准备 Go（版本以 NxPanel 的 `go.mod` 为准）和 OpenSSL。

构建机允许访问 GitHub、所用镜像仓库和受审核的 Nginx/OpenResty 软件源；用户生产服务器只需能访问模块服务就能查询和下载模块。安装运行库时，仍可能需要访问该服务器已有的软件源。

## 2. 构建服务镜像

在 NxPanel 主仓库根目录执行。如果独立仓库放在其他位置，修改 `BUILDER_SRC`；两条构建命令应使用同一份已审核的 `NXPANEL_SRC`：

```bash
export NXPANEL_SRC="$PWD"
export BUILDER_SRC="$NXPANEL_SRC/local-module-repos/nginx-module-builder"
export BUILDER_WORK=/var/lib/nxpanel-module-builder/work
cd "$BUILDER_SRC"

docker build --build-context "nxpanel=$NXPANEL_SRC" --target service \
  -t nxpanel-module-builder:service .
docker build --build-context "nxpanel=$NXPANEL_SRC" --target worker \
  -t nxpanel-module-builder:worker .
```

## 3. 准备持久化目录和 `.env`

首次部署复制 `.env.example` 为 `.env` 并设为 `0600`；已有部署应编辑而不是覆盖配置。容器默认以 `65532:65532` 运行。下面以没有 UID 映射的专用 Docker 主机为例：

```bash
install -m 0600 .env.example .env
sudo install -d -m 0700 -o 65532 -g 65532 data work/staging "$BUILDER_WORK"
sudo install -d -m 0700 secrets
```

`BUILDER_WORK` 在构建主机和 Worker 容器中必须是同一个绝对路径，不能包含逗号。优先使用专用的 rootless Docker daemon；使用 rootless 或 `userns-remap` 时，按实际 UID/GID 映射准备目录和私钥权限，不要直接照搬上面的属主。**Docker Socket 权限等价于控制对应 Docker daemon**，不能把网络隔离当成宿主机权限隔离；构建机不运行生产网站，也不存放其他生产凭据。

## 4. 配置签名密钥和构建授权

索引签名密钥、模块发布者密钥、构建签名密钥、面板热更新密钥必须分开。构建公钥不能用来授权模块脚本。首次部署在可信环境生成构建密钥，目标目录必须尚不存在：

```bash
go -C "$NXPANEL_SRC" run ./cmd/nxpanel-update-package \
  -keygen "$HOME/nxpanel-module-build-keys" -key-id module-build-1
```

通过安全通道将生成的 `signing.key` 放到构建服务的 `secrets/signing.key`，权限 `0600`，仅签名容器的运行身份可读。以下是无 UID 映射且密钥在同一可信主机上的示例：

```bash
sudo install -m 0600 -o 65532 -g 65532 \
  "$HOME/nxpanel-module-build-keys/signing.key" "$BUILDER_SRC/secrets/signing.key"
```

| `.env` 配置 | 填写方式 |
| --- | --- |
| `BUILDER_INDEX_KEYS` | NxPanel 模块索引当前受信任的公钥，与发行版 `NXPANEL_MODULE_TRUST_KEYS` 一致 |
| `BUILDER_UPDATE_KEYS` | 面板热更新公钥（如已配置）；用于防止与索引签名复用密钥 |
| `BUILDER_SIGNING_PUBLIC_KEY` | 构建密钥 `trust.pub` 的完整一行，例如 `module-build-1:BASE64`，与签名容器私钥配对 |
| `BUILDER_TOKEN_HASHES` | 各授权面板令牌原文的 SHA-256，小写十六进制、逗号分隔；留空则不接受新构建 |
| `BUILDER_GITHUB_TOKEN` | 可选的最小权限只读 GitHub 令牌，用于提高 GitHub API 配额 |
| `BUILDER_WORK` | 上一步准备的宿主机绝对路径 |
| `BUILDER_DOCKER_SOCKET` / `BUILDER_DOCKER_GID` | 构建 daemon 的 Socket 路径与 Worker 可访问 Socket 的 GID；普通 Docker 可用 `stat -c '%g' /var/run/docker.sock` 查询，不要假定为 `0` |

公钥配置填 `key-id:BASE64` 原文，不填文件路径。为每个授权面板单独生成构建令牌，例如 `openssl rand -hex 32`。将原文通过安全通道交给该面板；服务端只保存 `printf '%s' "$TOKEN" | sha256sum` 输出的第一列（不含末尾换行）。不要把私钥或令牌提交到 Git、传给浏览器、写到命令历史或打进公开镜像。

## 5. 配置运行时适配器

`adapters.json` 放在构建服务仓库根目录。只提供目录和现成 `.so` 时，新建此文件并填入 `[]`，然后跳到第 6 步。开启按需构建时，先按目标运行时选构建模式，再构建并发布适配器镜像。

| `mode` | `Dockerfile.runtime` target | 临时镜像构建上下文中的输入 |
| --- | --- | --- |
| `apt` | `apt` | `runtime-source.gpg`、`repository.gpg`、`runtime.list`。使用相同发行版的基础镜像；APT 源只指向对应的官方运行时软件源，`signed-by` 指向 `/usr/share/keyrings/nxpanel-builder-repository.gpg` |
| `rpm` | `rpm` | `runtime-source.gpg`、`repository.gpg`、`nxpanel-builder-runtime.repo`。DNF 源 ID 必须是 `nxpanel-builder-runtime`，使用官方软件源并开启 `gpgcheck=1` |
| `image` | `alpine` | `runtime-source.gpg`。基础镜像已经包含目标 Nginx/OpenResty 二进制；添加工具链时不得替换该二进制 |

`runtime-source.gpg` 是核对指纹后导出的**源码发布者公钥**（如 [Nginx 官方源码发布公钥](https://nginx.org/en/pgp_keys.html)），不要用软件包仓库公钥代替。Worker 验证对应运行时的源码归档和分离 PGP 签名。`apt`、`rpm` 按请求下载**精确**软件包版本，不需要在模块仓库逐个登记补丁版本；源不再保留旧包时，服务不会改用相近版本。`image` 则只适用于适配器镜像能重现的二进制；官方镜像更新后需要更新适配器，不需要重新发布模块源码。

例如，Ubuntu 24.04 上从 `nginx.org` 稳定源安装的 Nginx，其 `runtime.list` 可使用下列内容，并在 `repository.gpg` 中放入经核验的该软件源公钥。OpenResty 或其他发行版要改用各自的软件源与公钥，不能照搬本例。

```text
deb [signed-by=/usr/share/keyrings/nxpanel-builder-repository.gpg] https://nginx.org/packages/ubuntu noble nginx
```

准备好对应上下文后，设置 `ADAPTER_CONTEXT`（上表文件所在目录）、`BASE`（已审核的摘要固定基础镜像）和 `ADAPTER_TAG`（要上传的镜像标签）。以下以 Ubuntu/Nginx 的 APT 镜像为例：

```bash
docker buildx build --platform linux/amd64 --target apt \
  --build-arg "BASE=$BASE" --build-arg RUNTIME=nginx \
  -f "$BUILDER_SRC/Dockerfile.runtime" -t "$ADAPTER_TAG" --push "$ADAPTER_CONTEXT"
docker buildx imagetools inspect "$ADAPTER_TAG"
```

将发布的镜像地址和实际 `sha256` 摘要填入 `adapters.json`。以下是格式示例，不能直接使用占位符：

```json
[
  {
    "os": "ubuntu:24.04",
    "arch": "amd64",
    "runtime": "nginx",
    "image": "registry.example.com/nxpanel/nginx-builder@sha256:<实际摘要>",
    "mode": "apt"
  }
]
```

Worker 还会检查实际系统版本、Nginx/OpenResty 版本和二进制 SHA-256；即使配置能匹配，也不承诺未验证的包可构建。默认工具链带 GeoIP2 的头文件，其他模块的额外构建依赖应由运维审核并加入镜像。构建请求不能指定下载 URL、上传文件或传入命令。**每个准备上线的系统、运行时、架构组合应先在测试环境验收。**

## 6. 启动服务并检查同步

```bash
cd "$BUILDER_SRC"
docker compose config --quiet
docker compose up -d
docker compose ps
docker compose logs --tail=100 api sync worker signer
```

先从构建主机检查本地 API 和同步结果：

```bash
curl --fail --silent --show-error http://127.0.0.1:19092/health
curl --fail --silent --show-error http://127.0.0.1:19092/v1/modules.json
```

`/health` 返回 `204` 只说明 API 在运行。首次同步完成前，目录接口返回 `503`；如果始终返回 `503`，查看 `sync` 日志。若返回有效的空数组，检查主仓库签名索引是否已登记模块。索引登记及模块作者的发布步骤见[发布与联调指南](function/nginx-modules.md#官方发布)。

## 7. 配置 HTTPS 反代

给模块域名配置 HTTPS，将 `/v1/` 和 `/health` 原样反代到 `http://127.0.0.1:19092`。保留请求方法、路径和 `Authorization` 头；不要重定向下载，也不要在反代日志里打印令牌。公网入口应对 IP、连接数和下载带宽限流；构建提交接口 `POST /v1/builds` 建议单独限流。不要把本地 HTTP 端口或 Docker Socket 暴露到公网。

从能访问模块域名的主机检查公网入口，把示例域名替换为实际地址：

```bash
STORE_URL=https://modules.example.com
curl --fail --silent --show-error "$STORE_URL/health"
curl --fail --silent --show-error "$STORE_URL/v1/modules.json"
curl --fail --silent --show-error "$STORE_URL/v1/modules.sig.json"
```

## 8. 接入 NxPanel Agent

在 NxPanel **发行版的构建环境**配置下列变量，发行流程和 Docker 构建都应使用同一组值：

- `NXPANEL_MODULE_STORE_URL`：HTTPS 源站，例如 `https://modules.example.com`，不加 `/v1/` 或子路径。
- `NXPANEL_MODULE_TRUST_KEYS`：与 `BUILDER_INDEX_KEYS` 一致。
- `NXPANEL_MODULE_BUILD_TRUST_KEYS`：构建服务公钥；轮换时先在 Agent 部署新旧公钥，再让服务切换签名私钥，保留验证仍在使用的离线卸载凭据所需的旧公钥。

给授权面板的 **Agent 运行环境**设置 `NXPANEL_MODULE_BUILD_TOKEN`。如需覆盖发行版内置地址，也在 Agent 运行环境设置 `NXPANEL_MODULE_STORE_URL`；宿主机和 Docker 的配置方法见[使用指南](nginx-module-store-usage.md#配置服务地址和授权)。这些变量不是构建服务自身的 `BUILDER_*` 配置。

### 服务地址的默认行为

| Agent 运行时的 `NXPANEL_MODULE_STORE_URL` | 实际使用的地址 |
| --- | --- |
| 填了有效的 HTTPS 源站 | 使用填写的地址，覆盖发行版内置值；Agent 自动追加 `/v1/` |
| 未设置或留空，发行版构建时设置了该变量 | 使用编译进该 Agent 的地址。具体域名取决于发行版，不是固定值 |
| 未设置或留空，发行版构建时也未设置 | **没有默认地址**。在线目录会提示「未配置官方模块服务地址」，不会访问 GitHub，也不会使用示例域名或本机端口 |

例如，发行版构建时内置 `https://modules.example.com`，Agent 运行时留空，则目录请求会访问 `https://modules.example.com/v1/modules.json`。这是示例；直接用当前源码构建而未提供此变量，不会得到这个地址。填写无效的非空地址也不会回退到内置值。

## 9. 在测试面板验收

启动测试面板，验证以下场景后再接入生产：

1. 阻断面板到 GitHub 的连接，仍能刷新目录、选择正式 Release，并安装与当前运行时相符的签名模块。
2. 针对没有成品的源码 Release 提交构建，看到排队、构建、可安装；同一运行时和 Release 的另一台面板不带令牌也能复用成品。匿名请求不能触发构建。
3. 测试不支持的运行时和试载失败场景，确认没有改线上配置或重载。停止模块服务后，面板不能新装，但仍可使用有效凭据卸载托管模块。

## 日常运维

- **监控**：`sync` 每轮结束后间隔 10 分钟；上游失败时保留上一次镜像，同步时间超过 1 小时停止交付。监控 `/v1/modules.json`、各角色日志、磁盘占用和失败任务，不能只看 `/health`。
- **容量**：默认每令牌每小时 6 次新构建、全局最多 32 个活动任务、2 个同时编译，每次编译和试载最多 20 分钟（不含排队）。队列和成品共保留最多 512 条，达到上限需要按保留策略清理。镜像预算 256 MiB，每仓库从最近 50 个 Release 中最多选择 32 个可验证正式版本。
- **备份与升级**：`data/store.db` 存放镜像、索引防回退记录、队列和成品。停写后备份，或使用 SQLite 在线备份；不要在 WAL 模式运行时只复制主数据库文件。另行备份受审 `adapters.json`、配置和构建私钥，私钥备份单独加密。升级前在测试环境验证旧签名清单和卸载凭据，再更新服务镜像与 Agent。
- **处理异常**：构建失败先看 `worker` 和 `signer` 日志，检查精确包是否仍在上游、镜像权限、二进制指纹以及源码 PGP 公钥。令牌泄露时，移除对应摘要并重建 `api` 容器；签名私钥泄露则先停止交付，按安全发布流程撤销旧公钥并重新签发，不把失密密钥当作普通轮换处理。
