#!/bin/bash
set -e

# ============================================================
# nxpanel 卸载脚本
#
# 用法：
#   sudo bash uninstall.sh                    # 交互模式，保留数据目录
#   sudo bash uninstall.sh --non-interactive   # 非交互模式，保留数据目录
#   sudo bash uninstall.sh --purge             # 全部删除（含数据，不备份）
#   sudo bash uninstall.sh --purge-data        # 删除数据目录（交互式备份提示）
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

INSTALL_ROOT="/usr/local/nxpanel"
DATA_DIR="/opt/nxpanel"
USER="openrest"
INTERACTIVE=true
PURGE=false
PURGE_DATA=false
SKIP_BACKUP=false

BACKUP_BASE_DIR="$HOME"

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${CYAN}[STEP]${NC} $1"; }

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

die() {
    log_error "$1"
    exit 1
}

confirm_yes() {
    local prompt="$1"
    local default="${2:-n}"

    if [ "$INTERACTIVE" != true ]; then
        return 1
    fi

    local suffix
    if [ "$default" = "y" ]; then
        suffix="[Y/n]"
    else
        suffix="[y/N]"
    fi

    prompt_read "$prompt $suffix " REPLY "-r"
    echo

    if [ -z "$REPLY" ]; then
        [ "$default" = "y" ] && return 0 || return 1
    fi

    [[ $REPLY =~ ^[Yy]$ ]]
}

# ============================================================
# 步骤 1：停止并禁用 systemd 服务
# ============================================================

stop_services() {
    log_step "[1/6] 停止服务..."

    for svc in nxpanel-api nxpanel-agent; do
        if systemctl is-active --quiet "$svc" 2>/dev/null; then
            systemctl stop "$svc" && log_info "$svc 已停止" || log_warn "$svc 停止失败"
        else
            log_info "$svc 未在运行，跳过"
        fi

        if systemctl is-enabled --quiet "$svc" 2>/dev/null; then
            systemctl disable "$svc" && log_info "$svc 已禁用" || log_warn "$svc 禁用失败"
        else
            log_info "$svc 未启用，跳过"
        fi
    done
}

# ============================================================
# 步骤 2：删除 systemd 服务文件
# ============================================================

remove_service_files() {
    log_step "[2/6] 删除 systemd 服务文件..."

    local changed=false

    for svc in nxpanel-api.service nxpanel-agent.service; do
        local path="/etc/systemd/system/$svc"
        if [ -f "$path" ]; then
            rm -f "$path"
            log_info "已删除 $path"
            changed=true
        else
            log_info "$path 不存在，跳过"
        fi
    done

    if [ "$changed" = true ]; then
        systemctl daemon-reload
        log_info "systemd 已重载"
    fi
}

# ============================================================
# 步骤 3：删除运行时目录
# ============================================================

remove_runtime_dir() {
    log_step "[3/6] 删除运行时目录..."

    if [ -d /run/nxpanel ]; then
        rm -rf /run/nxpanel
        log_info "已删除 /run/nxpanel"
    else
        log_info "/run/nxpanel 不存在，跳过"
    fi
}

# ============================================================
# 步骤 4：删除安装目录
# ============================================================

remove_install_dir() {
    log_step "[4/6] 删除安装目录 ($INSTALL_ROOT)..."

    if [ -d "$INSTALL_ROOT" ]; then
        rm -rf "$INSTALL_ROOT"
        log_info "已删除 $INSTALL_ROOT"
    else
        log_info "$INSTALL_ROOT 不存在，跳过"
    fi
}

# ============================================================
# 步骤 5：备份数据（交互模式）
# ============================================================

backup_data() {
    if [ ! -d "$DATA_DIR" ]; then
        return
    fi

    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local backup_file="$BACKUP_BASE_DIR/nxpanel-backup-${timestamp}.tar.gz"

    log_step "[5/6] 备份数据..."

    echo ""
    echo -e "${YELLOW}数据目录 $DATA_DIR 包含：${NC}"
    local db_count=0
    [ -f "$DATA_DIR/data/panel.db" ] && { echo "  - 数据库: $DATA_DIR/data/panel.db"; db_count=1; }
    [ -d "$DATA_DIR/nginx" ] && echo "  - Nginx 配置: $DATA_DIR/nginx/"
    [ -d "$DATA_DIR/logs" ] && echo "  - 日志: $DATA_DIR/logs/"
    echo ""

    if confirm_yes "是否将数据目录备份到 $backup_file？" "y"; then
        local data_parent
        data_parent=$(dirname "$DATA_DIR")
        local data_name
        data_name=$(basename "$DATA_DIR")

        tar -czf "$backup_file" -C "$data_parent" "$data_name" 2>/dev/null && {
            local size
            size=$(du -sh "$backup_file" 2>/dev/null | cut -f1 || echo "?")
            log_info "备份完成: $backup_file ($size)"
        } || {
            log_error "备份失败"
            if ! confirm_yes "备份失败，是否仍要删除数据目录？" "n"; then
                log_info "保留数据目录: $DATA_DIR"
                return
            fi
        }
    else
        log_info "跳过备份"
    fi
}

# ============================================================
# 步骤 5/6：处理数据目录
# ============================================================

