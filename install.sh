#!/bin/bash
set -e

# ============================================================
# nxpanel 一键安装脚本
#
# 支持三种使用场景：
#   1. 在线安装（从 GitHub Releases 下载最新版本）：
#      curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash
#      sudo bash install.sh
#
#   2. 使用本地 tarball：
#      sudo bash install.sh --local ./nxpanel-linux-amd64.tar.gz
#
#   3. 使用已解压目录：
#      sudo bash install.sh --source-dir /path/to/nxpanel
#      （或在解压目录内直接运行，自动检测）
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

INSTALL_ROOT="/usr/local/nxpanel"
CONFIG_DIR="$INSTALL_ROOT/configs"
DATA_DIR="/opt/nxpanel"
USER="openrest"
API_LISTEN="0.0.0.0:18888"
INTERACTIVE=true
TOKEN=""
INSTALL_WEB_SERVER=""
EXISTING_WEB_SERVER=false
INSECURE_RELAX_SHA1=false
WEB_USER_DECIDED=false
RESOLVED_WEB_USER=""
RESOLVED_WEB_GROUP=""

GITHUB_REPO="luoye663/nxpanel"
TARGET_VERSION=""
LOCAL_TARBALL=""
SOURCE_DIR=""
TEMP_DIR=""

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

confirm_yes() {
    local prompt="$1"
    local default="${2:-n}"
    local reply=""
    local suffix="[y/N]"

    if [ "$INTERACTIVE" != true ]; then
        return 1
    fi

    [ "$default" = "y" ] && suffix="[Y/n]"
    prompt_read "$prompt $suffix " reply "-r"
    echo

    if [ -z "$reply" ]; then
        [ "$default" = "y" ] && return 0 || return 1
    fi

    [[ "$reply" =~ ^[Yy]$ ]]
}

die() {
    log_error "$1"
    exit 1
}

cleanup_temp() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# ============================================================
# OS 检测
# ============================================================

detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        VERSION=$VERSION_ID
    else
        die "无法检测操作系统"
    fi

    case $OS in
        ubuntu|debian)
            PKG_MANAGER="apt"
            ;;
        centos|rhel|fedora|rocky|almalinux|alinux)
            PKG_MANAGER="yum"
            ;;
        *)
            log_warn "未知的操作系统 $OS，可能需要手动安装依赖"
            PKG_MANAGER="unknown"
            ;;
    esac

    if [ "$PKG_MANAGER" = "yum" ] && command -v dnf &>/dev/null; then
        PKG_MANAGER="dnf"
    fi
}

# ============================================================
# 在线模式：从 GitHub 获取最新版本并下载
# ============================================================

fetch_latest_version() {
    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local response
    response=$(curl -fsSL --connect-timeout 10 --max-time 30 "$api_url" 2>/dev/null) || {
        die "无法访问 GitHub API，请检查网络连接或使用 --local 模式"
    }
    TARGET_VERSION=$(echo "$response" | grep -oP '"tag_name"\s*:\s*"\K[^"]+' || true)
    if [ -z "$TARGET_VERSION" ]; then
        die "无法从 GitHub API 解析最新版本号"
    fi
    log_info "最新版本: $TARGET_VERSION"
}

download_release() {
    if [ -z "$TARGET_VERSION" ]; then
        fetch_latest_version
    fi

    TEMP_DIR=$(mktemp -d /tmp/nxpanel-install.XXXXXX)
    trap cleanup_temp EXIT

    local tarball_name="nxpanel-linux-amd64.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${TARGET_VERSION}/${tarball_name}"
    local tarball_path="$TEMP_DIR/${tarball_name}"

    log_step "下载 release ${TARGET_VERSION}..."
    curl -fSL --progress-bar --connect-timeout 10 --max-time 300 -o "$tarball_path" "$download_url" || {
        die "下载失败: $download_url"
    }

    log_step "解压..."
    tar -xzf "$tarball_path" -C "$TEMP_DIR"
    rm -f "$tarball_path"

    local extracted_dir
    extracted_dir=$(find "$TEMP_DIR" -maxdepth 1 -mindepth 1 -type d | head -1)
    if [ -z "$extracted_dir" ]; then
        die "解压后未找到目录"
    fi

    SOURCE_DIR="$extracted_dir"
    log_info "源目录: $SOURCE_DIR"
}

# ============================================================
# 本地 tarball 模式：解压本地压缩包
# ============================================================

extract_local_tarball() {
    if [ ! -f "$LOCAL_TARBALL" ]; then
        die "找不到文件: $LOCAL_TARBALL"
    fi

    TEMP_DIR=$(mktemp -d /tmp/nxpanel-install.XXXXXX)
    trap cleanup_temp EXIT

    log_step "解压 $LOCAL_TARBALL ..."
    tar -xzf "$LOCAL_TARBALL" -C "$TEMP_DIR"

    local extracted_dir
    extracted_dir=$(find "$TEMP_DIR" -maxdepth 1 -mindepth 1 -type d | head -1)
    if [ -z "$extracted_dir" ]; then
        die "解压后未找到目录"
    fi

    SOURCE_DIR="$extracted_dir"
    log_info "源目录: $SOURCE_DIR"
}

