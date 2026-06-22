# ============================================================
# lib/verify.sh — 安装后验证：二进制定位、编译参数、模块校验
#
# 提供：
#   find_runtime_binary
#   get_nginx_compile_info
#   check_required_modules
#   verify_runtime_installation
#
# 全局结果变量（由 verify_runtime_installation 填充）：
#   NGINX_BIN、NGINX_VERSION、NGINX_CONF、NGINX_PREFIX、NGINX_PID_PATH
#
# 依赖：lib/log.sh、runtime/<id>.sh（RUNTIME_BINARY_CANDIDATES）
# ============================================================

# find_runtime_binary
#   遍历 RUNTIME_BINARY_CANDIDATES，依次尝试 command -v 和绝对路径。
#   命中后写入 NGINX_BIN；全部失败则 die。
find_runtime_binary() {
    local cand
    NGINX_BIN=""
    for cand in "${RUNTIME_BINARY_CANDIDATES[@]:-}"; do
        [ -z "$cand" ] && continue
        # command -v 对绝对路径也有效；优先取 PATH 中的可执行文件
        if [ "${cand:0:1}" = "/" ]; then
            if [ -x "$cand" ]; then
                NGINX_BIN="$cand"
                return
            fi
        else
            local resolved
            resolved=$(command -v "$cand" 2>/dev/null || true)
            if [ -n "$resolved" ] && [ -x "$resolved" ]; then
                NGINX_BIN="$resolved"
                return
            fi
        fi
    done

    die "安装失败：未找到 ${RUNTIME_ID} 二进制文件（候选：${RUNTIME_BINARY_CANDIDATES[*]}）"
}

# get_nginx_compile_info
#   运行 $NGINX_BIN -V（输出在 stderr），解析 conf-path/prefix/pid-path 与 version。
get_nginx_compile_info() {
    local v_output
    v_output=$("$NGINX_BIN" -V 2>&1 || true)
    if [ -z "$v_output" ]; then
        log_warn "无法读取 ${RUNTIME_ID} 编译参数，将使用默认配置路径"
    fi

    # 版本
    NGINX_VERSION=$(echo "$v_output" | grep -oP 'nginx version:\s*\K\S+' | head -n 1 || true)
    if [ -z "$NGINX_VERSION" ]; then
        NGINX_VERSION=$("$NGINX_BIN" -v 2>&1 | grep -oP 'nginx/\S+' | head -n 1 || echo "unknown")
    fi

    # conf-path
    NGINX_CONF=$(echo "$v_output" | grep -oP '(?<=--conf-path=)\S+' | head -n 1 || true)
    if [ -z "$NGINX_CONF" ]; then
        local t_output
        t_output=$("$NGINX_BIN" -t 2>&1 || true)
        NGINX_CONF=$(echo "$t_output" | grep -oP 'configuration file \K\S+' | head -n 1 || true)
    fi
    if [ -z "$NGINX_CONF" ]; then
        if [ "$RUNTIME_ID" = "openresty" ]; then
            NGINX_CONF="/usr/local/openresty/nginx/conf/nginx.conf"
        else
            NGINX_CONF="/etc/nginx/nginx.conf"
        fi
    fi

    # prefix
    NGINX_PREFIX=$(echo "$v_output" | grep -oP '(?<=--prefix=)\S+' | head -n 1 || true)
    if [ -z "$NGINX_PREFIX" ]; then
        if [ "$RUNTIME_ID" = "openresty" ]; then
            NGINX_PREFIX="/usr/local/openresty/nginx"
        else
            NGINX_PREFIX="/etc/nginx"
        fi
    fi

    # pid-path
    NGINX_PID_PATH=$(echo "$v_output" | grep -oP '(?<=--pid-path=)\S+' | head -n 1 || true)
    if [ -z "$NGINX_PID_PATH" ]; then
        NGINX_PID_PATH="/var/run/nginx.pid"
    fi

    # 输出
    log_info "二进制路径: $NGINX_BIN"
    log_info "版本: $NGINX_VERSION"
    log_info "配置文件: $NGINX_CONF"
    log_info "Prefix: $NGINX_PREFIX"
    log_info "PID 文件: $NGINX_PID_PATH"
}

# check_required_modules
#   校验 http_ssl_module 与 PCRE 支持；缺失则 die。
check_required_modules() {
    local v_output
    v_output=$("$NGINX_BIN" -V 2>&1 || true)

    if [ -z "$v_output" ]; then
        log_warn "无法读取 ${RUNTIME_ID} 编译参数，跳过模块检查"
        return 0
    fi

    local has_ssl=false
    local has_pcre=false

    if echo "$v_output" | grep -q "http_ssl_module"; then
        has_ssl=true
    fi
    if echo "$v_output" | grep -qi "pcre" || ldd "$NGINX_BIN" 2>/dev/null | grep -qi "pcre"; then
        has_pcre=true
    fi

    if [ "$has_ssl" = false ]; then
        die "安装的 ${RUNTIME_ID} 缺少 http_ssl_module 模块，面板 SSL 功能无法使用。
请安装包含 SSL 模块的版本（官方仓库默认包含）。"
    fi
    if [ "$has_pcre" = false ]; then
        die "安装的 ${RUNTIME_ID} 缺少 PCRE 支持，面板访问限制功能无法使用。
请安装包含 PCRE 支持的版本。"
    fi
    log_info "模块检查通过: SSL ✓, PCRE ✓"
}

# verify_runtime_installation
#   入口：定位二进制 + 解析编译参数 + 校验必需模块。
verify_runtime_installation() {
    log_step "验证安装..."
    find_runtime_binary
    get_nginx_compile_info
    check_required_modules
}