handle_data_dir() {
    if [ ! -d "$DATA_DIR" ]; then
        log_info "$DATA_DIR 不存在，跳过数据目录处理"
        return
    fi

    if [ "$PURGE" = true ]; then
        log_step "[5/6] 删除数据目录 (--purge 模式，跳过备份)..."
        rm -rf "$DATA_DIR"
        log_info "已删除 $DATA_DIR"
        return
    fi

    if [ "$PURGE_DATA" = true ] || [ "$INTERACTIVE" = true ]; then
        if [ "$INTERACTIVE" = true ]; then
            backup_data
        fi

        if [ "$PURGE_DATA" = true ]; then
            log_step "[5/6] 删除数据目录 (--purge-data)..."
            rm -rf "$DATA_DIR"
            log_info "已删除 $DATA_DIR"
            return
        fi

        if [ "$INTERACTIVE" = true ]; then
            if confirm_yes "是否删除数据目录(请确认已备份每个站点的nginx配置) $DATA_DIR ？" "n"; then
                rm -rf "$DATA_DIR"
                log_info "已删除 $DATA_DIR"
            else
                log_info "保留数据目录: $DATA_DIR"
            fi
        else
            log_info "保留数据目录: $DATA_DIR（使用 --purge-data 可删除）"
        fi
    else
        log_info "保留数据目录: $DATA_DIR"
    fi
}

# ============================================================
# 步骤 6：删除系统用户
# ============================================================

remove_user() {
    log_step "[6/6] 清理系统用户..."

    if ! id "$USER" &>/dev/null; then
        log_info "用户 $USER 不存在，跳过"
        return
    fi

    local user_processes
    user_processes=$(pgrep -u "$USER" 2>/dev/null || true)
    if [ -n "$user_processes" ]; then
        log_warn "用户 $USER 仍有活跃进程 (PID: $user_processes)，跳过删除"
        log_warn "请手动终止进程后执行: userdel $USER"
        return
    fi

    if [ "$INTERACTIVE" = true ]; then
        if ! confirm_yes "是否删除系统用户 $USER？" "y"; then
            log_info "保留用户 $USER"
            return
        fi
    fi

    userdel "$USER" 2>/dev/null && log_info "用户 $USER 已删除" || log_warn "用户 $USER 删除失败（可能仍有文件归属）"
}

# ============================================================
# 显示卸载摘要
# ============================================================

show_summary() {
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}nxpanel 卸载完成${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "已删除："
    echo "  - systemd 服务: nxpanel-api, nxpanel-agent"
    echo "  - 安装目录:     $INSTALL_ROOT"
    echo "  - 运行时目录:   /run/nxpanel"
    [ ! -d "$DATA_DIR" ] && echo "  - 数据目录:     $DATA_DIR"
    echo ""

    if [ -d "$DATA_DIR" ]; then
        echo -e "${YELLOW}已保留：${NC}"
        echo "  - 数据目录:     $DATA_DIR"
        echo "    如需彻底删除: rm -rf $DATA_DIR"
        echo ""
    fi

    if id "$USER" &>/dev/null; then
        echo -e "${YELLOW}已保留：${NC}"
        echo "  - 系统用户:     $USER"
        echo "    如需删除:     userdel $USER"
        echo ""
    fi

    echo -e "${CYAN}提示：Nginx/OpenResty 未被卸载，如需卸载请手动操作。${NC}"
    echo ""
}

# ============================================================
# 参数解析
# ============================================================

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --non-interactive)
                INTERACTIVE=false
                shift
                ;;
            --purge)
                PURGE=true
                shift
                ;;
            --purge-data)
                PURGE_DATA=true
                shift
                ;;
            --install-dir)
                INSTALL_ROOT="$2"
                shift 2
                ;;
            --data-dir)
                DATA_DIR="$2"
                shift 2
                ;;
            --help|-h)
                echo "用法: sudo $0 [选项]"
                echo ""
                echo "卸载选项:"
                echo "  --non-interactive     非交互模式（保留数据目录和用户）"
                echo "  --purge               全部删除（含数据目录，不备份）"
                echo "  --purge-data          删除数据目录（交互模式下先提示备份）"
                echo ""
                echo "自定义路径:"
                echo "  --install-dir DIR     安装根目录 (默认: /usr/local/nxpanel)"
                echo "  --data-dir DIR        数据目录 (默认: /opt/nxpanel)"
                echo ""
                echo "示例:"
                echo "  sudo bash $0                          # 交互模式，保留数据"
                echo "  sudo bash $0 --non-interactive         # 静默卸载，保留数据"
                echo "  sudo bash $0 --purge                   # 全部删除"
                echo "  sudo bash $0 --purge-data              # 删除数据（交互式备份）"
                echo ""
                echo "注意: Nginx/OpenResty 不会被卸载，需手动操作。"
                exit 0
                ;;
            *)
                die "未知参数: $1\n使用 --help 查看帮助信息"
                ;;
        esac
    done
}

# ============================================================
# 主流程
# ============================================================

main() {
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}nxpanel 卸载脚本${NC}"
    echo -e "${RED}========================================${NC}"
    echo ""

    if [[ $EUID -ne 0 ]]; then
        die "请使用 root 权限运行此脚本"
    fi

    parse_args "$@"
    ensure_interactive_input

    echo -e "${YELLOW}即将卸载以下内容：${NC}"
    echo "  安装目录: $INSTALL_ROOT"
    echo "  数据目录: $DATA_DIR"
    [ "$PURGE" = true ] && echo "  模式:     全部删除 (--purge)"
    [ "$PURGE_DATA" = true ] && echo "  模式:     删除数据 (--purge-data)"
    echo ""

    if [ "$INTERACTIVE" = true ]; then
        if ! confirm_yes "确认卸载 nxpanel？" "n"; then
            echo "卸载已取消"
            exit 0
        fi
    fi

    stop_services
    remove_service_files
    remove_runtime_dir
    remove_install_dir
    handle_data_dir
    remove_user

    show_summary
}

main "$@"
