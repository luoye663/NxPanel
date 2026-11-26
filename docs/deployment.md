# 部署、构建与发布

## 宿主机

- [`install.sh`](../install.sh) 支持从发布包下载、本地压缩包或已解压目录安装。覆盖安装先停止相关面板服务，保留持久化数据及用户配置；对已有 Nginx/OpenResty 默认不接管主配置。API systemd 单元以非 root 用户运行，Agent 以 root 运行；在线更新器由独立单元管理。
- [`scripts/nginx-install`](../scripts/nginx-install/) 将发行版包管理与 Nginx/OpenResty 运行时适配分开。修改兼容逻辑时，以脚本中的支持矩阵为准，并保持配置模板的用户、PID、面板目录、错误日志替换、OpenResty 相对 include 及 systemd `PIDFile` 一致，最终用目标运行时执行 `nginx -t`。
- 保留已有兼容措施：Debian 13 + OpenResty 的 SHA-1 签名策略放宽仅允许显式 opt-in，退出须清理；CentOS 7 仅在检测到旧 mirrorlist 时切换 vault；OpenCloudOS 9 的 dnf 安装保留 `--nobest` 的可安装版本回退。

## Docker 独享模式

- [`docker-compose.yml`](../docker-compose.yml) 的 `nginx`、`openresty` profiles 二选一。设置强随机 `NXPANEL_AGENT_TOKEN` 并保护 `.env`。默认使用 bridge 端口映射；[`docker-compose.host.yml`](../docker-compose.host.yml) 在 Linux host 网络下清空映射，容器直接占用宿主机端口，与宿主机直装是两种部署方式。
- 持久化四类目录：`data`（SQLite、面板配置、TLS 和运行日志）、`nginx`（主配置、站点配置和备份）、`wwwroot`（站点文件）、`wwwlogs`（站点日志）。[`docker/cont-init.d/nxpanel-init`](../docker/cont-init.d/nxpanel-init) 首次复制 YAML 和渲染 nginx.conf，后续保留配置；既有旧错误日志指令迁移会先备份，`mime.types` 每次同步供相对 include 使用。只读挂载会限制对应的配置编辑与回写功能。
- s6 按各自身份管理 Nginx、Agent、API 与更新组件；Agent Socket 由组权限桥接。两个镜像用各自的运行时路径、worker 用户和配置模板，不能假定 `.so`、PID、include 或配置路径通用。重建容器不会保留在线安装的模块和容器中的面板更新槽；分别参见[模块商店](nginx-module-store-usage.md)和[面板在线更新](function/web-update.md)。

## 配置与验证

- [`configs/config.example.yaml`](../configs/config.example.yaml) 是部署示例，实际加载优先级为受支持的 `NXPANEL_*` 环境变量、YAML、程序默认值。进程入口和开发代理见[架构与进程边界](architecture.md)。面板默认启用 TLS，未指定证书时生成并更新自签名证书；CN 使用随机 UUID v4，SAN 仅包含 `localhost`、loopback 和显式配置的额外项，不自动采集主机地址。访问入口从受控启动日志查看，不对外分享含 gate 的日志。
- 安装改动按范围运行 `make test-install-compat`（运行时安装）或 `make test-install-compat-full`（完整发行包与 systemd 容器）；选择受影响的发行版和 Nginx/OpenResty。覆盖安装至少确认服务停止／启动顺序、持久化配置和数据、PID/include 以及目标 `nginx -t`。镜像改动检查受影响的 Docker 变体首次启动、重启保留与端口／权限。

## 发布与信任链

- 工具链、构建入口和目标以 [`go.mod`](../go.mod)、[`webapp-react/package.json`](../webapp-react/package.json)、[`Makefile`](../Makefile) 及 [CI 工作流](../.github/workflows/ci.yml) 为准。`make all` 联合构建 React 和二进制；`make release` 本地打包，不发布 GitHub Release。
- [Release 工作流](../.github/workflows/release.yml) 从 `main` 手动触发，要求目标提交通过 `main` CI，构建安装包和签名在线更新资产，并按启用的仓库发布双变体多架构镜像。预览版不覆盖稳定镜像别名。验收应核对 Release 资产，并在相应通道试跑下载、验签、暂存、切换和失败回滚。
- 面板更新和模块发布共享离线根信任，以授权的发布键签名，不绕过签名及运行时兼容性核验；根私钥不得用于日常发布或存入仓库。在线更新只切换 API 与 Agent，不替换更新器、Nginx 或基础镜像。模块服务的构建、签名和试载见[模块服务部署](nginx-module-store-deployment.md)及[模块发布与联调](function/nginx-modules.md)。
