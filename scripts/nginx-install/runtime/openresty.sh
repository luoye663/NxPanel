# ============================================================
# runtime/openresty.sh — OpenResty 官方仓库 runtime 配置
#
# 职责（runtime 文件允许的发行版感知）：
#   - 声明支持矩阵（哪些 distro:version 提供 openresty.org 仓库）
#   - 声明 flag-gated 矩阵（需要 --insecure-relax-sha1 opt-in 才支持）
#   - 提供 per-distro 的 URL / keyring / repo 内容数据
#   - 不执行 apt/dnf/yum/systemctl（那些是 dist/ 与 lib/ 的工作）
#
# 依赖：lib/log.sh、lib/matrix.sh、lib/os.sh、lib/command.sh
# ============================================================

RUNTIME_ID="openresty"
RUNTIME_PACKAGE="openresty"
RUNTIME_SERVICE="openresty"
RUNTIME_BINARY_CANDIDATES=(
    "openresty"
    "/usr/local/openresty/nginx/sbin/nginx"
    "/usr/local/openresty/bin/openresty"
)
RUNTIME_CONF_TEMPLATE="nginx.conf.openresty"
RUNTIME_CUSTOM_SERVICE_FILE="openresty.service"

# openresty.org 官方仓库默认支持矩阵（聚焦发行版）
RUNTIME_SUPPORTED_MATRIX="
debian:12
ubuntu:22 ubuntu:24 ubuntu:26
almalinux:8 almalinux:9
alinux:3
rocky:9 rockylinux:9
centos:7
ol:8 ol:9
opencloudos:9
"

# 需要 --insecure-relax-sha1 opt-in 才支持的组合
# OpenResty 仓库 key (97DB7443D5EDEB74) 的 binding signature 使用 SHA1，
# 被 Sequoia (apt-sqv) 默认拒绝；用户显式 opt-in 后由 dist adapter 临时放宽策略。
RUNTIME_FLAG_GATED_MATRIX="debian:13"

# runtime_validate_support
#   1. 命中 RUNTIME_SUPPORTED_MATRIX → 直接通过
#   2. 命中 RUNTIME_FLAG_GATED_MATRIX → 看 INSECURE_RELAX_SHA1，未传则 die 带详细说明
#   3. 都未命中 → die 不支持
runtime_validate_support() {
    if matrix_contains "$RUNTIME_SUPPORTED_MATRIX" "${OS_ID}:${OS_VERSION_ID_MAJOR}"; then
        return 0
    fi

    if matrix_contains "$RUNTIME_FLAG_GATED_MATRIX" "${OS_ID}:${OS_VERSION_ID_MAJOR}"; then
        if [ "${INSECURE_RELAX_SHA1:-false}" = "true" ]; then
            return 0
        fi
        die "Debian 13 + OpenResty 需要 --insecure-relax-sha1 标志

原因：OpenResty 官方仓库的 GPG key (97DB7443D5EDEB74) binding signature 使用 SHA1，
      Sequoia (apt-sqv) 在 Debian 13 默认拒绝该 key。
      详见 https://github.com/openresty/openresty/issues/1072

如果你接受以下权衡，请加 --insecure-relax-sha1 重试：
  - 安装过程会临时写 /etc/crypto-policies/back-ends/apt-sequoia.config，
    把 apt-sqv 的 SHA1 接受截止延后到 2027-02-01
  - 该放宽作用域为全系统（不止 OpenResty 仓库）
  - 安装完成（或失败）后自动清理该文件
  - 后续 apt upgrade openresty 仍会再次失败，需重新跑本脚本或手动应用 workaround
  - OpenResty 官方迁移到 pubkey2.gpg 签 InRelease 后，本 opt-in 将不再需要"
    fi

    die "openresty 官方仓库不支持 ${OS_ID} ${OS_VERSION_ID}"
}

# runtime_get_repo_key_url
#   仓库 InRelease 实际由 pubkey.gpg 的 key (97DB7443D5EDEB74) 签名，
#   故所有发行版统一用 pubkey.gpg；Debian 13 通过 --insecure-relax-sha1
#   让 Sequoia 接受该 key 的 SHA1 binding signature。
runtime_get_repo_key_url() {
    echo "https://openresty.org/package/pubkey.gpg"
}

# runtime_get_keyring_path
runtime_get_keyring_path() {
    echo "/usr/share/keyrings/openresty.gpg"
}

# runtime_get_apt_repo_base
#   URL 本身 per-distro，故保留 case；这不是支持判断。
runtime_get_apt_repo_base() {
    case "$OS_ID" in
        ubuntu) echo "https://openresty.org/package/ubuntu" ;;
        debian) echo "https://openresty.org/package/debian" ;;
        *)      die "openresty apt 仓库不支持 $OS_ID" ;;
    esac
}

# runtime_write_apt_repo <repo_base> <codename> <keyring>
#   Debian 走 openresty 组件，Ubuntu 走 main 组件；统一 signed-by。
runtime_write_apt_repo() {
    local repo_base="$1"
    local codename="$2"
    local keyring="$3"

    local component="main"
    [ "$OS_ID" = "debian" ] && component="openresty"

    echo "deb [signed-by=${keyring}] ${repo_base} ${codename} ${component}" \
        > /etc/apt/sources.list.d/openresty.list
}

# runtime_write_apt_preferences
#   OpenResty 不需要 priorities（官方仓库优先级已足够）。
runtime_write_apt_preferences() {
    :
}

# runtime_get_openresty_yum_repo_url
#   per-distro:version 显式选择 .repo 文件：
#     - el7（centos:7）→ centos/openresty.repo（pubkey.gpg）
#     - el8 family     → centos/openresty.repo（pubkey.gpg）；ol:8 用 oracle/openresty.repo；alinux:3 用 alinux/openresty.repo
#     - el9 family     → centos/openresty2.repo（pubkey2.gpg，所有 el9 发行版共用）
#   说明：openresty.org 未发布 oracle/openresty2.repo 等专用 el9 repo；
#         centos/openresty2.repo 含 el9 二进制且 binary 兼容所有 el9 发行版。
runtime_get_openresty_yum_repo_url() {
    case "${OS_ID}:${OS_VERSION_ID_MAJOR}" in
        # el7
        centos:7)
            echo "https://openresty.org/package/centos/openresty.repo"
            ;;
        # el8 family
        centos:8|almalinux:8|rocky:8|rockylinux:8)
            echo "https://openresty.org/package/centos/openresty.repo"
            ;;
        ol:8)
            echo "https://openresty.org/package/oracle/openresty.repo"
            ;;
        alinux:3)
            echo "https://openresty.org/package/alinux/openresty.repo"
            ;;
        # el9 family（无 per-distro el9 repo，全部借用 centos/openresty2.repo）
        centos:9|almalinux:9|rocky:9|rockylinux:9|ol:9|opencloudos:9)
            echo "https://openresty.org/package/centos/openresty2.repo"
            ;;
        *)
            die "openresty yum 仓库不支持 ${OS_ID} ${OS_VERSION_ID}"
            ;;
    esac
}

# runtime_write_yum_repo
#   OpenResty 官方维护 .repo 文件（含 $releasever/$basearch），直接下载。
runtime_write_yum_repo() {
    local repo_url
    repo_url=$(runtime_get_openresty_yum_repo_url)
    log_info "OpenResty 官方仓库文件: $repo_url"
    download_to "$repo_url" /etc/yum.repos.d/openresty.repo
}