# ============================================================
# 自动检测：当前目录是否为已解压的 release 目录
# ============================================================

auto_detect_source_dir() {
    local script_dir
    script_dir="$(cd "$(dirname "$0")" && pwd)"

    if [ -f "$script_dir/bin/nxpanel-api" ]; then
        SOURCE_DIR="$script_dir"
        log_info "检测到当前目录为 release 目录: $SOURCE_DIR"
        return 0
    fi

    if [ -f "$script_dir/../bin/nxpanel-api" ]; then
        SOURCE_DIR="$(cd "$script_dir/.." && pwd)"
        log_info "检测到上级目录为 release 目录: $SOURCE_DIR"
        return 0
    fi

    return 1
}

# ============================================================
# 确定安装源（主分发逻辑）
# ============================================================

determine_source() {
    if [ -n "$SOURCE_DIR" ]; then
        if [ ! -d "$SOURCE_DIR" ]; then
            die "指定目录不存在: $SOURCE_DIR"
        fi
        log_info "使用指定目录: $SOURCE_DIR"
        return
    fi

    if [ -n "$LOCAL_TARBALL" ]; then
        extract_local_tarball
        return
    fi

    if auto_detect_source_dir; then
        return
    fi

    log_step "未检测到本地文件，将从 GitHub 下载..."
    download_release
}

# ============================================================
# 依赖检查（Nginx/OpenResty）
# ============================================================

check_dependencies() {
    log_step "检查系统依赖..."

    if command -v nginx &> /dev/null; then
        log_info "检测到 Nginx: $(command -v nginx)"
        EXISTING_WEB_SERVER=true
        return
    fi

    if command -v openresty &> /dev/null; then
        log_info "检测到 OpenResty: $(command -v openresty)"
        EXISTING_WEB_SERVER=true
        return
    fi

    log_warn "未找到 Nginx 或 OpenResty"

    local install_args=""
    if [ "$INTERACTIVE" = true ]; then
        echo ""
        echo "当前系统未检测到 Nginx 或 OpenResty。"
        echo "你可以现在安装 Web 服务器，也可以先继续安装 nxpanel，后续再自行安装并在面板中配置/检测。"
        prompt_read "是否现在安装 Web 服务器？(y/n): " REPLY "-n 1 -r"
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_warn "跳过 Web 服务器安装，继续安装 nxpanel"
            return
        fi

        echo "请选择要安装的 Web 服务器："
        echo "  1) Nginx（推荐）"
        echo "  2) OpenResty"
        echo "  3) 取消"
        prompt_read "请输入选项 (1-3): " REPLY "-n 1 -r"
        echo
        case $REPLY in
            1) install_args="--nginx" ;;
            2) install_args="--openresty" ;;
            *) log_warn "跳过 Web 服务器安装，继续安装 nxpanel"; return ;;
        esac
    else
        if [ "$INSTALL_WEB_SERVER" = "openresty" ]; then
            install_args="--openresty --non-interactive"
        elif [ "$INSTALL_WEB_SERVER" = "nginx" ]; then
            install_args="--nginx --non-interactive"
        else
            log_warn "非交互模式未指定 --install-nginx/--install-openresty，跳过 Web 服务器安装"
            return
        fi
    fi

    local nginx_script="$SOURCE_DIR/scripts/nginx-install/install.sh"
    if [ ! -f "$nginx_script" ]; then
        die "找不到安装脚本: $nginx_script"
    fi

    local insecure_arg=""
    if [ "$INSECURE_RELAX_SHA1" = "true" ]; then
        insecure_arg="--insecure-relax-sha1"
    fi

    bash "$nginx_script" $install_args $insecure_arg
    if [ $? -ne 0 ]; then
        die "Nginx/OpenResty 安装失败"
    fi
}

# ============================================================
# 安装步骤
# ============================================================

create_user() {
    log_step "[1/7] 创建系统用户..."
    if id "$USER" &>/dev/null; then
        log_warn "用户 $USER 已存在"
    else
        useradd -r -s /sbin/nologin "$USER"
        log_info "用户 $USER 创建成功"
    fi
}

