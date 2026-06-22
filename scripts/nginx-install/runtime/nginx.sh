# ============================================================
# runtime/nginx.sh — Nginx 官方仓库 runtime 配置
#
# 职责（runtime 文件允许的发行版感知）：
#   - 声明支持矩阵（哪些 distro:version 提供 nginx.org 仓库）
#   - 提供 per-distro 的 URL / keyring / repo 内容数据
#   - 不执行 apt/dnf/yum/systemctl（那些是 dist/ 与 lib/ 的工作）
#
# 依赖：lib/log.sh、lib/matrix.sh、lib/os.sh、lib/command.sh
# ============================================================

RUNTIME_ID="nginx"
RUNTIME_PACKAGE="nginx"
RUNTIME_SERVICE="nginx"
RUNTIME_BINARY_CANDIDATES=("nginx" "/usr/sbin/nginx" "/usr/local/nginx/sbin/nginx")
RUNTIME_CONF_TEMPLATE="nginx.conf.production"
RUNTIME_CUSTOM_SERVICE_FILE="nginx.service"

# nginx.org 官方仓库支持矩阵（聚焦发行版）
RUNTIME_SUPPORTED_MATRIX="
debian:12 debian:13
ubuntu:22 ubuntu:24 ubuntu:26
almalinux:8 almalinux:9
alinux:3
rocky:9 rockylinux:9
centos:7
ol:8 ol:9
opencloudos:9
"

# runtime_validate_support
#   通过 matrix_contains 判断，命中返回 0；未命中 die。
runtime_validate_support() {
    matrix_contains "$RUNTIME_SUPPORTED_MATRIX" "${OS_ID}:${OS_VERSION_ID_MAJOR}" \
        || die "nginx 官方仓库不支持 ${OS_ID} ${OS_VERSION_ID}"
}

# runtime_get_repo_key_url
runtime_get_repo_key_url() {
    echo "https://nginx.org/keys/nginx_signing.key"
}

# runtime_get_keyring_path
runtime_get_keyring_path() {
    echo "/usr/share/keyrings/nginx-archive-keyring.gpg"
}

# runtime_get_apt_repo_base
#   nginx.org apt 仓库 base URL，按 OS_ID 区分 ubuntu/debian。
#   （URL 本身 per-distro，故保留 case；这不是支持判断）
runtime_get_apt_repo_base() {
    case "$OS_ID" in
        ubuntu) echo "https://nginx.org/packages/ubuntu" ;;
        debian) echo "https://nginx.org/packages/debian" ;;
        *)      die "nginx apt 仓库不支持 $OS_ID" ;;
    esac
}

# runtime_write_apt_repo <repo_base> <codename> <keyring>
runtime_write_apt_repo() {
    local repo_base="$1"
    local codename="$2"
    local keyring="$3"

    echo "deb [signed-by=${keyring}] ${repo_base} ${codename} nginx" \
        > /etc/apt/sources.list.d/nginx.list
}

# runtime_write_apt_preferences
#   nginx 设置高优先级，避免被发行版仓库覆盖。
runtime_write_apt_preferences() {
    cat > /etc/apt/preferences.d/99nginx <<'EOF'
Package: nginx*
Pin: origin nginx.org
Pin: release o=nginx
Pin-Priority: 900
EOF
}

# runtime_get_yum_repo_base
#   nginx.org yum 仓库 base URL；centos/almalinux/alinux/rocky/ol/opencloudos 共用基础路径。
#   其他发行版（rhel 等）不在 dist adapter 白名单，已被 dist_validate_support 拒绝。
runtime_get_yum_repo_base() {
    case "$OS_ID" in
        almalinux|alinux|rocky|rockylinux|centos|ol|opencloudos)
            echo "https://nginx.org/packages/centos"
            ;;
        *)
            die "nginx yum 仓库不支持 $OS_ID"
            ;;
    esac
}

# runtime_write_yum_repo
#   按当前 releasever 写 nginx-stable 仓库（mainline 关闭）。
runtime_write_yum_repo() {
    local repo_base ver
    repo_base=$(runtime_get_yum_repo_base)
    ver=$(resolve_releasever "$repo_base" "$OS_VERSION_ID")

    cat > "/etc/yum.repos.d/nginx.repo" <<EOF
[nginx-stable]
name=nginx stable repo
baseurl=${repo_base}/${ver}/\$basearch/
gpgcheck=1
enabled=1
gpgkey=https://nginx.org/keys/nginx_signing.key
module_hotfixes=true
EOF
}
