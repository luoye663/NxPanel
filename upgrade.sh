#!/bin/bash
set -e

# ============================================================
# nxpanel 升级脚本
#
# 支持两种升级方式：
#   1. 在线升级（从 GitHub Releases 下载）：
#      sudo bash upgrade.sh
#      sudo bash upgrade.sh --version v1.2.0
#
#   2. 使用本地 tarball：
#      sudo bash upgrade.sh --local ./nxpanel-linux-amd64.tar.gz
#
# 其他功能：
#   sudo bash upgrade.sh --check       # 仅检查更新
#   sudo bash upgrade.sh --list        # 列出可用版本
#   sudo bash upgrade.sh --current     # 查看当前版本
#   sudo bash upgrade.sh --rollback    # 回滚到上一个版本
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

INSTALL_ROOT="/usr/local/nxpanel"
CONFIG_DIR="$INSTALL_ROOT/configs"
BACKUP_DIR="$INSTALL_ROOT/backup"
MAX_BACKUPS=3
GITHUB_REPO="nxpanel/nxpanel"
MIN_UPGRADE_VERSION="0.0.1"

# 运行时变量
CURRENT_VERSION=""
TARGET_VERSION=""
LOCAL_TARBALL=""
TEMP_DIR=""
ACTION="upgrade"  # upgrade / check / list / current / rollback

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

confirm_yes() {
    local prompt="$1"
    local reply=""

    prompt_read "$prompt (y/n): " reply "-n 1 -r"
    echo
    [[ "$reply" =~ ^[Yy]$ ]]
}

die() {
    log_error "$1"
    cleanup_temp
    exit 1
}

cleanup_temp() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
        TEMP_DIR=""
    fi
}

# ============================================================
# 版本比较工具
# ============================================================