read_api_listen_from_config() {
    local config_file="$1"
    [ -f "$config_file" ] || return 1

    awk '
        /^[[:space:]]*api:[[:space:]]*$/ { in_api=1; next }
        /^[^[:space:]]/ { if (in_api) exit }
        in_api && /^[[:space:]]*listen:[[:space:]]*/ {
            line=$0
            sub(/^[[:space:]]*listen:[[:space:]]*/, "", line)
            gsub(/"/, "", line)
            print line
            exit
        }
    ' "$config_file"
}

read_api_login_path_from_config() {
    local config_file="$1"
    [ -f "$config_file" ] || return 1

    awk '
        /^[[:space:]]*api:[[:space:]]*$/ { in_api=1; next }
        /^[^[:space:]]/ { if (in_api) exit }
        in_api && /^[[:space:]]*login_path:[[:space:]]*/ {
            line=$0
            sub(/^[[:space:]]*login_path:[[:space:]]*/, "", line)
            gsub(/"/, "", line)
            print line
            exit
        }
    ' "$config_file"
}

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
    echo "www-data"
}

get_nginx_version_output() {
    local nginx_bin="$1"
    local output=""

    output=$("$nginx_bin" -V 2>&1 || true)
    if [ -z "$output" ]; then
        log_warn "无法读取 $(basename "$nginx_bin") -V 输出，将使用默认路径和用户" >&2
    fi
    printf '%s\n' "$output"
}

ensure_system_user_group() {
    local name="$1"

    if ! getent group "$name" >/dev/null 2>&1; then
        if command -v groupadd >/dev/null 2>&1; then
            groupadd --system "$name"
        else
            addgroup --system "$name"
        fi
        log_info "系统组 $name 创建成功"
    fi

    if ! getent passwd "$name" >/dev/null 2>&1; then
        if command -v useradd >/dev/null 2>&1; then
            useradd -r -g "$name" -s /usr/sbin/nologin -d /nonexistent "$name" 2>/dev/null \
                || useradd -r -g "$name" -s /sbin/nologin -d /nonexistent "$name"
        else
            adduser --system --ingroup "$name" --no-create-home --disabled-login "$name"
        fi
        log_info "系统用户 $name 创建成功"
    fi
}

maybe_replace_nobody_web_user() {
    local web_user="$1"
    local web_group="$2"
    local reply=""

    if [ "$web_user" != "nobody" ]; then
        RESOLVED_WEB_USER="$web_user"
        RESOLVED_WEB_GROUP="$web_group"
        return
    fi

    log_warn "检测到 Web 用户为 nobody:nobody。生产环境不建议让站点文件归属 nobody，建议切换为 www-data。"
    if [ "$INTERACTIVE" = true ]; then
        prompt_read "是否切换 Web 用户为 www-data？(Y/n): " reply "-n 1 -r"
        echo
        if [[ "$reply" =~ ^[Nn]$ ]]; then
            RESOLVED_WEB_USER="$web_user"
            RESOLVED_WEB_GROUP="$web_group"
            return
        fi
    else
        log_info "非交互模式下自动切换 Web 用户为 www-data"
    fi

    ensure_system_user_group "www-data"
    RESOLVED_WEB_USER="www-data"
    RESOLVED_WEB_GROUP="www-data"
}

resolve_nginx_conf_path() {
    local nginx_bin="$1"
    local v_output="$2"
    local is_openresty="$3"
    local nginx_conf=""

    nginx_conf=$(printf '%s\n' "$v_output" | grep -oP '(?<=--conf-path=)\S+' | head -n 1 || true)
    if [ -z "$nginx_conf" ]; then
        nginx_conf=$("$nginx_bin" -t 2>&1 | grep -oP 'configuration file \K\S+' | head -n 1 || true)
    fi
    if [ -z "$nginx_conf" ]; then
        if [ "$is_openresty" = true ]; then
            nginx_conf="/usr/local/openresty/nginx/conf/nginx.conf"
        else
            nginx_conf="/etc/nginx/nginx.conf"
        fi
    fi

    printf '%s\n' "$nginx_conf"
}

resolve_nginx_web_user() {
    local nginx_conf="$1"
    local v_output="$2"
    local web_user=""
    local web_group=""

    if [ "$WEB_USER_DECIDED" = true ]; then
        return
    fi

    if [ -n "$nginx_conf" ] && [ -f "$nginx_conf" ]; then
        web_user=$(grep -E '^[[:space:]]*user[[:space:]]+' "$nginx_conf" | head -1 | awk '{print $2}' | tr -d ';' || true)
        web_group=$(grep -E '^[[:space:]]*user[[:space:]]+' "$nginx_conf" | head -1 | awk '{print $3}' | tr -d ';' || true)
        [ -z "$web_group" ] && web_group="$web_user"
    fi

    if [ -z "$web_user" ]; then
        web_user=$(printf '%s\n' "$v_output" | grep -oP '(?<=--user=)\S+' | head -n 1 || true)
        web_group=$(printf '%s\n' "$v_output" | grep -oP '(?<=--group=)\S+' | head -n 1 || true)
        [ -n "$web_user" ] && [ -z "$web_group" ] && web_group="$web_user"
    fi

    if [ -z "$web_user" ]; then
        web_user=$(resolve_web_user_default)
        web_group="$web_user"
    fi

    maybe_replace_nobody_web_user "$web_user" "${web_group:-$web_user}"
    WEB_USER_DECIDED=true
}

write_api_listen_to_config() {
    local config_file="$1"
    local api_listen="$2"
    [ -f "$config_file" ] || return 1
    sed -i -E "s|^([[:space:]]*listen:).*$|\1 \"$api_listen\"|" "$config_file"
}

write_api_login_path_to_config() {
    local config_file="$1"
    local login_path="$2"
    [ -f "$config_file" ] || return 1
    sed -i -E "s|^([[:space:]]*login_path:).*$|\1 \"$login_path\"|" "$config_file"
}

validate_login_path() {
    local login_path="$1"
    local token=""

    [ -n "$login_path" ] || return 1
    [[ "$login_path" == /* ]] || return 1

    case "$login_path" in
        /|/setup|/login|/api|/assets|/health|/auth) return 1 ;;
    esac

    token="${login_path#/}"
    [[ "$token" != */* ]] || return 1
    [ ${#token} -ge 8 ] || return 1
    [ ${#token} -le 64 ] || return 1
    [[ "$token" =~ ^[A-Za-z0-9_-]+$ ]] || return 1
}

generate_login_path() {
    local alphabet="0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    local nums=""
    local path="/"
    local n idx

    if command -v openssl >/dev/null 2>&1; then
        nums=$(openssl rand 16 2>/dev/null | od -An -tu1 2>/dev/null || true)
    fi
    if [ -z "$nums" ] && [ -r /dev/urandom ]; then
        nums=$(od -An -N16 -tu1 /dev/urandom 2>/dev/null || true)
    fi
    [ -n "$nums" ] || return 1

    for n in $nums; do
        idx=$((n % ${#alphabet}))
        path+="${alphabet:$idx:1}"
        [ ${#path} -ge 17 ] && break
    done

    path=$(printf '%s' "$path" | tr '[:upper:]' '[:lower:]')
    validate_login_path "$path" || return 1
    printf '%s\n' "$path"
}

extract_listen_port() {
    local listen="$1"
    local port="${listen##*:}"
    if [[ "$port" =~ ^[0-9]+$ ]]; then
        echo "$port"
    fi
}

extract_listen_host() {
    local listen="$1"
    local fallback_host="$2"
    if [[ "$listen" =~ ^\[[^]]+\]:[0-9]+$ || "$listen" =~ ^[^:]+:[0-9]+$ ]]; then
        echo "${listen%:*}"
        return
    fi
    echo "${fallback_host:-0.0.0.0}"
}

normalize_api_listen() {
    local value="$1"
    local fallback_host="$2"
    value="${value//[[:space:]]/}"
    if [[ "$value" =~ ^[0-9]+$ ]]; then
        echo "${fallback_host:-0.0.0.0}:$value"
        return
    fi
    if [[ "$value" =~ ^\[[^]]+\]:[0-9]+$ || "$value" =~ ^[^:]+:[0-9]+$ ]]; then
        echo "$value"
        return
    fi
}

PORT_OWNER_PID=""
PORT_OWNER_PROC=""
PORT_OWNER_RAW=""

detect_listen_port_owner() {
    local port="$1"
    local line pid proc

    PORT_OWNER_PID=""
    PORT_OWNER_PROC=""
    PORT_OWNER_RAW=""

    if command -v ss >/dev/null 2>&1; then
        line=$(ss -ltnpH 2>/dev/null | awk -v port="$port" '
            {
                local_addr=$4
                sub(/^.*:/, "", local_addr)
                if (local_addr == port) {
                    print
                    exit
                }
            }
        ' || true)
        if [ -n "$line" ]; then
            pid=$(printf '%s\n' "$line" | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -n 1 || true)
            proc=$(printf '%s\n' "$line" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n 1 || true)
            PORT_OWNER_PID="${pid:-未知}"
            PORT_OWNER_PROC="${proc:-未知}"
            PORT_OWNER_RAW="$line"
            log_info "ss 命中端口 $port: $line"
            return 0
        fi
        log_info "ss 未命中端口 $port，尝试使用 lsof"
    fi

    if command -v lsof >/dev/null 2>&1; then
        line=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR==2 { print $1 "|" $2 }' || true)
        if [ -n "$line" ]; then
            proc=${line%%|*}
            pid=${line##*|}
            PORT_OWNER_PID="${pid:-未知}"
            PORT_OWNER_PROC="${proc:-未知}"
            PORT_OWNER_RAW="$line"
            log_info "lsof 命中端口 $port: $line"
            return 0
        fi
        log_info "lsof 未命中端口 $port"
    fi

    return 1
}

ensure_api_listen_available() {
    local config_file="$CONFIG_DIR/config.yaml"
    local current_listen host port new_listen

    if [ -f "$config_file" ]; then
        current_listen=$(read_api_listen_from_config "$config_file" || true)
        if [ -n "$current_listen" ]; then
            API_LISTEN="$current_listen"
        fi
    fi

    host=$(extract_listen_host "$API_LISTEN" "0.0.0.0")

    while true; do
        port=$(extract_listen_port "$API_LISTEN")
        if [ -z "$port" ]; then
            log_warn "无法解析 API 监听地址: $API_LISTEN，跳过端口占用检查"
            return 0
        fi

        if ! detect_listen_port_owner "$port"; then
            return 0
        fi

        log_warn "API 监听端口 $port 已被占用"
        log_warn "占用进程: PID=${PORT_OWNER_PID:-未知}, 进程名=${PORT_OWNER_PROC:-未知}"

        if [ "$INTERACTIVE" = true ]; then
            prompt_read "请输入新的 API 监听地址或端口（例如 127.0.0.1:18889 或 18889）: " new_listen "-r"
            if [ -z "$new_listen" ]; then
                continue
            fi

            new_listen=$(normalize_api_listen "$new_listen" "$host")
            if [ -z "$new_listen" ]; then
                log_warn "输入无效，请输入如 127.0.0.1:18889 或 18889"
                continue
            fi

            API_LISTEN="$new_listen"
            write_api_listen_to_config "$config_file" "$API_LISTEN"
            host=$(extract_listen_host "$API_LISTEN" "$host")
            continue
        fi

        log_warn "API 监听端口 $port 已被占用"
        log_warn "占用进程: PID=${PORT_OWNER_PID:-未知}, 进程名=${PORT_OWNER_PROC:-未知}"
        die "请使用 --api-listen 指定新端口后重试"
    done
}

stop_existing_services() {
    log_step "停止现有 nxpanel 服务..."

    if ! command -v systemctl &>/dev/null || [ ! -d /run/systemd/system ]; then
        log_warn "systemd 不可用，跳过停止现有服务"
        return
    fi

    local svc
    for svc in nxpanel-api nxpanel-agent; do
        if ! systemctl cat "$svc" >/dev/null 2>&1; then
            log_info "$svc 未安装，跳过"
            continue
        fi

        if systemctl is-active --quiet "$svc"; then
            log_info "正在停止 $svc..."
            if systemctl stop "$svc"; then
                log_info "$svc 已停止"
            else
                log_warn "$svc 停止失败，继续安装可能失败"
            fi
        else
            log_info "$svc 未运行"
        fi
    done
}

create_directories() {
    log_step "[2/7] 创建目录..."
    mkdir -p "$INSTALL_ROOT"/{bin,web}
    mkdir -p "$CONFIG_DIR/templates"
	mkdir -p "$DATA_DIR"/{data/tls,nginx,logs}
    mkdir -p /run/nxpanel
    chown -R "$USER:$USER" "$DATA_DIR"
    log_info "目录创建成功"
}

copy_files() {
    log_step "[3/7] 复制文件..."

    if [ ! -f "$SOURCE_DIR/bin/nxpanel-api" ]; then
        die "找不到 nxpanel-api 二进制（源目录: $SOURCE_DIR）"
    fi

    install -m 755 "$SOURCE_DIR/bin/nxpanel-api" "$INSTALL_ROOT/bin/"
    install -m 755 "$SOURCE_DIR/bin/nxpanel-agent" "$INSTALL_ROOT/bin/"

    if [ -d "$SOURCE_DIR/web" ]; then
        cp -r "$SOURCE_DIR/web/"* "$INSTALL_ROOT/web/"
    else
        die "找不到 web 目录（源目录: $SOURCE_DIR）"
    fi

    if [ -f "$SOURCE_DIR/configs/config.example.yaml" ]; then
        cp "$SOURCE_DIR/configs/config.example.yaml" "$CONFIG_DIR/"
    fi

    if [ -d "$SOURCE_DIR/configs/templates" ]; then
        cp -r "$SOURCE_DIR/configs/templates/"* "$CONFIG_DIR/templates/"
    fi

    if [ -d "$SOURCE_DIR/configs/nginx" ]; then
        mkdir -p "$CONFIG_DIR/nginx"
        cp -r "$SOURCE_DIR/configs/nginx/"* "$CONFIG_DIR/nginx/"
    fi

    log_info "文件复制成功"
}

generate_config() {
    log_step "[4/7] 生成配置文件..."

    local config_file="$CONFIG_DIR/config.yaml"

    if [ -f "$config_file" ]; then
        if [ "$INTERACTIVE" = true ]; then
            prompt_read "配置文件已存在，是否覆盖？(y/n): " REPLY "-n 1 -r"
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                log_warn "跳过配置文件生成"
                return
            fi
        else
            log_warn "配置文件已存在，跳过生成"
            return
        fi
    fi

    if [ -f "$CONFIG_DIR/config.example.yaml" ]; then
        mv "$CONFIG_DIR/config.example.yaml" "$config_file"
    fi

    if [ -z "$TOKEN" ]; then
        TOKEN=$(openssl rand -base64 32)
    fi

    if [ -f "$config_file" ]; then
        local loginPath=""
        loginPath=$(generate_login_path || true)

        sed -i "s|change-me-in-production|$TOKEN|" "$config_file"
        sed -i "s|data_dir: .*|data_dir: $DATA_DIR/data|" "$config_file"
        sed -i "s|panel_dir: .*|panel_dir: $DATA_DIR/nginx|" "$config_file"
        write_api_listen_to_config "$config_file" "$API_LISTEN"
        if [ -n "$loginPath" ]; then
            write_api_login_path_to_config "$config_file" "$loginPath"
            log_info "登录入口: $loginPath"
        else
            log_warn "随机登录入口生成失败，将由 nxpanel-api 启动时自动生成并写回"
        fi

        local nginxBin=""
        local nginxConf=""
        local nginxVOutput=""
        local webUser=""
        local webGroup=""
        local isOpenresty=false

        if command -v nginx &>/dev/null; then
            nginxBin=$(command -v nginx)
        elif command -v openresty &>/dev/null; then
            nginxBin=$(command -v openresty)
            isOpenresty=true
        fi

        if [ -n "$nginxBin" ]; then
            nginxVOutput=$(get_nginx_version_output "$nginxBin")
            nginxConf=$(resolve_nginx_conf_path "$nginxBin" "$nginxVOutput" "$isOpenresty")
            resolve_nginx_web_user "$nginxConf" "$nginxVOutput"
            webUser="$RESOLVED_WEB_USER"
            webGroup="$RESOLVED_WEB_GROUP"

            sed -i "s|bin:.*|bin: \"$nginxBin\"|" "$config_file"
            [ -n "$nginxConf" ] && sed -i "s|conf_path:.*|conf_path: \"$nginxConf\"|" "$config_file"
            [ -n "$webUser" ] && sed -i "s|web_user:.*|web_user: $webUser|" "$config_file"
            [ -n "$webGroup" ] && sed -i "s|web_group:.*|web_group: $webGroup|" "$config_file"

            log_info "Nginx 路径: $nginxBin"
            [ -n "$nginxConf" ] && log_info "配置文件: $nginxConf"
            [ -n "$webUser" ] && log_info "Web 用户: $webUser:$webGroup"
        fi

        log_info "配置文件生成成功"
        log_info "Agent Token: $TOKEN"
    else
        die "配置文件生成失败"
    fi
}

deploy_nginx_conf() {
    log_step "[5/7] 部署 nginx 配置模板..."

    local nginxBin=""
    local nginxConf=""
    local isOpenresty=false

    if command -v nginx &>/dev/null; then
        nginxBin=$(command -v nginx)
    elif command -v openresty &>/dev/null; then
        nginxBin=$(command -v openresty)
        isOpenresty=true
    fi

    if [ -z "$nginxBin" ]; then
        log_warn "未检测到 nginx 或 openresty，跳过配置部署"
        return
    fi

    local v_output
    v_output=$(get_nginx_version_output "$nginxBin")

    nginxConf=$(resolve_nginx_conf_path "$nginxBin" "$v_output" "$isOpenresty")

    local nginxConfDir
    nginxConfDir=$(dirname "$nginxConf")

    local pid_path
    pid_path=$(echo "$v_output" | grep -oP '(?<=--pid-path=)\S+' | head -n 1 || true)
    [ -z "$pid_path" ] && pid_path="/var/run/nginx.pid"

    local error_log_path
    error_log_path=$(echo "$v_output" | grep -oP '(?<=--error-log-path=)\S+' | head -n 1 || true)
    [ -z "$error_log_path" ] && error_log_path="/var/log/nginx/error.log"

    local nginx_was_running=false
    if pgrep -x "$(basename "$nginxBin")" >/dev/null 2>&1; then
        nginx_was_running=true
    fi

    # 已有 Web 服务器时默认不接管系统 nginx.conf。
    # 面板只管理自身目录下的配置；如需重装/接管，请单独运行 scripts/nginx-install/install.sh。
    if [ "$EXISTING_WEB_SERVER" = true ]; then
        log_info "检测到已有 $(basename "$nginxBin")，跳过 nginx.conf 覆盖（如需接管请使用 scripts/nginx-install/install.sh）"
        return
    fi

    # 备份
    if [ -f "$nginxConf" ]; then
        local backup="${nginxConf}.bak.$(date +%Y%m%d%H%M%S)"
        cp "$nginxConf" "$backup"
        log_info "已备份原始配置: $backup"
    else
        log_info "目标文件不存在，将创建新文件: $nginxConf"
    fi

    # 如果目标路径异常为目录（可能来自之前失败的安装），先删除
    if [ -d "$nginxConf" ]; then
        log_warn "$nginxConf 是目录（异常状态），正在删除..."
        rm -rf "$nginxConf"
    fi

    # 选择模板
    local template=""
    if [ "$isOpenresty" = true ]; then
        template="$CONFIG_DIR/nginx/nginx.conf.openresty"
    else
        template="$CONFIG_DIR/nginx/nginx.conf.production"
    fi

    if [ ! -f "$template" ]; then
        log_warn "未找到配置模板: $template，跳过部署"
        return
    fi
    log_info "使用模板: $template"

    # 确定 web user
    local webUser=""
    local webGroup=""
    resolve_nginx_web_user "$nginxConf" "$v_output"
    webUser="$RESOLVED_WEB_USER"
    webGroup="$RESOLVED_WEB_GROUP"
    log_info "Web 用户: $webUser:$webGroup"

    # --- 创建必要目录 ---
    mkdir -p "$nginxConfDir"
    mkdir -p "$nginxConfDir/conf.d"

    if [ "$isOpenresty" = false ]; then
        mkdir -p "$nginxConfDir/sites-enabled"
    fi

    local log_dir
    log_dir=$(dirname "$error_log_path")
    mkdir -p "$log_dir"
    chown "$webUser:$webGroup" "$log_dir" 2>/dev/null || true

    mkdir -p "$DATA_DIR/nginx"/{conf.d,sites-available,sites-enabled,rewrite,ssl,ssl-store,backups,access-limit,htpasswd,proxy/cache}

    # --- 清理无效 PID 文件 ---
    if [ -f "$pid_path" ]; then
        local old_pid
        old_pid=$(cat "$pid_path" 2>/dev/null || true)
        if [ -z "$old_pid" ]; then
            rm -f "$pid_path"
            log_info "已清理空 PID 文件: $pid_path"
        elif ! kill -0 "$old_pid" 2>/dev/null; then
            rm -f "$pid_path"
            log_info "已清理无效 PID 文件: $pid_path (旧 PID: $old_pid)"
        fi
    fi

    # --- 写入配置 ---
    local content
    content=$(cat "$template")
    content="${content//\{\{WEB_USER\}\}/$webUser}"
    content="${content//\{\{PANEL_DIR\}\}/$DATA_DIR/nginx}"
    content="${content//\{\{PID_PATH\}\}/$pid_path}"
    content="${content//\{\{ERROR_LOG_PATH\}\}/$error_log_path}"

    echo "$content" > "$nginxConf"

    # 写入后验证
    if [ ! -f "$nginxConf" ]; then
        log_error "部署失败: $nginxConf 写入后不存在"
        return
    fi
    local first_line
    first_line=$(head -1 "$nginxConf")
    log_info "已部署 nginx 配置: $nginxConf (首行: $first_line)"

    # --- 验证配置 ---
    if $nginxBin -t -c "$nginxConf" 2>&1; then
        log_info "nginx -t 验证通过"
    else
        log_warn "nginx -t 验证失败，请检查 $nginxConf"
    fi

    # --- 如果 nginx 之前在运行，重载配置 ---
    if [ "$nginx_was_running" = true ]; then
        log_info "重载 nginx 配置..."
        if systemctl is-active --quiet nginx 2>/dev/null; then
            systemctl reload nginx 2>/dev/null && log_info "nginx 重载成功 (systemctl)" || log_warn "nginx 重载失败"
        elif systemctl is-active --quiet openresty 2>/dev/null; then
            systemctl reload openresty 2>/dev/null && log_info "openresty 重载成功 (systemctl)" || log_warn "openresty 重载失败"
        else
            $nginxBin -c "$nginxConf" -s reload 2>/dev/null && log_info "nginx 重载成功" || log_warn "nginx 重载失败，可能需要手动重启"
        fi
    fi
}

install_services() {
    log_step "[6/7] 安装 systemd 服务..."

    local agent_service="$SOURCE_DIR/scripts/nxpanel-agent.service"
    local api_service="$SOURCE_DIR/scripts/nxpanel-api.service"

    if [ ! -f "$agent_service" ] || [ ! -f "$api_service" ]; then
        die "找不到 systemd 服务文件（$SOURCE_DIR/scripts/*.service）"
    fi

    sed \
        -e "s|/usr/local/nxpanel/bin/nxpanel-agent -config /usr/local/nxpanel/configs/config.yaml|$INSTALL_ROOT/bin/nxpanel-agent -config $CONFIG_DIR/config.yaml|" \
        "$agent_service" > /etc/systemd/system/nxpanel-agent.service

    sed \
        -e "s|/usr/local/nxpanel/bin/nxpanel-api -config /usr/local/nxpanel/configs/config.yaml|$INSTALL_ROOT/bin/nxpanel-api -config $CONFIG_DIR/config.yaml|" \
        -e "s|ReadWritePaths=.*|ReadWritePaths=$DATA_DIR /run/nxpanel|" \
        "$api_service" > /etc/systemd/system/nxpanel-api.service

    systemctl daemon-reload
    log_info "systemd 服务安装成功"
}

start_services() {
    log_step "[7/7] 启动服务..."

    systemctl enable nxpanel-agent
    systemctl start nxpanel-agent

    systemctl enable nxpanel-api
    systemctl start nxpanel-api

    sleep 2
    if systemctl is-active --quiet nxpanel-agent; then
        log_info "nxpanel-agent 启动成功"
    else
        log_error "nxpanel-agent 启动失败"
        systemctl status nxpanel-agent
    fi

    if systemctl is-active --quiet nxpanel-api; then
        log_info "nxpanel-api 启动成功"
    else
        log_error "nxpanel-api 启动失败"
        systemctl status nxpanel-api
    fi
}

show_completion() {
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}安装完成！${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "安装目录: $INSTALL_ROOT"
    echo "配置文件: $CONFIG_DIR/config.yaml"
    echo "数据目录: $DATA_DIR"
    echo "Agent Token: $TOKEN"
    echo ""
    local config_api_listen=""
    config_api_listen=$(read_api_listen_from_config "$CONFIG_DIR/config.yaml" || true)
    [ -n "$config_api_listen" ] && API_LISTEN="$config_api_listen"
    local login_path=""
    login_path=$(read_api_login_path_from_config "$CONFIG_DIR/config.yaml" || true)
    validate_login_path "$login_path" || login_path=""
    local api_port=""
    api_port=$(extract_listen_port "$API_LISTEN" || true)
    echo "访问地址: https://IP:${api_port:-18888}$login_path"
    echo "TLS 证书: $DATA_DIR/data/tls/api.crt（自签名，浏览器会显示安全警告）"
    echo "HTTP→HTTPS 重定向: :${api_port:-18888} → https://IP:${api_port:-18888}$login_path"
    echo ""
    echo "常用命令："
    echo "  查看日志: journalctl -u nxpanel-api -f"
    echo "  重启服务: systemctl restart nxpanel-api nxpanel-agent"
    echo "  停止服务: systemctl stop nxpanel-api nxpanel-agent"
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
            --token)
                TOKEN="$2"
                shift 2
                ;;
            --api-listen)
                API_LISTEN="$2"
                shift 2
                ;;
            --install-dir)
                INSTALL_ROOT="$2"
                CONFIG_DIR="$INSTALL_ROOT/configs"
                shift 2
                ;;
            --data-dir)
                DATA_DIR="$2"
                shift 2
                ;;
            --install-nginx)
                INSTALL_WEB_SERVER="nginx"
                shift
                ;;
            --install-openresty)
                INSTALL_WEB_SERVER="openresty"
                shift
                ;;
            --insecure-relax-sha1)
                INSECURE_RELAX_SHA1=true
                shift
                ;;
            --local)
                LOCAL_TARBALL="$2"
                shift 2
                ;;
            --source-dir)
                SOURCE_DIR="$2"
                shift 2
                ;;
            --version)
                TARGET_VERSION="$2"
                shift 2
                ;;
            --repo)
                GITHUB_REPO="$2"
                shift 2
                ;;
            --help)
                echo "用法: $0 [选项]"
                echo ""
                echo "安装源:"
                echo "  (无参数)              在线安装，从 GitHub 下载最新 release"
                echo "  --local <tarball>     使用本地 tarball 安装"
                echo "  --source-dir <dir>    使用已解压的目录安装"
                echo ""
                echo "安装选项:"
                echo "  --non-interactive     非交互式模式"
                echo "  --token TOKEN         指定 Agent Token"
                echo "  --api-listen ADDR     API 监听地址 (默认: 0.0.0.0:18888)"
                echo "  --install-dir DIR     安装根目录 (默认: /usr/local/nxpanel)"
                echo "  --data-dir DIR        数据目录 (默认: /opt/nxpanel)"
                echo "  --install-nginx       未检测到 Web 服务器时安装 Nginx（非交互模式需显式指定）"
                echo "  --install-openresty   未检测到 Web 服务器时安装 OpenResty（非交互模式需显式指定）"
                echo "  --insecure-relax-sha1 透传给 install-nginx.sh：Debian 13 + OpenResty 时临时放宽 SHA1 策略"
                echo ""
                echo "在线模式选项:"
                echo "  --version <tag>       安装指定版本 (默认: latest)"
                echo "  --repo <owner/repo>   GitHub 仓库 (默认: $GITHUB_REPO)"
                echo ""
                echo "示例:"
                echo "  sudo bash $0                                              # 在线安装最新版"
                echo "  sudo bash $0 --version v1.2.0                             # 安装指定版本"
                echo "  sudo bash $0 --local ./nxpanel-linux-amd64.tar.gz  # 本地 tarball"
                echo "  sudo bash $0 --source-dir ./nxpanel                # 已解压目录"
                echo "  sudo bash $0 --non-interactive                            # 跳过 Web 服务器安装，只安装 nxpanel"
                echo "  sudo bash $0 --non-interactive --install-nginx            # 同时安装 Nginx"
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
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}nxpanel 安装脚本${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""

    if [[ $EUID -ne 0 ]]; then
        die "请使用 root 权限运行此脚本"
    fi

    parse_args "$@"
    ensure_interactive_input

    detect_os
    log_info "检测到操作系统: $OS $VERSION"

    determine_source

    check_dependencies

    if [ "$INTERACTIVE" = true ]; then
        echo ""
        echo -e "${YELLOW}当前配置：${NC}"
        echo "  安装目录: $INSTALL_ROOT"
        echo "  配置目录: $CONFIG_DIR"
        echo "  数据目录: $DATA_DIR"
        echo "  API 监听: $API_LISTEN"
        echo "  源目录:   $SOURCE_DIR"
        echo ""
        if ! confirm_yes "是否继续？" "n"; then
            echo "安装已取消"
            exit 0
        fi
    fi

    stop_existing_services
    create_user
    create_directories
    copy_files
    generate_config
    ensure_api_listen_available
    deploy_nginx_conf
    install_services
    start_services

    show_completion
}

main "$@"
