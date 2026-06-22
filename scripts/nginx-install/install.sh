#!/usr/bin/env bash
# ============================================================
# scripts/nginx-install/install.sh — Nginx / OpenResty 安装脚本入口
#
# 模块化拆分：
#   lib/     纯工具函数
#   dist/    发行版适配器（apt、dnf）：负责"怎么装包"
#   runtime/ runtime 适配器（nginx、openresty）：负责"装什么"
#
# 用法：
#   sudo bash scripts/nginx-install/install.sh [--nginx|--openresty] [--non-interactive] [--panel-dir DIR]
# ============================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# 脚本位于 <repo>/scripts/nginx-install/install.sh
# - SCRIPTS_DIR      → <repo>/scripts（找 nginx.service、openresty.service）
# - CONF_TEMPLATE_DIR → <repo>/configs/nginx（找 nginx.conf.production / nginx.conf.openresty）
SCRIPTS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CONF_TEMPLATE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)/configs/nginx"

# 加载 lib
. "$SCRIPT_DIR/lib/log.sh"
. "$SCRIPT_DIR/lib/os.sh"
. "$SCRIPT_DIR/lib/command.sh"
. "$SCRIPT_DIR/lib/matrix.sh"
. "$SCRIPT_DIR/lib/repo.sh"
. "$SCRIPT_DIR/lib/service.sh"
. "$SCRIPT_DIR/lib/verify.sh"

# ============================================================
# 默认参数 + 全局结果变量（保留与旧脚本同名，便于日志输出兼容）
# ============================================================

RUNTIME=""              # nginx / openresty（由参数或交互选择）
INTERACTIVE=true
PANEL_DIR="/opt/nxpanel/nginx"
INSECURE_RELAX_SHA1=false   # 仅 Debian 13 + OpenResty + 用户显式 opt-in 时使用

# 安装后处理填充
NGINX_BIN=""
NGINX_CONF=""
NGINX_VERSION=""
NGINX_PREFIX=""
NGINX_PID_PATH=""
WEB_USER=""
WEB_GROUP=""
OPENRESTY=false         # 兼容旧输出字段

has_tty() {
    [ -r /dev/tty ] && [ -w /dev/tty ]
}

prompt_read() {
    local prompt="$1"
    local var_name="$2"
    local read_opts="$3"

    if has_tty; then
        read $read_opts -p "$prompt" "$var_name" </dev/tty
        return
    fi

    read $read_opts -p "$prompt" "$var_name"
}

ensure_interactive_input() {
    if [ "$INTERACTIVE" = true ] && ! has_tty && [ ! -t 0 ]; then
        log_warn "当前 stdin 不是终端且 /dev/tty 不可用，自动切换为非交互模式"
        INTERACTIVE=false
    fi
}

# ============================================================
# 参数解析
# ============================================================

print_usage() {
    cat <<EOF
用法: sudo bash $0 [选项]

选项:
  --nginx            安装 Nginx（nginx.org 官方仓库）
  --openresty        安装 OpenResty（openresty.org 官方仓库）
  --non-interactive  非交互模式（默认安装 Nginx）
  --panel-dir DIR    面板 nginx 配置目录（默认: /opt/nxpanel/nginx）
  --insecure-relax-sha1
                     临时放宽 Debian 13 的 apt-sqv SHA1 接受策略到 2027-02-01。
                     仅在 Debian 13 + OpenResty 组合下生效（OpenResty 仓库 key
                     的 binding signature 仍使用 SHA1，被 Sequoia 默认拒绝）。
                     安装完成（或失败）后自动清理；下次 apt upgrade openresty
                     仍会失败，需重新跑本脚本或手动应用 workaround。
                     ⚠ 该放宽会临时影响系统所有 apt 仓库的 SHA1 签名接受。
  --help, -h         显示此帮助信息

示例:
  sudo bash $0                                # 交互式选择
  sudo bash $0 --nginx                        # 直接安装 Nginx
  sudo bash $0 --openresty --non-interactive  # 非交互安装 OpenResty
  sudo bash $0 --openresty --non-interactive --insecure-relax-sha1
                                              # Debian 13 + OpenResty opt-in
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --nginx)
                RUNTIME="nginx"
                shift
                ;;
            --openresty)
                RUNTIME="openresty"
                shift
                ;;
            --non-interactive)
                INTERACTIVE=false
                shift
                ;;
            --panel-dir)
                [ $# -ge 2 ] || die "--panel-dir 缺少参数"
                PANEL_DIR="$2"
                shift 2
                ;;
            --insecure-relax-sha1)
                INSECURE_RELAX_SHA1=true
                shift
                ;;
            --help|-h)
                print_usage
                exit 0
                ;;
            *)
                echo "未知参数: $1"
                echo "使用 --help 查看帮助信息"
                exit 1
                ;;
        esac
    done
}

