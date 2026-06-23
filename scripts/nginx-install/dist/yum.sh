# ============================================================
# dist/yum.sh — CentOS 7 等 yum-only 发行版的包安装适配器
#
# 聚焦发行版：centos:7（其他 yum 发行版若有需要再扩展）
#
# CentOS 7 已于 2024-06-30 EOL，官方 mirrorlist.centos.org 已下线。
# 本 adapter 在 dist_install_prereqs 阶段自动检测并把 CentOS-Base.repo
# 重写为 vault.centos.org/7.9.2009/，确保 yum 能正常工作。
#
# 依赖：lib/log.sh、lib/os.sh、lib/command.sh、lib/repo.sh、runtime/<id>.sh
# ============================================================

PKG_MANAGER="yum"

# dist_validate_support
#   仅 centos:7；其他 yum 发行版暂未列入聚焦矩阵。
dist_validate_support() {
    case "${OS_ID}:${OS_VERSION_ID_MAJOR}" in
        centos:7)
            return 0
            ;;
        *)
            die "yum 适配器不支持 ${OS_ID} ${OS_VERSION_ID}（聚焦：centos:7）"
            ;;
    esac
}

# _fix_centos7_eol_base_repo
#   CentOS 7 已 EOL，官方 mirrorlist.centos.org 关闭。检测到该情况时
#   把 /etc/yum.repos.d/CentOS-Base.repo 重写为 vault.centos.org/7.9.2009/。
#   仅当文件存在且仍指向 mirrorlist.centos.org 时才重写，尊重用户自定义。
_fix_centos7_eol_base_repo() {
    if [ "$OS_ID" != "centos" ] || [ "$OS_VERSION_ID_MAJOR" != "7" ]; then
        return 0
    fi

    local base_repo="/etc/yum.repos.d/CentOS-Base.repo"
    if [ ! -f "$base_repo" ]; then
        return 0
    fi

    # 仅在文件还含 mirrorlist.centos.org 时才重写（用户已改过则不动）
    if ! grep -q "mirrorlist.centos.org" "$base_repo" 2>/dev/null; then
        return 0
    fi

    log_warn "检测到 CentOS 7 EOL（mirrorlist.centos.org 已下线），自动切换到 vault.centos.org"

    # 备份原文件
    cp "$base_repo" "${base_repo}.bak.$(date +%Y%m%d%H%M%S)"

    cat > "$base_repo" <<'EOF'
# CentOS-Base.repo - 重写为 vault.centos.org（CentOS 7 EOL by nxpanel installer）
[base]
name=CentOS-$releasever - Base
baseurl=https://vault.centos.org/7.9.2009/os/$basearch/
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7

[updates]
name=CentOS-$releasever - Updates
baseurl=https://vault.centos.org/7.9.2009/updates/$basearch/
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7

[extras]
name=CentOS-$releasever - Extras
baseurl=https://vault.centos.org/7.9.2009/extras/$basearch/
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7

[centosplus]
name=CentOS-$releasever - Plus
baseurl=https://vault.centos.org/7.9.2009/centosplus/$basearch/
gpgcheck=1
enabled=0
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7
EOF

    yum clean all >/dev/null 2>&1 || true
    log_info "已重写 $base_repo → vault.centos.org/7.9.2009/"
}

# dist_install_prereqs
#   先修 EOL base repo，再装基础依赖。
dist_install_prereqs() {
    log_step "安装前置依赖 (yum)..."
    _fix_centos7_eol_base_repo
    yum install -y ca-certificates curl gnupg2
}

# dist_add_repository
#   runtime 决定 repo 文件内容（nginx 内联生成 / openresty 下载官方 .repo）。
dist_add_repository() {
    log_step "添加 ${RUNTIME_ID} 官方 YUM 仓库..."
    runtime_write_yum_repo
}

# dist_refresh_cache
dist_refresh_cache() {
    yum makecache fast 2>/dev/null || yum makecache
}

# dist_install_packages <pkg...>
dist_install_packages() {
    yum install -y "$@"
}
