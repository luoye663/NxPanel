# ============================================================
# lib/os.sh — 操作系统、包管理器、systemd、架构检测
#
# 提供：
#   detect_os              解析 /etc/os-release
#   detect_pkg_manager     根据 OS_ID/ID_LIKE 推断 apt/dnf
#   detect_systemd         判断 systemd 是否可用
#   detect_arch            输出 basearch（x86_64/aarch64/...）
#   resolve_web_user_default  运行时探测 www-data/nginx/nobody
#
# 依赖：lib/log.sh
# ============================================================

# detect_os
#   读取 /etc/os-release，设置全局变量：
#     OS_ID          小写 ID
#     OS_VERSION_ID  主版本号字符串（如 "12"、"24.04"、"9"）
#     OS_VERSION_ID_MAJOR  主版本号整数部分（如 "12"、"24"、"9"）
#     OS_CODENAME    VERSION_CODENAME（可能为空）
#     OS_LIKE        ID_LIKE（可能为空）
detect_os() {
    if [ ! -f /etc/os-release ]; then
        die "无法检测操作系统（缺少 /etc/os-release）"
    fi

    # shellcheck disable=SC1091
    . /etc/os-release

    OS_ID="${ID:-}"
    OS_VERSION_ID="${VERSION_ID:-}"
    OS_CODENAME="${VERSION_CODENAME:-}"
    OS_LIKE="${ID_LIKE:-}"

    # 主版本号（去 . 之后）
    OS_VERSION_ID_MAJOR="${OS_VERSION_ID%%.*}"

    # 统一小写（部分发行版 ID 大小写不一致）
    OS_ID="$(echo "$OS_ID" | tr '[:upper:]' '[:lower:]')"
    OS_LIKE="$(echo "$OS_LIKE" | tr '[:upper:]' '[:lower:]')"

    detect_pkg_manager

    log_info "检测到操作系统: $OS_ID $OS_VERSION_ID (包管理器: $PKG_MANAGER)"
}

# detect_pkg_manager
#   仅根据 OS_ID/OS_LIKE 决定 PKG_MANAGER：apt / dnf / yum
#   聚焦发行版：
#     debian、ubuntu → apt
#     almalinux、rockylinux、ol、opencloudos → dnf
#     centos:7 → yum（CentOS 7 无 dnf；CentOS 8 已 EOL 但理论上有 dnf）
#   其他 ID_LIKE 走兜底推断，但 dist adapter 仍会拒绝不支持的发行版。
detect_pkg_manager() {
    case "$OS_ID" in
        ubuntu|debian)
            PKG_MANAGER="apt"
            ;;
        centos)
            if [ "$OS_VERSION_ID_MAJOR" = "7" ]; then
                PKG_MANAGER="yum"
            else
                PKG_MANAGER="dnf"
            fi
            ;;
        almalinux|rocky|rockylinux|rhel|fedora|amzn|amazon|ol)
            PKG_MANAGER="dnf"
            ;;
        opencloudos)
            # ID_LIKE=opencloudos 不含 rhel/fedora，必须显式列出
            PKG_MANAGER="dnf"
            ;;
        *)
            if echo "$OS_LIKE" | grep -qi "debian\|ubuntu"; then
                PKG_MANAGER="apt"
            elif echo "$OS_LIKE" | grep -qi "rhel\|centos\|fedora"; then
                PKG_MANAGER="dnf"
            else
                die "不支持的操作系统: $OS_ID ($OS_VERSION_ID)"
            fi
            ;;
    esac
}

# detect_systemd
#   设置 SYSTEMD_AVAILABLE=true/false
detect_systemd() {
    if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
        SYSTEMD_AVAILABLE=true
    else
        SYSTEMD_AVAILABLE=false
    fi
}

# detect_arch
#   输出 basearch：uname -m（与 RPM 约定一致）
detect_arch() {
    uname -m
}

# resolve_web_user_default
#   运行时探测可用的 web 用户：优先 www-data，其次 nginx，最后 nobody。
#   全部缺失时返回空字符串（调用方需回退到 PKG_MANAGER 默认值）。
resolve_web_user_default() {
    if getent passwd www-data >/dev/null 2>&1; then
        echo "www-data"
        return
    fi
    if getent passwd nginx >/dev/null 2>&1; then
        echo "nginx"
        return
    fi
    if getent passwd nobody >/dev/null 2>&1; then
        echo "nobody"
        return
    fi
    echo ""
}

# check_root
check_root() {
    if [ "${EUID:-$(id -u)}" -ne 0 ]; then
        die "请使用 root 权限运行此脚本"
    fi
}
