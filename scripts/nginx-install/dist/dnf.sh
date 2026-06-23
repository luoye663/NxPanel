# ============================================================
# dist/dnf.sh — AlmaLinux / Rocky / RHEL / CentOS 系 dnf 适配器
#
# 聚焦发行版白名单：
#   almalinux:8、almalinux:9、alinux:3、rockylinux:9（容器内 OS_ID=rocky）
#
# 实现 5 个 adapter 接口函数 + PKG_MANAGER 变量。
# repo/key 数据来自 runtime/<id>.sh，dnf adapter 负责落盘。
#
# 依赖：lib/log.sh、lib/os.sh、lib/command.sh、lib/repo.sh、runtime/<id>.sh
# ============================================================

PKG_MANAGER="dnf"

# dist_validate_support
#   用主版本号匹配，避免 OS 报告带小版本（如 rocky 9.3）时漏判。
dist_validate_support() {
    case "${OS_ID}:${OS_VERSION_ID_MAJOR}" in
        almalinux:8|almalinux:9|alinux:3|rocky:9|rockylinux:9|ol:8|ol:9|opencloudos:9)
            return 0
            ;;
        *)
            die "dnf 适配器不支持 ${OS_ID} ${OS_VERSION_ID}（聚焦：almalinux:8/9、alinux:3、rockylinux:9、oracle linux:8/9、opencloudos:9）"
            ;;
    esac
}

# dist_install_prereqs
#   el8/el9 用 curl-minimal 减少依赖；ca-certificates、gnupg2 是基础。
#   --allowerasing：处理系统已装 curl 而我们装 curl-minimal 时的冲突（oracle/opencloudos 等预装 curl 全包）。
dist_install_prereqs() {
    log_step "安装前置依赖 (dnf)..."

    if [ "$OS_VERSION_ID_MAJOR" -ge 9 ]; then
        dnf install -y --allowerasing ca-certificates curl-minimal gnupg2
    else
        dnf install -y ca-certificates curl gnupg2
    fi
}

# dist_add_repository
#   runtime 决定 repo 文件内容（nginx 内联生成 / openresty 下载官方 .repo）。
dist_add_repository() {
    log_step "添加 ${RUNTIME_ID} 官方 YUM 仓库..."
    runtime_write_yum_repo
}

# dist_refresh_cache
dist_refresh_cache() {
    dnf makecache
}

# dist_install_packages <pkg...>
#   --nobest：当最新版有未满足依赖时回退到可安装的旧版本。
#   例：OpenCloudOS 9 ships OpenSSL 3.0.x，最新 nginx 需要 OpenSSL 3.2.0+ ABI；
#       --nobest 让 dnf 自动选可安装的较旧版本（如 1.28.x），避免硬失败。
dist_install_packages() {
    dnf install -y --nobest "$@"
}
