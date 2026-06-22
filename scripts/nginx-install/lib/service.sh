# ============================================================
# lib/service.sh — systemd 服务管理
#
# 提供：
#   service_enable <name>
#   service_start <name>
#   service_status <name>
#   deploy_service_file <src_file> <dst_name> <pid_path>
#
# 依赖：lib/log.sh、lib/os.sh（SYSTEMD_AVAILABLE）
# ============================================================

# service_enable <name>
#   非 systemd 时警告跳过；enable 失败容忍（systemctl 返回非零时仅 warn）。
service_enable() {
    local name="$1"

    if [ "$SYSTEMD_AVAILABLE" != "true" ]; then
        log_warn "systemd 不可用（容器/WSL 环境？），跳过 enable $name"
        return 0
    fi

    if systemctl enable "$name" >/dev/null 2>&1; then
        log_info "已启用 $name 服务（未启动，由面板管理）"
    else
        log_warn "systemctl enable $name 失败（可能服务单元未加载）"
    fi
}

# service_start <name>
#   非本地（如 install.sh 在容器内运行测试）场景下不强制启动，
#   这里只做 systemd 启动；失败不 die（与原脚本"enable 不 start"的语义对齐）。
service_start() {
    local name="$1"

    if [ "$SYSTEMD_AVAILABLE" != "true" ]; then
        log_warn "systemd 不可用，跳过 start $name"
        return 0
    fi

    if systemctl start "$name" >/dev/null 2>&1; then
        log_info "已启动 $name 服务"
    else
        log_warn "systemctl start $name 失败（请手动检查）"
    fi
}

# service_status <name>
service_status() {
    local name="$1"

    if [ "$SYSTEMD_AVAILABLE" != "true" ]; then
        return 1
    fi
    systemctl is-active "$name" >/dev/null 2>&1
}

# deploy_service_file <src_file> <dst_name> <pid_path> [bin_path]
#   复制自定义 systemd 单元文件到 /etc/systemd/system/<dst_name>，
#   并把 PIDFile= 行替换为实际 pid_path（与 nginx.conf 的 pid 指令对齐）。
#   传入 bin_path 时同步替换 ExecStartPre/ExecStart/ExecReload 的二进制。
deploy_service_file() {
    local src_file="$1"
    local dst_name="$2"
    local pid_path="$3"
    local bin_path="${4:-}"

    if [ ! -f "$src_file" ]; then
        log_warn "未找到自定义 service 文件: $src_file，使用官方默认"
        return 0
    fi

    if [ -n "$bin_path" ]; then
        sed \
            -e "s|PIDFile=.*|PIDFile=${pid_path}|" \
            -e "s|^ExecStartPre=.*|ExecStartPre=${bin_path} -t -q|" \
            -e "s|^ExecStart=.*|ExecStart=${bin_path}|" \
            -e "s|^ExecReload=.*|ExecReload=${bin_path} -s reload|" \
            "$src_file" > "/etc/systemd/system/${dst_name}"
    else
        sed "s|PIDFile=.*|PIDFile=${pid_path}|" "$src_file" \
            > "/etc/systemd/system/${dst_name}"
    fi
    systemctl daemon-reload
    log_info "已部署自定义 ${dst_name}（PIDFile=${pid_path}）"
}

# write_tmpfilesd <path> <mode> <user> <group>
#   写 /etc/tmpfiles.d/nxpanel.conf 条目（仅一条 /run/nxpanel）
write_tmpfilesd_nxpanel() {
    mkdir -p /etc/tmpfiles.d
    cat > /etc/tmpfiles.d/nxpanel.conf <<'EOF'
d /run/nxpanel 0755 root root -
EOF
}