# ============================================================
# 已有安装检查（交互确认覆盖）
# ============================================================

check_existing_runtime() {
    local bin_name="${RUNTIME_BINARY_CANDIDATES[0]}"
    if ! command -v "$bin_name" >/dev/null 2>&1; then
        return 0
    fi

    local existing_version
    existing_version=$("$bin_name" -v 2>&1 || true)
    log_warn "已检测到 ${RUNTIME_ID}: $existing_version"

    if [ "$INTERACTIVE" = true ]; then
        prompt_read "是否继续安装（将覆盖现有 ${RUNTIME_ID}）？(y/n): " REPLY "-n 1 -r"
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "安装已取消"
            exit 0
        fi
    else
        log_info "非交互模式：将覆盖现有 ${RUNTIME_ID}"
    fi
}

# ============================================================
# 交互选择 runtime
# ============================================================

choose_runtime() {
    if [ -n "$RUNTIME" ]; then
        return
    fi

    if [ "$INTERACTIVE" = true ]; then
        echo ""
        echo "请选择要安装的 Web 服务器："
        echo "  1) Nginx（推荐，来自 nginx.org 官方仓库）"
        echo "  2) OpenResty（来自 openresty.org 官方仓库）"
        echo "  3) 取消安装"
        echo ""
        prompt_read "请输入选项 (1-3): " REPLY "-n 1 -r"
        echo
        case "$REPLY" in
            1) RUNTIME="nginx" ;;
            2) RUNTIME="openresty" ;;
            *) log_info "安装已取消"; exit 0 ;;
        esac
    else
        RUNTIME="nginx"
    fi
}

# ============================================================
# 安装后处理
# ============================================================

stop_runtime_if_started() {
    log_step "停止包管理器自动启动的 ${RUNTIME_SERVICE}..."

    if [ "$SYSTEMD_AVAILABLE" = "true" ]; then
        local svc
        for svc in "$RUNTIME_SERVICE" nginx openresty; do
            [ -n "$svc" ] || continue
            if systemctl is-active --quiet "$svc" 2>/dev/null; then
                if systemctl stop "$svc" 2>/dev/null; then
                    log_info "已停止 $svc 服务，后续由面板按需启动"
                else
                    log_warn "停止 $svc 服务失败，继续尝试二进制 quit"
                fi
            fi
        done
    fi

    if [ -n "$NGINX_BIN" ] && [ -n "$NGINX_CONF" ]; then
        "$NGINX_BIN" -c "$NGINX_CONF" -s quit >/dev/null 2>&1 || true
    fi
}

deploy_production_conf() {
    log_step "部署生产 nginx.conf..."

    local template="${CONF_TEMPLATE_DIR}/${RUNTIME_CONF_TEMPLATE}"
    if [ ! -f "$template" ]; then
        log_warn "未找到配置模板: $template，跳过部署"
        log_warn "将使用发行版默认配置"
        return
    fi

    # 备份原配置
    if [ -f "$NGINX_CONF" ]; then
        local backup="${NGINX_CONF}.bak.$(date +%Y%m%d%H%M%S)"
        cp "$NGINX_CONF" "$backup"
        log_info "已备份原始配置: $backup"
    fi

    local web_user_default
    web_user_default=$(resolve_web_user_default)

    local content
    content=$(cat "$template")
    content="${content//\{\{WEB_USER\}\}/$web_user_default}"
    content="${content//\{\{PANEL_DIR\}\}/$PANEL_DIR}"
    content="${content//\{\{PID_PATH\}\}/$NGINX_PID_PATH}"
    content="${content//\{\{ERROR_LOG_PATH\}\}//var/log/nginx/error.log}"

    echo "$content" > "$NGINX_CONF"
    log_info "已部署面板配置: $NGINX_CONF"
}

