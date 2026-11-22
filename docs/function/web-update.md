# 面板在线更新

在线更新只替换面板 API 和 Agent，不重启站点 Nginx/OpenResty。下载完成后，必须手动点击 **重启并应用** 才会切换版本。

## 在面板中更新

1. **确认部署支持在线更新。** 已安装带更新器的发行包或镜像，可直接操作。旧版需按下文[首次启用](#首次启用与日常维护)升级一次；普通源码开发版本只能检查更新，不能安装。
2. **选择通道并检查。** 点击右上角 **程序版本**，在 **程序更新** 弹窗中选择 **稳定版** 或 **预览版**。若要重新检查，点击右侧的刷新按钮。先核对 **当前版本**、**通道版本** 和对应的 **更新内容**。

   | 通道 | 显示内容 |
   | --- | --- |
   | 稳定版（默认，推荐生产环境） | 仅显示正式版，例如 `v1.2.0`。 |
   | 预览版 | 仅显示预发布版，例如 `v1.3.0-beta.1` 或 `v1.3.0-rc.1`。没有预发布版时显示 **暂无预览版**，不拿稳定版补位。 |

   只有比当前版本更新、且有可验证兼容更新包的版本才可安装。切换通道不会更改当前版本，也不会降级。

3. **下载并暂存。** 点击 **立即更新**，等待进度变为 **等待手动重启**。这一步只下载、验签并暂存；此时普通重启面板或容器不会应用更新。不想继续时，可点击 **放弃**。
4. **重启并应用。** 点击 **重启并应用** 并在弹窗中确认。面板和 Agent 会短暂断开，请等待自动重连，不要重复确认。如果面板提示仍有写入或后台任务在运行，等任务结束后再试。
5. **确认结果。** 重连后，在 **程序更新** 中确认进度显示 **更新完成**，且 **当前版本** 已切换到目标版本。如果收到失败通知，按下文[更新失败与恢复](#更新失败与恢复)排查。失败通知只在发起更新的页面显示一次；重新打开弹窗或刷新页面不会再显示旧失败进度。

## 首次启用与日常维护

### 宿主机安装

1. 从旧版迁移时，下载一次支持在线更新的发行包，按 [README 安装说明](../../README.md#二进制模式安装-nxpanel)覆盖安装，保留原有数据和配置。
2. 安装完成后，由 `nxpanel-updater.service` 与原有面板服务协作。后续面板更新可以在网页内完成。

在线更新替换 API、Agent 两个二进制，前端和默认渲染模板随二进制更新。生效的 YAML、Nginx 配置、站点、证书和显式配置的自定义模板会保留；数据库仍按版本迁移。覆盖安装时，与新发行包不同的旧默认模板会保留到安装目录的 `templates-local`；审核后可移走该目录，启用新的默认模板。

### Docker 安装

1. 从旧版迁移时，拉取支持在线更新的镜像，并用原来的持久化挂载重建容器。不要删除数据卷。
2. 后续可在面板中更新。在线更新只写容器可写层，不需要 Docker Socket。`docker restart` 保留当前运行的面板版本；删除并重建容器则使用所选镜像自带的版本。
3. 重建前选择不低于当前面板版本的镜像。若镜像不支持数据库已应用的迁移，支持在线更新的镜像会拒绝启动面板，不会降库。更新 Nginx、OpenResty、s6 或系统依赖时，仍需更换镜像。

## 更新失败与恢复

1. **先等自动恢复。** 新版在试运行通过前不接受业务写入。试运行失败时，更新器恢复原来的程序版本和本次 SQLite 快照；站点服务不受此流程控制。
2. **再看日志。** 宿主机执行 `journalctl -u nxpanel-updater -u nxpanel-api -u nxpanel-agent`；Docker 执行 `docker logs nxpanel`（容器改过名时使用实际名称）。
3. **自动恢复失败时再人工救援。** 此时面板保持停止，不会反复用不兼容的版本打开数据库。先停止更新器及两个面板服务，保留安装目录的 `update-control`、`versions` 和完整数据卷，再使用经过验证且与数据库匹配的发行版和快照恢复。不要清空事务文件后直接启动未知版本。

### 宿主机下载报 `/tmp/nxpanel-update-*` 只读

这个错误发生在暂存之前，尚未切换版本，不需要回滚。旧版 API 的 systemd 沙箱将 `/tmp` 设为只读，应在服务已有的可写数据目录下配置私有临时目录，不要关闭 `ProtectSystem`。

1. 默认安装执行 `sudo -u openrest -g openrest -- install -d -m 0700 /opt/nxpanel/tmp`。如果安装时使用了 `--data-dir`，将这里和下方的 `/opt/nxpanel/tmp` 替换为实际数据目录下的 `tmp`。可用 `systemctl show -p ReadWritePaths nxpanel-api` 确认服务的可写路径。
2. 执行 `sudo systemctl edit nxpanel-api`，添加以下覆盖配置：

   ```ini
   [Service]
   Environment="TMPDIR=/opt/nxpanel/tmp"
   ```

3. 执行 `sudo systemctl restart nxpanel-api`，重新检查并下载更新。只会短暂中断面板，不需要重启站点服务。

修复后的完整安装包会创建此目录并配置服务；网页更新不会替换 systemd unit。若以后迁移数据目录，也要更新或删除手动添加的 `TMPDIR` 覆盖配置。

## 发布签名（维护者）

1. 在受控机器上生成 Ed25519 密钥：

   ```sh
   go run ./cmd/nxpanel-update-package -keygen /path/to/new-key-dir -key-id release-1
   ```

2. 将 `trust.pub` 的内容设为 GitHub 仓库变量 `NXPANEL_UPDATE_TRUST_KEYS`，将 `signing.key` 的内容设为受保护的 `release-signing` 环境机密 `NXPANEL_UPDATE_SIGNING_KEYS`。将环境限制到 `main` 并要求审核；私钥不提交到仓库，不进入镜像或普通 CI。
3. 按 [Release 流程](../../README.md#release-流程)发起发布。正式版使用 `vX.Y.Z`；预发布版使用 `vX.Y.Z-beta.N` 或 `vX.Y.Z-rc.N`，不会覆盖稳定镜像标签。发布保留完整安装包，另为两个 Linux 架构生成签名更新包。API 和更新器分别以编译内置公钥验证清单，并核对包长度和 SHA-256。

轮换密钥时，两个变量都可填写逗号分隔的 `key_id:base64` 项。先在基线内置后继公钥，再过渡双签。更新器本身不通过面板升级；旧基线未预置后继公钥时，退役旧密钥后需手动安装带新公钥的基线。

## 本地 Nginx 联调

只验证通道和发布说明，选 **宿主机检查**；要验证暂存、应用和回滚，选 **Docker 联调**。仅使用测试数据，不要把开发构建暴露到公网。

### 1. 准备测试源（两种方式共用）

在仓库根目录执行：

```sh
make update-fixture
```

测试索引和签名包生成在 `tmp/update-fixture/feed`，测试公钥在 `tmp/update-fixture/trust.pub`。测试私钥在脚本退出时删除。正常索引 `index.json` 同时包含稳定版和预览版；`corrupt.json` 指向损坏的预览更新包。

### 2A. 宿主机检查：只验证通道和发布说明

1. 用现有 Nginx 托管整个 `feed` 目录，或在一个终端运行：

   ```sh
   docker run --rm --name nxpanel-update-feed \
     -p 127.0.0.1:18080:80 \
     --mount "type=bind,source=$PWD/tmp/update-fixture/feed,target=/usr/share/nginx/html,readonly" \
     nginx:alpine
   ```

2. 在另一个终端运行 `curl -fsS http://127.0.0.1:18080/index.json`，确认返回索引。按 [源码构建说明](../../README.md#从源码构建)准备 `configs/config.yaml`，确认 `upgrade.enabled: true`，并另开终端启动 Agent。
3. 在仓库根目录启动开发 API：

   ```sh
   NXPANEL_UPDATE_TEST_FEED=http://127.0.0.1:18080/index.json \
   NXPANEL_UPDATE_TEST_KEYS="$(cat tmp/update-fixture/trust.pub)" \
   make run-api BUILD_TAGS=update_dev VERSION=v0.0.1
   ```

   `NXPANEL_UPDATE_TEST_FEED` 必须是 **API 能访问的完整索引 URL**，不是 Nginx 根目录、面板登录地址或 YAML 配置项。测试源支持 HTTP 和 HTTPS，重定向不能跨源。若索引挂在 `/nxpanel-test/index.json`，版本目录也必须在 `/nxpanel-test/` 下。`NXPANEL_UPDATE_TEST_KEYS` 填公钥内容，不是文件路径。这两个变量只在 `update_dev` 构建中生效，修改后需重启 API。

4. 登录并切换通道：稳定版应显示 `v0.0.2` 及 `Fixture: stable update`；预览版应显示 `v0.0.3-rc.1` 及 `Fixture: preview update`。源码开发模式未接入更新器，所以不会出现 **立即更新**。

### 2B. Docker 联调：验证暂存、应用和回滚

先构建一次性开发镜像，两个构建参数都不能省略：

```sh
docker build -f docker/Dockerfile.nginx --build-arg VERSION=v0.0.1 \
  --build-arg UPDATE_BUILD_TAGS=embed_web,update_dev -t nxpanel:update-dev .
```

**推荐：自动联调。** 执行 `bash scripts/test-web-update-docker.sh nxpanel:update-dev`。脚本自行启动测试源和面板容器，无需另设 URL。预期结果：暂存 `v0.0.2` 后重启容器，版本不切换；显式应用后才切换到 `v0.0.2`；模拟 `v0.0.3-rc.1` 启动失败时，恢复到 `v0.0.2`。

**可选：手动联调。** 按顺序启动静态 Nginx 和面板容器。两者共用一个 Docker 网络，面板使用 Nginx 容器名访问测试源；容器内的 `127.0.0.1` 指向容器自己。

```sh
docker network create nxpanel-update-local
docker run -d --rm --name nxpanel-update-feed-local --network nxpanel-update-local \
  --mount "type=bind,source=$PWD/tmp/update-fixture/feed,target=/usr/share/nginx/html,readonly" \
  nginx:alpine

docker run --rm --network nxpanel-update-local -p 127.0.0.1:18888:18888 \
  -e NXPANEL_API_LOGIN_PATH=/nx-update-local \
  -e NXPANEL_UPDATE_TEST_FEED=http://nxpanel-update-feed-local/index.json \
  -e "NXPANEL_UPDATE_TEST_KEYS=$(cat tmp/update-fixture/trust.pub)" \
  -e "NXPANEL_AGENT_TOKEN=$(openssl rand -hex 32)" \
  nxpanel:update-dev
```

1. 打开 `https://127.0.0.1:18888/nx-update-local` 初始化面板。切换通道，先核对版本和说明，再按 **立即更新 → 重启并应用** 测试更新。`-e` 同时为 API 和内置更新器传入测试公钥。
2. 要验证下载校验失败，启动面板容器前将索引 URL 末尾改为 `corrupt.json`，登录后选择预览版并点击 **立即更新**。要验证应用后自动回滚，继续使用 `index.json`，启动时额外传入 `-e NXPANEL_UPDATE_TEST_FAIL_VERSION=v0.0.3-rc.1`，再暂存并应用预览版。