# 比较版本号，返回：
#   0 = 相等
#   1 = v1 > v2
#   2 = v1 < v2
version_compare() {
    local v1=$1
    local v2=$2
    
    # 移除 v 前缀
    v1=${v1#v}
    v2=${v2#v}
    
    # 分割版本号
    local IFS=.
    local ver1=($v1)
    local ver2=($v2)
    
    # 补齐长度
    local len=${#ver1[@]}
    if [ ${#ver2[@]} -gt $len ]; then
        len=${#ver2[@]}
    fi
    
    for ((i=0; i<len; i++)); do
        local n1=${ver1[i]:-0}
        local n2=${ver2[i]:-0}
        
        if [ "$n1" -gt "$n2" ]; then
            return 1
        elif [ "$n1" -lt "$n2" ]; then
            return 2
        fi
    done
    
    return 0
}

# v1 < v2
version_lt() {
    version_compare "$1" "$2"
    [ $? -eq 2 ]
}

# v1 > v2
version_gt() {
    version_compare "$1" "$2"
    [ $? -eq 1 ]
}

# v1 >= v2
version_gte() {
    version_compare "$1" "$2"
    [ $? -eq 0 ] || [ $? -eq 1 ]
}

# ============================================================
# 版本信息获取
# ============================================================

get_current_version() {
    if [ -x "$INSTALL_ROOT/bin/nxpanel-api" ]; then
        CURRENT_VERSION=$("$INSTALL_ROOT/bin/nxpanel-api" --version 2>/dev/null || echo "")
    fi

    if [ -z "$CURRENT_VERSION" ] && [ -x "$INSTALL_ROOT/bin/nxpanel-agent" ]; then
        CURRENT_VERSION=$("$INSTALL_ROOT/bin/nxpanel-agent" --version 2>/dev/null || echo "")
    fi
    
    if [ -z "$CURRENT_VERSION" ]; then
        CURRENT_VERSION="unknown"
    fi
}

get_package_version() {
    local source_dir=$1

    if [ -x "$source_dir/bin/nxpanel-api" ]; then
        "$source_dir/bin/nxpanel-api" --version 2>/dev/null || true
        return
    fi

    if [ -x "$source_dir/bin/nxpanel-agent" ]; then
        "$source_dir/bin/nxpanel-agent" --version 2>/dev/null || true
    fi
}

read_api_config_value() {
    local key="$1"
    local config_file="$CONFIG_DIR/config.yaml"

    [ -f "$config_file" ] || return 1

    awk -v key="$key" '
        /^[[:space:]]*api:[[:space:]]*$/ { in_api=1; next }
        /^[^[:space:]]/ { if (in_api) exit }
        in_api && $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
            line=$0
            sub("^[[:space:]]*" key ":[[:space:]]*", "", line)
            sub(/[[:space:]]+#.*/, "", line)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
            gsub(/^["\047]+|["\047]+$/, "", line)
            print line
            exit
        }
    ' "$config_file"
}

read_api_tls_enabled() {
    local config_file="$CONFIG_DIR/config.yaml"

    [ -f "$config_file" ] || return 1

    awk '
        function indent_len(s) {
            match(s, /^[[:space:]]*/)
            return RLENGTH
        }
        /^[[:space:]]*api:[[:space:]]*$/ { in_api=1; api_indent=indent_len($0); next }
        in_api && indent_len($0) <= api_indent && /^[^[:space:]]/ { exit }
        in_api && /^[[:space:]]*tls:[[:space:]]*$/ { in_tls=1; tls_indent=indent_len($0); next }
        in_api && in_tls && indent_len($0) <= tls_indent && $0 !~ /^[[:space:]]*($|#)/ { in_tls=0 }
        in_api && in_tls && /^[[:space:]]*enabled:[[:space:]]*/ {
            line=$0
            sub(/^[[:space:]]*enabled:[[:space:]]*/, "", line)
            sub(/[[:space:]]+#.*/, "", line)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
            gsub(/^["\047]+|["\047]+$/, "", line)
            print line
            exit
        }
    ' "$config_file"
}

normalize_health_listen() {
    local listen="$1"

    listen=${listen:-"127.0.0.1:8888"}
    case "$listen" in
        0.0.0.0:*)
            echo "127.0.0.1:${listen#0.0.0.0:}"
            ;;
        "[::]:"*)
            echo "127.0.0.1:${listen#\[::\]:}"
            ;;
        ":::"*)
            echo "127.0.0.1:${listen#:::}"
            ;;
        :*)
            echo "127.0.0.1$listen"
            ;;
        *)
            echo "$listen"
            ;;
    esac
}

get_latest_release() {
    local api_url="https://api.github.com/repos/$GITHUB_REPO/releases/latest"
    
    # 使用 curl 获取最新 release
    local response
    if ! response=$(curl -s -f "$api_url" 2>/dev/null); then
        # 如果 latest 失败，尝试获取所有 releases
        api_url="https://api.github.com/repos/$GITHUB_REPO/releases"
        if ! response=$(curl -s -f "$api_url" 2>/dev/null); then
            return 1
        fi
        
        # 解析第一个非 draft release
        TARGET_VERSION=$(echo "$response" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')
    else
        TARGET_VERSION=$(echo "$response" | grep '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')
    fi
    
    if [ -z "$TARGET_VERSION" ]; then
        return 1
    fi
    
    return 0
}

list_releases() {
    local api_url="https://api.github.com/repos/$GITHUB_REPO/releases"
    
    log_info "从 GitHub 获取版本列表..."
    
    local response
    if ! response=$(curl -s -f "$api_url" 2>/dev/null); then
        die "无法获取版本列表，请检查网络连接"
    fi
    
    echo ""
    echo "可用版本："
    echo "----------------------------------------"
    echo "$response" | grep '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/' | while read -r version; do
        echo "  $version"
    done
    echo "----------------------------------------"
}

# ============================================================
# 下载和解压
# ============================================================

download_release() {
    local version=$1
    
    log_step "下载 nxpanel $version..."
    
    # 创建临时目录
    TEMP_DIR=$(mktemp -d /tmp/nxpanel-upgrade-XXXXXX)
    
    local download_url="https://github.com/$GITHUB_REPO/releases/download/$version/nxpanel-linux-amd64.tar.gz"
    local tarball="$TEMP_DIR/nxpanel-linux-amd64.tar.gz"
    
    # 下载
    if ! curl -L -o "$tarball" "$download_url" 2>/dev/null; then
        die "下载失败: $download_url"
    fi
    
    # 解压
    log_info "解压文件..."
    if ! tar -xzf "$tarball" -C "$TEMP_DIR"; then
        die "解压失败，文件可能损坏"
    fi
    
    # 验证关键文件
    if [ ! -f "$TEMP_DIR/nxpanel/bin/nxpanel-api" ]; then
        die "下载的包缺少 nxpanel-api 二进制"
    fi
    
    if [ ! -f "$TEMP_DIR/nxpanel/bin/nxpanel-agent" ]; then
        die "下载的包缺少 nxpanel-agent 二进制"
    fi
    
    log_info "下载完成"
}

extract_local() {
    local tarball=$1
    
    if [ ! -f "$tarball" ]; then
        die "文件不存在: $tarball"
    fi
    
    log_step "解压本地包..."
    
    # 创建临时目录
    TEMP_DIR=$(mktemp -d /tmp/nxpanel-upgrade-XXXXXX)
    
    # 解压
    if ! tar -xzf "$tarball" -C "$TEMP_DIR"; then
        die "解压失败，文件可能损坏"
    fi
    
    # 查找解压后的目录
    local source_dir="$TEMP_DIR/nxpanel"
    if [ ! -d "$source_dir" ]; then
        # 尝试查找其他目录名
        source_dir=$(find "$TEMP_DIR" -maxdepth 1 -type d -name "nxpanel*" | head -1)
        if [ -z "$source_dir" ]; then
            die "压缩包中找不到 nxpanel 目录"
        fi
    fi
    
    # 验证关键文件
    if [ ! -f "$source_dir/bin/nxpanel-api" ]; then
        die "压缩包缺少 nxpanel-api 二进制"
    fi
    
    log_info "解压完成"
}

# ============================================================
# 校验函数
# ============================================================

# 验证下载的包完整性
verify_package() {
    local source_dir="$TEMP_DIR/nxpanel"
    local checksums_file="$source_dir/checksums.txt"
    
    log_step "验证包完整性..."
    
    # 检查 checksums.txt 是否存在
    if [ ! -f "$checksums_file" ]; then
        log_warn "包中缺少 checksums.txt，跳过校验"
        return 0
    fi
    
    # 进入源目录执行校验
    local verify_result
    verify_result=$(cd "$source_dir" && sha256sum -c "$checksums_file" 2>&1)
    local verify_exit=$?
    
    if [ $verify_exit -ne 0 ]; then
        log_error "包完整性校验失败："
        echo "$verify_result" | grep -v "OK$"
        return 1
    fi
    
    log_info "包完整性校验通过"
    return 0
}

# 验证安装后的文件
verify_installation() {
    local source_dir="$TEMP_DIR/nxpanel"
    local checksums_file="$source_dir/checksums.txt"
    
    log_step "验证安装文件..."
    
    # 检查 checksums.txt 是否存在
    if [ ! -f "$checksums_file" ]; then
        log_warn "缺少 checksums.txt，跳过安装校验"
        return 0
    fi
    
    local errors=0
    
    # 逐行校验
    while IFS= read -r line; do
        # 跳过注释和空行
        [[ "$line" =~ ^#.*$ ]] && continue
        [[ -z "$line" ]] && continue
        
        # 解析哈希和文件路径
        local expected_hash=$(echo "$line" | awk '{print $1}')
        local file_path=$(echo "$line" | awk '{print $2}')
        
        # 检查文件是否存在
        local target_file="$INSTALL_ROOT/$file_path"
        if [ ! -f "$target_file" ]; then
            log_error "文件缺失: $file_path"
            errors=$((errors + 1))
            continue
        fi
        
        # 计算文件哈希
        local actual_hash=$(sha256sum "$target_file" | awk '{print $1}')
        if [ "$actual_hash" != "$expected_hash" ]; then
            log_error "文件校验失败: $file_path"
            log_error "  期望: $expected_hash"
            log_error "  实际: $actual_hash"
            errors=$((errors + 1))
        fi
    done < "$checksums_file"
    
    if [ $errors -gt 0 ]; then
        log_error "安装校验失败，共 $errors 个错误"
        return 1
    fi
    
    log_info "安装文件校验通过"
    return 0
}

# ============================================================
# 备份和回滚
# ============================================================

create_backup() {
    local backup_name="${CURRENT_VERSION}_$(date +%Y%m%d_%H%M%S)"
    local backup_path="$BACKUP_DIR/$backup_name"
    
    log_step "创建备份: $backup_name..."
    
    mkdir -p "$backup_path"
    
    # 备份二进制
    if [ -d "$INSTALL_ROOT/bin" ]; then
        cp -r "$INSTALL_ROOT/bin" "$backup_path/"
    fi
    
    # 备份前端
    if [ -d "$INSTALL_ROOT/web" ]; then
        cp -r "$INSTALL_ROOT/web" "$backup_path/"
    fi
    
    # 备份配置和模板
    if [ -d "$CONFIG_DIR" ]; then
        cp -r "$CONFIG_DIR" "$backup_path/"
    fi
    
    # 记录备份路径用于回滚
    echo "$backup_path" > "$BACKUP_DIR/latest"
    
    log_info "备份完成: $backup_path"
}

get_latest_backup() {
    local latest_file="$BACKUP_DIR/latest"
    
    if [ ! -f "$latest_file" ]; then
        return 1
    fi
    
    local backup_path
    backup_path=$(cat "$latest_file")
    
    if [ ! -d "$backup_path" ]; then
        return 1
    fi
    
    echo "$backup_path"
    return 0
}

rollback() {
    local backup_path
    
    if [ -n "$1" ]; then
        backup_path="$1"
    else
        backup_path=$(get_latest_backup)
        if [ $? -ne 0 ]; then
            die "找不到可用的备份"
        fi
    fi
    
    if [ ! -d "$backup_path" ]; then
        die "备份目录不存在: $backup_path"
    fi
    
    log_step "开始回滚..."
    
    # 停止服务
    stop_services_ignore_error
    
    # 恢复二进制
    if [ -d "$backup_path/bin" ]; then
        cp -r "$backup_path/bin/"* "$INSTALL_ROOT/bin/"
    fi
    
    # 恢复前端
    if [ -d "$backup_path/web" ]; then
        rm -rf "$INSTALL_ROOT/web"
        cp -r "$backup_path/web" "$INSTALL_ROOT/"
    fi
    
    # 恢复配置和模板
    local backup_config_dir="$backup_path/$(basename "$CONFIG_DIR")"
    if [ -d "$backup_config_dir" ]; then
        cp -r "$backup_config_dir/templates/"* "$CONFIG_DIR/templates/"
        if [ -f "$backup_config_dir/config.yaml" ]; then
            cp "$backup_config_dir/config.yaml" "$CONFIG_DIR/"
        fi
    fi
    
    # 启动服务
    start_services
    
    # 健康检查
    if health_check; then
        log_info "回滚成功"
    else
        log_error "回滚后服务异常，请手动检查"
        exit 1
    fi
}

cleanup_old_backups() {
    log_info "清理旧备份..."
    
    if [ ! -d "$BACKUP_DIR" ]; then
        return
    fi
    
    # 获取备份列表（按时间排序）
    local backups=($(ls -dt "$BACKUP_DIR"/*_* 2>/dev/null || true))
    local count=${#backups[@]}
    
    if [ "$count" -le "$MAX_BACKUPS" ]; then
        return
    fi
    
    # 删除多余的备份
    for ((i=MAX_BACKUPS; i<count; i++)); do
        log_info "删除旧备份: ${backups[$i]}"
        rm -rf "${backups[$i]}"
    done
}

# ============================================================
# 服务管理
# ============================================================

stop_services() {
    log_step "停止服务..."
    
    systemctl stop nxpanel-api 2>/dev/null || true
    systemctl stop nxpanel-agent 2>/dev/null || true
    
    # 等待服务完全停止
    sleep 2
    
    # 检查是否已停止
    if systemctl is-active --quiet nxpanel-api 2>/dev/null; then
        die "nxpanel-api 停止失败"
    fi
    
    if systemctl is-active --quiet nxpanel-agent 2>/dev/null; then
        die "nxpanel-agent 停止失败"
    fi
    
    log_info "服务已停止"
}

stop_services_ignore_error() {
    log_step "停止服务..."
    
    systemctl stop nxpanel-api 2>/dev/null || true
    systemctl stop nxpanel-agent 2>/dev/null || true
    
    sleep 2
}

start_services() {
    log_step "启动服务..."
    
    systemctl start nxpanel-agent
    systemctl start nxpanel-api
    
    sleep 3
    
    if systemctl is-active --quiet nxpanel-agent; then
        log_info "nxpanel-agent 启动成功"
    else
        log_error "nxpanel-agent 启动失败"
        return 1
    fi
    
    if systemctl is-active --quiet nxpanel-api; then
        log_info "nxpanel-api 启动成功"
    else
        log_error "nxpanel-api 启动失败"
        return 1
    fi
    
    return 0
}

health_check() {
    log_info "执行健康检查..."

    local api_listen
    api_listen=$(read_api_config_value "listen" || true)
    api_listen=$(normalize_health_listen "$api_listen")

    local tls_enabled
    tls_enabled=$(read_api_tls_enabled || true)
    local scheme="http"
    local curl_tls_opts=()
    if [ "$tls_enabled" = "true" ] || [ "$tls_enabled" = "1" ]; then
        scheme="https"
        curl_tls_opts=(-k)
    fi

    local public_health
    public_health=$(read_api_config_value "public_health" || true)
    local health_path="/health"
    if [ "$public_health" != "true" ] && [ "$public_health" != "1" ]; then
        health_path=$(read_api_config_value "login_path" || true)
        health_path=${health_path:-"/"}
    fi

    local health_url="${scheme}://${api_listen}${health_path}"

    local max_retries=10
    local retry_interval=3

    for ((i=1; i<=max_retries; i++)); do
        if curl -s -f "${curl_tls_opts[@]}" "$health_url" >/dev/null 2>&1; then
            log_info "健康检查通过"
            return 0
        fi

        log_warn "健康检查失败，重试 ($i/$max_retries)..."
        sleep $retry_interval
    done

    log_error "健康检查失败"
    return 1
}

# ============================================================
# 升级执行
# ============================================================

replace_files() {
    log_step "替换文件..."
    
    local source_dir="$TEMP_DIR/nxpanel"
    
    # 停止服务
    stop_services
    
    # 替换二进制
    log_info "更新二进制文件..."
    install -m 755 "$source_dir/bin/nxpanel-api" "$INSTALL_ROOT/bin/"
    install -m 755 "$source_dir/bin/nxpanel-agent" "$INSTALL_ROOT/bin/"
    
    # 替换前端
    if [ -d "$source_dir/web" ]; then
        log_info "更新前端文件..."
        rm -rf "$INSTALL_ROOT/web"
        mkdir -p "$INSTALL_ROOT/web"
        cp -r "$source_dir/web/"* "$INSTALL_ROOT/web/"
    fi
    
    # 替换模板
    if [ -d "$source_dir/configs/templates" ]; then
        log_info "更新模板文件..."
        cp -r "$source_dir/configs/templates/"* "$CONFIG_DIR/templates/"
    fi
    
    log_info "文件替换完成"
}

migrate_config() {
    log_step "迁移配置文件..."
    
    # 调用 agent 的配置迁移命令
    if ! "$INSTALL_ROOT/bin/nxpanel-agent" --migrate-config --config "$CONFIG_DIR/config.yaml"; then
        log_error "配置迁移失败"
        return 1
    fi
    
    log_info "配置迁移完成"
    return 0
}

# ============================================================
# 兼容性检查
# ============================================================

check_compatibility() {
    local current=$1
    local target=$2
    
    # 当前版本未知（可能是旧安装）
    if [ "$current" = "unknown" ]; then
        log_warn "无法确定当前版本，跳过兼容性检查"
        return 0
    fi
    
    # 检查当前版本是否满足最低要求
    if version_lt "$current" "$MIN_UPGRADE_VERSION"; then
        die "当前版本 $current 过旧，请先手动升级到 $MIN_UPGRADE_VERSION 或更高版本"
    fi
    
    # 检查是否降级
    if version_lt "$target" "$current"; then
        die "不支持从 $current 降级到 $target"
    fi
    
    # 检查是否相同版本
    if [ "$target" = "$current" ]; then
        log_warn "目标版本与当前版本相同 ($current)"
        return 1
    fi
    
    return 0
}

# ============================================================
# 参数解析
# ============================================================

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --version)
                TARGET_VERSION="$2"
                shift 2
                ;;
            --local)
                LOCAL_TARBALL="$2"
                shift 2
                ;;
            --check)
                ACTION="check"
                shift
                ;;
            --list)
                ACTION="list"
                shift
                ;;
            --current)
                ACTION="current"
                shift
                ;;
            --rollback)
                ACTION="rollback"
                shift
                ;;
            --force)
                FORCE=true
                shift
                ;;
            --repo)
                GITHUB_REPO="$2"
                shift 2
                ;;
            --help)
                show_help
                exit 0
                ;;
            *)
                die "未知参数: $1\n使用 --help 查看帮助信息"
                ;;
        esac
    done
}

show_help() {
    echo "用法: $0 [选项]"
    echo ""
    echo "升级操作:"
    echo "  (无参数)              在线升级到最新版本"
    echo "  --version <tag>       升级到指定版本"
    echo "  --local <tarball>     使用本地 tarball 升级"
    echo ""
    echo "查询操作:"
    echo "  --check               检查是否有可用更新"
    echo "  --list                列出所有可用版本"
    echo "  --current             查看当前安装版本"
    echo ""
    echo "回滚操作:"
    echo "  --rollback            回滚到上一个版本"
    echo ""
    echo "其他选项:"
    echo "  --force               跳过兼容性检查"
    echo "  --repo <owner/repo>   GitHub 仓库 (默认: $GITHUB_REPO)"
    echo "  --help                显示帮助信息"
    echo ""
    echo "示例:"
    echo "  sudo bash $0                           # 升级到最新版本"
    echo "  sudo bash $0 --version v1.2.0          # 升级到指定版本"
    echo "  sudo bash $0 --local ./nxpanel.tar.gz  # 使用本地包升级"
    echo "  sudo bash $0 --check                   # 检查更新"
    echo "  sudo bash $0 --rollback                # 回滚"
}

# ============================================================
# 主流程
# ============================================================

do_upgrade() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}nxpanel 升级程序${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    
    # 检查 root 权限
    if [[ $EUID -ne 0 ]]; then
        die "请使用 root 权限运行此脚本"
    fi
    
    # 获取当前版本
    get_current_version
    log_info "当前版本: $CURRENT_VERSION"
    
    # 确定目标版本和来源
    if [ -n "$LOCAL_TARBALL" ]; then
        # 本地模式
        extract_local "$LOCAL_TARBALL"
        
        # 尝试从包内二进制获取版本
        local package_version
        package_version=$(get_package_version "$TEMP_DIR/nxpanel")
        if [ -n "$package_version" ]; then
            TARGET_VERSION="$package_version"
        elif [ -z "$TARGET_VERSION" ]; then
            TARGET_VERSION="local"
        fi
    else
        # 在线模式
        if [ -z "$TARGET_VERSION" ]; then
            log_info "获取最新版本..."
            if ! get_latest_release; then
                die "无法获取最新版本，请检查网络连接"
            fi
        fi
        download_release "$TARGET_VERSION"
    fi
    
    log_info "目标版本: $TARGET_VERSION"
    
    # 验证包完整性
    if ! verify_package; then
        cleanup_temp
        die "包完整性校验失败，升级终止"
    fi
    
    # 兼容性检查
    if [ "$FORCE" != "true" ]; then
        if ! check_compatibility "$CURRENT_VERSION" "$TARGET_VERSION"; then
            cleanup_temp
            exit 0
        fi
    fi
    
    # 确认升级
    echo ""
    echo -e "${YELLOW}升级信息：${NC}"
    echo "  当前版本: $CURRENT_VERSION"
    echo "  目标版本: $TARGET_VERSION"
    echo "  安装目录: $INSTALL_ROOT"
    echo ""
    if ! confirm_yes "是否继续升级？"; then
        cleanup_temp
        echo "升级已取消"
        exit 0
    fi
    
    # 创建备份
    create_backup
    
    # 替换文件
    replace_files
    
    # 验证安装文件
    if ! verify_installation; then
        log_error "安装文件校验失败，开始回滚..."
        rollback
        exit 1
    fi
    
    # 配置迁移
    if ! migrate_config; then
        log_error "配置迁移失败，开始回滚..."
        rollback
        exit 1
    fi
    
    # 启动服务
    if ! start_services; then
        log_error "服务启动失败，开始回滚..."
        rollback
        exit 1
    fi
    
    # 健康检查
    if ! health_check; then
        log_error "健康检查失败，开始回滚..."
        rollback
        exit 1
    fi
    
    # 清理
    cleanup_temp
    cleanup_old_backups
    
    # 完成
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}升级成功！${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "版本: $CURRENT_VERSION -> $TARGET_VERSION"
    echo ""
    echo "备份位置: $BACKUP_DIR"
    echo "如需回滚: sudo bash $0 --rollback"
    echo ""
}

do_check() {
    get_current_version
    log_info "当前版本: $CURRENT_VERSION"
    
    log_info "检查最新版本..."
    if ! get_latest_release; then
        die "无法获取版本信息"
    fi
    
    log_info "最新版本: $TARGET_VERSION"
    
    if [ "$CURRENT_VERSION" = "unknown" ]; then
        log_warn "无法确定当前安装版本"
    elif version_gt "$TARGET_VERSION" "$CURRENT_VERSION"; then
        echo ""
        echo -e "${GREEN}有新版本可用！${NC}"
        echo "  当前: $CURRENT_VERSION"
        echo "  最新: $TARGET_VERSION"
        echo ""
        echo "执行升级: sudo bash $0 --version $TARGET_VERSION"
    elif [ "$TARGET_VERSION" = "$CURRENT_VERSION" ]; then
        echo ""
        log_info "已是最新版本"
    else
        echo ""
        log_warn "当前版本高于最新 release（可能使用了开发版）"
    fi
}

do_current() {
    get_current_version
    echo "当前版本: $CURRENT_VERSION"
}

do_rollback() {
    echo -e "${YELLOW}========================================${NC}"
    echo -e "${YELLOW}回滚操作${NC}"
    echo -e "${YELLOW}========================================${NC}"
    echo ""
    
    local backup_path
    backup_path=$(get_latest_backup)
    if [ $? -ne 0 ]; then
        die "找不到可用的备份"
    fi
    
    local backup_name=$(basename "$backup_path")
    log_info "将回滚到备份: $backup_name"
    
    if ! confirm_yes "是否继续？"; then
        echo "回滚已取消"
        exit 0
    fi
    
    rollback "$backup_path"
}

main() {
    parse_args "$@"
    
    case $ACTION in
        upgrade)
            do_upgrade
            ;;
        check)
            do_check
            ;;
        list)
            list_releases
            ;;
        current)
            do_current
            ;;
        rollback)
            do_rollback
            ;;
    esac
}

main "$@"