ensure_directories() {
    local nginx_conf_dir
    nginx_conf_dir=$(dirname "$NGINX_CONF")

    mkdir -p "$nginx_conf_dir/conf.d"
    mkdir -p "$nginx_conf_dir/sites-available"
    mkdir -p "$nginx_conf_dir/sites-enabled"
    mkdir -p /var/log/nginx
}

remove_default_site() {
    log_step "清除默认站点..."

    local nginx_conf_dir
    nginx_conf_dir=$(dirname "$NGINX_CONF")

    if [ -f "$nginx_conf_dir/sites-enabled/default" ]; then
        rm -f "$nginx_conf_dir/sites-enabled/default"
        log_info "已删除 sites-enabled/default"
    fi
    if [ -f "$nginx_conf_dir/conf.d/default.conf" ]; then
        rm -f "$nginx_conf_dir/conf.d/default.conf"
        log_info "已删除 conf.d/default.conf"
    fi
}

detect_web_user() {
    WEB_USER=$(resolve_web_user_default)
    WEB_GROUP="$WEB_USER"

    if [ -z "$WEB_USER" ]; then
        # 回退：根据包管理器选择默认
        if [ "$PKG_MANAGER" = "apt" ]; then
            WEB_USER="www-data"
        else
            WEB_USER="nginx"
        fi
        WEB_GROUP="$WEB_USER"
    fi

    # 从 nginx.conf user 指令解析（部署模板后该值就是我们注入的）
    if [ -f "$NGINX_CONF" ]; then
        local parsed_user parsed_group
        parsed_user=$(grep -E '^[[:space:]]*user[[:space:]]+' "$NGINX_CONF" | head -n 1 | awk '{print $2}' | tr -d ';' || true)
        parsed_group=$(grep -E '^[[:space:]]*user[[:space:]]+' "$NGINX_CONF" | head -n 1 | awk '{print $3}' | tr -d ';' || true)
        if [ -n "$parsed_user" ]; then
            WEB_USER="$parsed_user"
            WEB_GROUP="${parsed_group:-$parsed_user}"
        fi
    fi

    if [ -z "$WEB_USER" ]; then
        local v_output
        v_output=$("$NGINX_BIN" -V 2>&1 || true)
        WEB_USER=$(printf '%s\n' "$v_output" | grep -oP '(?<=--user=)\S+' | head -n 1 || true)
        WEB_GROUP=$(printf '%s\n' "$v_output" | grep -oP '(?<=--group=)\S+' | head -n 1 || true)
        [ -n "$WEB_USER" ] && [ -z "$WEB_GROUP" ] && WEB_GROUP="$WEB_USER"
    fi

    if [ -z "$WEB_USER" ]; then
        WEB_USER=$(ps -eo user=,comm= | awk '$2 == "nginx" { print $1; exit }' || true)
        [ -n "$WEB_USER" ] && WEB_GROUP="$WEB_USER"
    fi

    if [ -z "$WEB_USER" ]; then
        WEB_USER="www-data"
        WEB_GROUP="$WEB_USER"
    fi

    log_info "Web 用户: $WEB_USER:$WEB_GROUP"
}

create_panel_directories() {
    log_step "创建面板目录..."

    # 面板 nginx 配置目录
    mkdir -p "$PANEL_DIR"/{conf.d,sites-available,sites-enabled,rewrite,ssl,ssl-store,backups,access-limit,htpasswd,proxy/cache}

    # 面板数据目录
    mkdir -p /opt/nxpanel/data

    # Socket 运行目录
    mkdir -p /run/nxpanel

    # 默认日志目录
    mkdir -p /www/wwwlogs
    chown "$WEB_USER:$WEB_GROUP" /www/wwwlogs

    # 默认站点根目录
    mkdir -p /www/wwwroot
    chown "$WEB_USER:$WEB_GROUP" /www/wwwroot

    log_info "面板目录创建完成"
}

setup_systemd() {
    log_step "配置 systemd..."

    if [ "$SYSTEMD_AVAILABLE" != "true" ]; then
        log_warn "systemd 不可用（容器/WSL 环境？），跳过 systemd 配置"
        return
    fi

    write_tmpfilesd_nxpanel

    local custom_service="${SCRIPTS_DIR}/${RUNTIME_CUSTOM_SERVICE_FILE}"
    deploy_service_file "$custom_service" "${RUNTIME_SERVICE}.service" "$NGINX_PID_PATH" "$NGINX_BIN"

    # enable 但不 start（让面板管理；与原脚本语义一致）
    service_enable "$RUNTIME_SERVICE"
}

