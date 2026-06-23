# ============================================================
# dist/apt.sh — Debian / Ubuntu 包安装适配器
#
# 聚焦发行版白名单（与 dist_validate_support 一致）：
#   debian:12、debian:13、ubuntu:22.04、ubuntu:24.04、ubuntu:26.04
#
# 实现 5 个 adapter 接口函数 + PKG_MANAGER 变量。
# repo/key 数据来自 runtime/<id>.sh，apt adapter 负责落盘。
#
# 依赖：lib/log.sh、lib/os.sh、lib/command.sh、lib/repo.sh、runtime/<id>.sh
# ============================================================

PKG_MANAGER="apt"

# dist_validate_support
#   用主版本号匹配，避免 OS 报告带小版本（如 24.04.1）时漏判。
dist_validate_support() {
    case "${OS_ID}:${OS_VERSION_ID_MAJOR}" in
        debian:12|debian:13|ubuntu:22|ubuntu:24|ubuntu:26)
            return 0
            ;;
        *)
            die "apt 适配器不支持 ${OS_ID} ${OS_VERSION_ID}（聚焦：debian:12/13、ubuntu:22.04/24.04/26.04）"
            ;;
    esac
}

# dist_install_prereqs
#   安装基础依赖；清理上次运行残留的 nginx.list / openresty.list（仅脚本自身创建的）。
dist_install_prereqs() {
    log_step "安装前置依赖 (apt)..."

    rm -f /etc/apt/sources.list.d/nginx.list
    rm -f /etc/apt/sources.list.d/openresty.list

    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
        ca-certificates curl gnupg lsb-release
}

# dist_add_repository
#   通过 runtime_* 拿 key URL / keyring 路径 / repo base；通过 resolve_codename 选 codename。
dist_add_repository() {
    log_step "添加 ${RUNTIME_ID} 官方 APT 仓库..."

    local keyring key_url repo_base codename
    keyring=$(runtime_get_keyring_path)
    key_url=$(runtime_get_repo_key_url)
    repo_base=$(runtime_get_apt_repo_base)
    codename=$(resolve_codename "$repo_base")

    install -d -m 0755 "$(dirname "$keyring")"
    if [ ! -f "$keyring" ]; then
        download_to_pipe "$key_url" | gpg --dearmor -o "$keyring" 2>/dev/null
        chmod 644 "$keyring"
    fi

    # runtime 决定 repo 行写法（signed-by / trusted 策略 + 组件名）
    runtime_write_apt_repo "$repo_base" "$codename" "$keyring"

    # runtime 决定是否写 priorities（nginx 写 99nginx；openresty no-op）
    runtime_write_apt_preferences

    # Debian 13 + OpenResty + 用户 opt-in：临时放宽 apt-sqv SHA1 策略
    _maybe_apply_sha1_relax
}

# _cleanup_sha1_relax_policy
#   清理 _maybe_apply_sha1_relax 写的临时策略文件；best-effort 刷新 apt 缓存。
#   通过 register_cleanup_hook 在 EXIT/INT/TERM 时调用。
_cleanup_sha1_relax_policy() {
    local policy_file=/etc/crypto-policies/back-ends/apt-sequoia.config
    if [ -f "$policy_file" ]; then
        rm -f "$policy_file"
        log_info "已清理临时放宽的 SHA1 策略文件: $policy_file"
        # 刷新 apt 缓存：OpenResty repo 在策略恢复后会再次报 SHA1 警告，属预期
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq 2>/dev/null || \
                log_warn "恢复策略后 apt-get update 出现警告（OpenResty repo 报 SHA1 属预期）"
        fi
    fi
}

# _maybe_apply_sha1_relax
#   仅 Debian 13 + OpenResty + INSECURE_RELAX_SHA1=true 时生效。
#   写 /etc/crypto-policies/back-ends/apt-sequoia.config，注册清理钩子。
_maybe_apply_sha1_relax() {
    if [ "${INSECURE_RELAX_SHA1:-false}" != "true" ]; then
        return 0
    fi
    # 严格限定触发条件：避免误用
    if [ "$OS_ID" != "debian" ] || [ "$OS_VERSION_ID_MAJOR" -lt 13 ]; then
        return 0
    fi
    if [ "$RUNTIME_ID" != "openresty" ]; then
        return 0
    fi

    local policy_file=/etc/crypto-policies/back-ends/apt-sequoia.config

    log_warn "=============================================================="
    log_warn "  ⚠ 应用 SHA1 策略临时放宽（apt-sqv）"
    log_warn "--------------------------------------------------------------"
    log_warn "  原因：OpenResty 官方 key 的 binding signature 使用 SHA1，"
    log_warn "        Sequoia (apt-sqv) 在 Debian 13 默认拒绝。"
    log_warn "  操作：写 $policy_file"
    log_warn "        将 SHA1 second_preimage_resistance 截止延后到 2027-02-01。"
    log_warn "  ⚠ 该放宽作用于全系统 apt 仓库（不止 OpenResty）。"
    log_warn "  ⚠ 安装完成（或失败）后自动清理该文件。"
    log_warn "  ⚠ 后续 apt upgrade openresty 仍会再次失败，需重新跑本脚本。"
    log_warn "=============================================================="

    mkdir -p /etc/crypto-policies/back-ends
    cat > "$policy_file" <<'EOF'
[hash_algorithms]
sha1.second_preimage_resistance = 2027-02-01
EOF

    register_cleanup_hook _cleanup_sha1_relax_policy
}

# dist_refresh_cache
dist_refresh_cache() {
    apt-get update -qq
}

# dist_install_packages <pkg...>
dist_install_packages() {
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
}