verify_nginx_config() {
    log_step "验证 nginx 配置..."

    local pid_path="$NGINX_PID_PATH"
    [ -z "$pid_path" ] && pid_path="/var/run/nginx.pid"

    # 清理无效 PID 文件
    if [ -f "$pid_path" ]; then
        local old_pid
        old_pid=$(cat "$pid_path" 2>/dev/null || true)
        if [ -z "$old_pid" ] || ! kill -0 "$old_pid" 2>/dev/null; then
            rm -f "$pid_path"
            log_info "已清理无效 PID 文件: $pid_path"
        fi
    fi

    local test_result
    if test_result=$("$NGINX_BIN" -t -c "$NGINX_CONF" 2>&1); then
        log_info "nginx -t 验证通过"
    else
        log_warn "nginx -t 验证失败:"
        echo "$test_result"
        log_warn "配置可能需要手动调整，请检查 $NGINX_CONF"
    fi
}

output_result() {
    echo ""
    printf "${LOG_GREEN}========================================${LOG_NC}\n"
    printf "${LOG_GREEN}Nginx/OpenResty 安装完成${LOG_NC}\n"
    printf "${LOG_GREEN}========================================${LOG_NC}\n"
    echo ""
    echo "NGINX_BIN=$NGINX_BIN"
    echo "NGINX_CONF=$NGINX_CONF"
    echo "NGINX_VERSION=$NGINX_VERSION"
    echo "NGINX_PREFIX=$NGINX_PREFIX"
    echo "WEB_USER=$WEB_USER"
    echo "WEB_GROUP=$WEB_GROUP"
    echo "OPENRESTY=$OPENRESTY"
    echo ""
}

# ============================================================
# 主流程
# ============================================================

main() {
    printf "${LOG_GREEN}========================================${LOG_NC}\n"
    printf "${LOG_GREEN}Nginx / OpenResty 安装脚本${LOG_NC}\n"
    printf "${LOG_GREEN}========================================${LOG_NC}\n"
    echo ""

    parse_args "$@"
    ensure_interactive_input
    check_root
    detect_os
    detect_systemd

    # 必须先选 runtime，再加载 runtime adapter（detect_os 之前无法加载）
    choose_runtime

    # 加载 runtime + dist adapter
    # 顺序：runtime 先（dist_add_repository 内部要调 runtime_*）
    if [ ! -f "$SCRIPT_DIR/runtime/${RUNTIME}.sh" ]; then
        die "未知的 runtime: $RUNTIME（应为 nginx 或 openresty）"
    fi
    if [ ! -f "$SCRIPT_DIR/dist/${PKG_MANAGER}.sh" ]; then
        die "未知的 dist adapter: $PKG_MANAGER（应为 apt 或 dnf）"
    fi
    . "$SCRIPT_DIR/runtime/${RUNTIME}.sh"
    . "$SCRIPT_DIR/dist/${PKG_MANAGER}.sh"

    # 已有安装检查（依赖 RUNTIME_BINARY_CANDIDATES，需在加载 runtime 后）
    check_existing_runtime

    # 兼容旧输出字段
    if [ "$RUNTIME" = "openresty" ]; then
        OPENRESTY=true
    fi

    echo ""
    log_info "将安装: $RUNTIME"
    log_info "操作系统: $OS_ID $OS_VERSION_ID"
    log_info "包管理器: $PKG_MANAGER"
    echo ""

    # 校验支持范围
    runtime_validate_support
    dist_validate_support

    # 安装流程
    dist_install_prereqs
    dist_add_repository
    dist_refresh_cache
    dist_install_packages "$RUNTIME_PACKAGE"

    # 验证 + 安装后处理
    verify_runtime_installation
    stop_runtime_if_started
    deploy_production_conf
    ensure_directories
    remove_default_site
    detect_web_user
    create_panel_directories
    setup_systemd
    verify_nginx_config

    output_result
}

# 注册 trap：异常退出时清理临时文件
trap 'cleanup_temp' EXIT INT TERM

main "$@"
