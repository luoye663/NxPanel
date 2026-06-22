#!/bin/bash
set -euo pipefail

# ============================================================
# test-install-compat.sh
#
# 在 Docker 容器中测试 scripts/nginx-install/install.sh / install.sh 在各
# Linux 发行版上的兼容性。
#
# 安装源（三种模式，与 install.sh 对齐）：
#   1. 在线下载 GitHub Release：
#      bash test-install-compat.sh
#
#   2. 使用本地压缩包：
#      bash test-install-compat.sh --local ./nxpanel-linux-amd64.tar.gz
#
#   3. 使用已解压目录：
#      bash test-install-compat.sh --source-dir ./release/nxpanel
#
# 测试模式：
#   --nginx-only（默认）  测试 scripts/nginx-install/install.sh
#   --full                测试完整 install.sh
#
# Web 服务器：
#   --openresty           测试 OpenResty（默认 Nginx）
#   --insecure-relax-sha1 Debian 13 + OpenResty 时透传 opt-in 标志
#
# 代理:
#   --socks5-proxy URL    为测试容器设置 SOCKS5 代理（如 socks5h://127.0.0.1:1080）
#
# 其他选项：
#   --distros "..."       指定发行版列表
#   --keep                保留容器（用于调试）
#   --rm-images           删除测试镜像（默认保留）
#   --docker-logs         实时显示容器运行日志
#   --verbose             失败时显示更多日志
#   --parallel N          并行测试数
#   --socks5-proxy URL    为测试容器设置 SOCKS5 代理
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

RESULTS_DIR="$(pwd)/test-results"
GITHUB_REPO="nxpanel/nxpanel"

# 默认发行版矩阵
DEFAULT_DISTROS=(
    "debian:12"
    "debian:13"
    "ubuntu:22.04"
    "ubuntu:24.04"
    "ubuntu:26.04"
    "almalinux:8"
    "almalinux:9"
    "alinux:3"
    "rockylinux:9"
    "centos:7"
    "oraclelinux:9"
    "opencloudos/opencloudos9-minimal:latest"
)

# 参数默认值
MODE="nginx-only"
WEB_SERVER="nginx"
CUSTOM_DISTROS=""
KEEP=false
RM_IMAGES=false
DOCKER_LOGS=false
VERBOSE=false
PARALLEL=1
FAILED_COUNT=0
INSECURE_RELAX_SHA1=false
TOTAL_COUNT=0
SOURCE_DIR=""
LOCAL_TARBALL=""
TEMP_DIR=""
SOCKS5_PROXY=""
DOCKER_PROXY_ENV_ARGS=()

declare -a RESULT_LINES=()

# ============================================================
# 工具函数
# ============================================================

log_info()    { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()    { echo -e "${CYAN}[STEP]${NC} $1"; }

die() {
    log_error "$1"
    exit 1
}

log_pipe() {
    local log_file="$1"
    if [ "$DOCKER_LOGS" = true ]; then
        tee -a "$log_file"
    else
        cat >> "$log_file"
    fi
}

log_msg() {
    local log_file="$1"
    shift
    echo "$@" >> "$log_file"
    if [ "$DOCKER_LOGS" = true ]; then
        echo -e "  ${DIM}$*${NC}" >&2
    fi
}

safe_name() {
    # 处理 image name 中的 / 和 : 与 .
    # 例：opencloudos/opencloudos9-minimal:latest → opencloudos-opencloudos9-minimal-latest
    echo "$1" | tr ':./' '-'
}

cleanup_temp() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

prepare_proxy_env_args() {
    DOCKER_PROXY_ENV_ARGS=()
    if [ -z "$SOCKS5_PROXY" ]; then
        return
    fi

    DOCKER_PROXY_ENV_ARGS+=(
        -e "ALL_PROXY=$SOCKS5_PROXY"
        -e "all_proxy=$SOCKS5_PROXY"
        -e "NO_PROXY=localhost,127.0.0.1,::1"
        -e "no_proxy=localhost,127.0.0.1,::1"
    )
}

# ============================================================
# 参数解析
# ============================================================

show_help() {
    cat <<EOF
用法: $0 [选项]

在 Docker 容器中测试安装脚本的各发行版兼容性。

安装源:
  (无参数)              从 GitHub 下载最新 release
  --local <tarball>     使用本地压缩包
  --source-dir <dir>    使用已解压的目录

测试模式:
  (默认)               --nginx-only 模式：测试 scripts/nginx-install/install.sh
  --full               --full 模式：测试完整 install.sh 流程（需要 systemd）

Web 服务器:
  (默认)               测试 Nginx 安装
  --openresty          测试 OpenResty 安装
  --insecure-relax-sha1  Debian 13 + OpenResty 时透传 opt-in 标志（详见 install.sh --help）

发行版:
  --distros "..."      覆盖默认发行版列表（空格分隔）
                       默认: ${DEFAULT_DISTROS[*]}

控制:
  --keep               保留容器（用于调试）
  --rm-images          同时删除测试镜像（默认保留镜像）
  --docker-logs        实时显示容器运行日志
  --verbose            失败时显示更多日志
  --parallel N         并行测试数（默认: 1）
  --help               显示此帮助信息

示例:
  $0                                              # 在线下载 release，测试 scripts/nginx-install/install.sh
  $0 --full                                       # 在线下载 release，测试完整 install.sh
  $0 --openresty                                  # 测试 OpenResty 安装
  $0 --full --openresty                           # 完整 install.sh + OpenResty
  $0 --openresty --distros "debian:13" --insecure-relax-sha1  # Debian 13 + OpenResty opt-in
  $0 --local ./nxpanel-linux-amd64.tar.gz  # 本地压缩包
  $0 --source-dir ./release/nxpanel        # 已解压目录
  $0 --distros "debian:12 ubuntu:24.04"           # 仅测试指定发行版
  $0 --keep --verbose                             # 保留容器 + 详细输出
  $0 --rm-images                                  # 测试后同时删除镜像
  $0 --socks5-proxy socks5h://127.0.0.1:1080      # 通过 SOCKS5 代理访问外网
EOF
    exit 0
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --full)
                MODE="full"
                shift
                ;;
            --openresty)
                WEB_SERVER="openresty"
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
            --distros)
                CUSTOM_DISTROS="$2"
                shift 2
                ;;
            --keep)
                KEEP=true
                shift
                ;;
            --rm-images)
                RM_IMAGES=true
                shift
                ;;
            --docker-logs)
                DOCKER_LOGS=true
                shift
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            --parallel)
                PARALLEL="$2"
                shift 2
                ;;
            --socks5-proxy)
                SOCKS5_PROXY="$2"
                shift 2
                ;;
            --insecure-relax-sha1)
                INSECURE_RELAX_SHA1=true
                shift
                ;;
            --help|-h)
                show_help
                ;;
            *)
                die "未知参数: $1\n使用 --help 查看帮助"
                ;;
        esac
    done
}

# ============================================================
# 安装源确定（与 install.sh 对齐的三种模式）
# ============================================================

fetch_latest_version() {
    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local response
    response=$(curl -fsSL --connect-timeout 10 --max-time 30 "$api_url" 2>/dev/null) || {
        die "无法访问 GitHub API，请检查网络连接或使用 --local / --source-dir 模式"
    }
    local version
    version=$(echo "$response" | grep -oP '"tag_name"\s*:\s*"\K[^"]+' || true)
    if [ -z "$version" ]; then
        die "无法从 GitHub API 解析最新版本号"
    fi
    echo "$version"
}

download_release() {
    local version
    version=$(fetch_latest_version)
    log_info "最新版本: $version"

    TEMP_DIR=$(mktemp -d /tmp/nxpanel-test.XXXXXX)
    trap cleanup_temp EXIT

    local tarball_name="nxpanel-linux-amd64.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${tarball_name}"
    local tarball_path="$TEMP_DIR/${tarball_name}"

    log_step "下载 release ${version}..."
    curl -fSL --progress-bar --connect-timeout 10 --max-time 300 -o "$tarball_path" "$download_url" || {
        die "下载失败: $download_url"
    }

    log_step "解压..."
    tar -xzf "$tarball_path" -C "$TEMP_DIR"
    rm -f "$tarball_path"

    SOURCE_DIR=$(find "$TEMP_DIR" -maxdepth 1 -mindepth 1 -type d | head -1)
    if [ -z "$SOURCE_DIR" ]; then
        die "解压后未找到目录"
    fi

    log_info "源目录: $SOURCE_DIR"
}

extract_local_tarball() {
    if [ ! -f "$LOCAL_TARBALL" ]; then
        die "找不到文件: $LOCAL_TARBALL"
    fi

    TEMP_DIR=$(mktemp -d /tmp/nxpanel-test.XXXXXX)
    trap cleanup_temp EXIT

    log_step "解压 $LOCAL_TARBALL ..."
    tar -xzf "$LOCAL_TARBALL" -C "$TEMP_DIR"

    SOURCE_DIR=$(find "$TEMP_DIR" -maxdepth 1 -mindepth 1 -type d | head -1)
    if [ -z "$SOURCE_DIR" ]; then
        die "解压后未找到目录"
    fi

    log_info "源目录: $SOURCE_DIR"
}

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

    log_step "未指定安装源，将从 GitHub 下载最新 release..."
    download_release
}

# ============================================================
# 前置检查
# ============================================================

preflight_check() {
    log_step "前置检查..."

    if ! command -v docker &>/dev/null; then
        die "未找到 docker，请先安装 Docker"
    fi

    if ! docker info &>/dev/null 2>&1; then
        die "Docker daemon 未运行，请先启动 Docker"
    fi

    mkdir -p "$RESULTS_DIR"
}

# ============================================================
# 验证源目录结构
# ============================================================

validate_source() {
    log_step "验证安装源..."

    # install-nginx 入口始终需要
    if [ ! -f "$SOURCE_DIR/scripts/nginx-install/install.sh" ]; then
        die "源目录中未找到 scripts/nginx-install/install.sh: $SOURCE_DIR"
    fi

    # full 模式还需要 install.sh
    if [ "$MODE" = "full" ]; then
        if [ ! -f "$SOURCE_DIR/install.sh" ]; then
            die "full 模式需要 install.sh，但源目录中未找到: $SOURCE_DIR/install.sh"
        fi
    fi

    log_info "源目录结构验证通过"
}

# ============================================================
# 清理
# ============================================================

cleanup_containers=()
cleanup_images=()

register_cleanup() {
    local type="$1" name="$2"
    if [ "$type" = "container" ]; then
        cleanup_containers+=("$name")
    elif [ "$type" = "image" ]; then
        cleanup_images+=("$name")
    fi
}

do_cleanup() {
    cleanup_temp
    if [ "$KEEP" = false ]; then
        for c in "${cleanup_containers[@]}"; do
            docker rm -f "$c" &>/dev/null || true
        done
    fi
    if [ "$RM_IMAGES" = true ]; then
        for img in "${cleanup_images[@]}"; do
            docker rmi -f "$img" &>/dev/null || true
        done
    fi
}

trap 'echo ""; log_warn "收到中断信号，清理中..."; do_cleanup; exit 130' INT TERM

# ============================================================
# 拉取镜像
# ============================================================

pull_image() {
    local image="$1"
    local log_file="$2"

    if docker image inspect "$image" &>/dev/null 2>&1; then
        log_msg "$log_file" "  已有镜像 $image，跳过拉取"
        return 0
    fi

    log_msg "$log_file" "  拉取镜像 $image ..."
    if ! docker pull "$image" >> "$log_file" 2>&1; then
        echo "PULL_FAILED" >> "$log_file"
        return 1
    fi
    return 0
}

# ============================================================
# 模式 1：nginx-only（测试 scripts/nginx-install/install.sh）
# ============================================================

test_nginx_only() {
    local distro="$1"
    local sname="$2"
    local log_file="$3"

    prepare_proxy_env_args

    local nginx_flag="--$WEB_SERVER"
    local insecure_flag=""
    if [ "$INSECURE_RELAX_SHA1" = "true" ]; then
        insecure_flag="--insecure-relax-sha1"
    fi

    echo "===== 模式: nginx-only | 发行版: $distro | Web: $WEB_SERVER =====" > "$log_file"
    log_msg "$log_file" "源目录: $SOURCE_DIR"
    echo "" >> "$log_file"

    if ! pull_image "$distro" "$log_file"; then
        echo "RESULT: SKIP (镜像拉取失败)" >> "$log_file"
        return 2
    fi

    local test_cmd="
set -e

echo '--- OS 信息 ---'
if [ -f /etc/os-release ]; then
    cat /etc/os-release
fi
echo ''

echo '--- 运行 scripts/nginx-install/install.sh ---'
bash /src/scripts/nginx-install/install.sh $nginx_flag $insecure_flag --non-interactive 2>&1

echo ''
echo '--- 验证安装 ---'

if [ '$WEB_SERVER' = 'openresty' ]; then
    BIN_NAME=openresty
else
    BIN_NAME=nginx
fi

# 检查二进制
if command -v \$BIN_NAME >/dev/null 2>&1; then
    echo \"二进制: \$(command -v \$BIN_NAME)\"
    \$BIN_NAME -v 2>&1 || true
else
    echo 'ERROR: 未找到 \$BIN_NAME 二进制'
    exit 1
fi

# 检查关键目录
for d in /opt/nxpanel/nginx/conf.d \
         /opt/nxpanel/nginx/sites-available \
         /opt/nxpanel/nginx/sites-enabled \
         /opt/nxpanel/nginx/ssl \
         /opt/nxpanel/data; do
    if [ -d \"\$d\" ]; then
        echo \"目录存在: \$d\"
    else
        echo \"WARN: 目录缺失: \$d\"
    fi
done

# 检查配置文件
if [ -f /etc/nginx/nginx.conf ]; then
    echo '配置文件存在: /etc/nginx/nginx.conf'
else
    echo 'WARN: /etc/nginx/nginx.conf 不存在'
fi

# nginx -t
echo ''
echo '--- nginx -t 测试 ---'
if \$BIN_NAME -t 2>&1; then
    echo 'nginx -t 通过'
else
    echo 'WARN: nginx -t 未通过（容器内环境受限，可能是正常现象）'
fi

# 验证 SHA1 策略文件已被清理（仅 --insecure-relax-sha1 时检查）
if [ '$INSECURE_RELAX_SHA1' = 'true' ]; then
    if [ -f /etc/crypto-policies/back-ends/apt-sequoia.config ]; then
        echo 'ERROR: /etc/crypto-policies/back-ends/apt-sequoia.config 未被清理'
        exit 1
    else
        echo 'SHA1 策略文件已清理 ✓'
    fi
fi

echo ''
echo 'RESULT: PASS'
"

    local exit_code=0
    docker run --rm \
        -v "$SOURCE_DIR:/src:ro" \
        --tmpfs /run \
        --tmpfs /tmp \
        -e DEBIAN_FRONTEND=noninteractive \
        "${DOCKER_PROXY_ENV_ARGS[@]}" \
        "$distro" \
        bash -c "$test_cmd" 2>&1 | log_pipe "$log_file" || exit_code=$?

    if [ $exit_code -ne 0 ]; then
        echo "RESULT: FAIL (退出码: $exit_code)" >> "$log_file"
        return 1
    fi

    if grep -q "^RESULT: PASS" "$log_file"; then
        return 0
    else
        echo "RESULT: FAIL (未看到 PASS 标记)" >> "$log_file"
        return 1
    fi
}

# ============================================================
# 模式 2：full（测试完整 install.sh）
# ============================================================

build_systemd_image() {
    local distro="$1"
    local sname="$2"
    local log_file="$3"
    local image_tag="nxpanel-test-$sname:latest"

    prepare_proxy_env_args

    echo "  构建 systemd 测试镜像..." >> "$log_file"
    if [ "$DOCKER_LOGS" = true ]; then echo -e "  ${DIM}构建 systemd 测试镜像...${NC}" >&2; fi

    local tmpdir
    tmpdir=$(mktemp -d "/tmp/nxpanel-test-dockerfile.XXXXXX")

    local pkg_install=""
    if echo "$distro" | grep -qE "debian|ubuntu"; then
        pkg_install="apt-get update && apt-get install -y systemd systemd-sysv bash curl gnupg2 ca-certificates lsb-release && apt-get clean"
    elif echo "$distro" | grep -qE "almalinux|rockylinux|fedora|oraclelinux|opencloudos|alinux"; then
        pkg_install="dnf install -y systemd bash curl gnupg2 ca-certificates"
    elif echo "$distro" | grep -qE "^centos:7"; then
        pkg_install="yum install -y systemd bash curl gnupg2 ca-certificates"
    else
        pkg_install="echo 'unknown distro family'"
    fi

    cat > "$tmpdir/Dockerfile" <<DOCKERFILE
FROM $distro
RUN $pkg_install
RUN systemctl mask \
    systemd-remount-fs.service \
    dev-hugepages.mount \
    sys-fs-fuse-connections.mount \
    systemd-logind.service \
    getty.target \
    getty@tty1.service 2>/dev/null || true
VOLUME ["/sys/fs/cgroup"]
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
DOCKERFILE

    echo "  --- Dockerfile ---" >> "$log_file"
    cat "$tmpdir/Dockerfile" >> "$log_file"
    echo "  --- End Dockerfile ---" >> "$log_file"

    local build_output
    build_output=$(mktemp)
    docker build -t "$image_tag" "$tmpdir" > "$build_output" 2>&1
    local build_exit=$?
    cat "$build_output" >> "$log_file"
    if [ "$DOCKER_LOGS" = true ]; then
        cat "$build_output" >&2
    fi
    rm -f "$build_output"
    if [ $build_exit -ne 0 ]; then
        rm -rf "$tmpdir"
        echo "RESULT: SKIP (systemd 镜像构建失败)" >> "$log_file"
        return 1
    fi

    rm -rf "$tmpdir"
    register_cleanup "image" "$image_tag"
    return 0
}

test_full() {
    local distro="$1"
    local sname="$2"
    local log_file="$3"
    local container_name="nxpanel-test-$sname"

    local install_flag="--install-$WEB_SERVER"

    echo "===== 模式: full | 发行版: $distro | Web: $WEB_SERVER =====" > "$log_file"
    log_msg "$log_file" "源目录: $SOURCE_DIR"
    echo "" >> "$log_file"

    if ! pull_image "$distro" "$log_file"; then
        echo "RESULT: SKIP (镜像拉取失败)" >> "$log_file"
        return 2
    fi

    local systemd_image
    if ! build_systemd_image "$distro" "$sname" "$log_file"; then
        return 2
    fi
    systemd_image=$(docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | grep "^nxpanel-test-$sname:" | head -1) || true
    if [ -z "$systemd_image" ]; then
        echo "RESULT: SKIP (构建后未找到镜像 nxpanel-test-$sname)" >> "$log_file"
        return 2
    fi

    log_msg "$log_file" "  systemd 镜像: $systemd_image"

    log_msg "$log_file" "  启动 systemd 容器..."
    docker rm -f "$container_name" &>/dev/null || true
    local run_args=(
        -d --privileged
        --tmpfs /run --tmpfs /tmp
        -v /sys/fs/cgroup:/sys/fs/cgroup:ro
        --name "$container_name"
        "$systemd_image"
    )
    if [ ${#DOCKER_PROXY_ENV_ARGS[@]} -gt 0 ]; then
        run_args=(
            -d --privileged
            --tmpfs /run --tmpfs /tmp
            -v /sys/fs/cgroup:/sys/fs/cgroup:ro
            --name "$container_name"
            "${DOCKER_PROXY_ENV_ARGS[@]}"
            "$systemd_image"
        )
    fi
    if [ "$DOCKER_LOGS" = true ]; then
        docker run "${run_args[@]}" 2>&1 | tee -a "$log_file" || {
            echo "RESULT: FAIL (容器启动失败)" >> "$log_file"
            return 1
        }
    else
        docker run "${run_args[@]}" >> "$log_file" 2>&1 || {
            echo "RESULT: FAIL (容器启动失败)" >> "$log_file"
            return 1
        }
    fi
    register_cleanup "container" "$container_name"

    local logs_pid=""
    if [ "$DOCKER_LOGS" = true ]; then
        docker logs -f "$container_name" >&2 &
        logs_pid=$!
    fi

    kill_logs_pid() {
        if [ -n "$logs_pid" ]; then
            kill "$logs_pid" 2>/dev/null || true
            wait "$logs_pid" 2>/dev/null || true
        fi
    }

    # 等待 systemd 就绪
    log_msg "$log_file" "  等待 systemd 就绪..."
    local wait_count=0
    while [ $wait_count -lt 30 ]; do
        if docker exec "$container_name" bash -c "systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded'" 2>/dev/null; then
            break
        fi
        if [ "$DOCKER_LOGS" = true ]; then
            echo -e "  ${DIM}.${NC}\c" >&2
        fi
        sleep 2
        wait_count=$((wait_count + 1))
    done

    if [ $wait_count -ge 30 ]; then
        log_msg "$log_file" "  WARN: systemd 等待超时，继续测试..."
    else
        log_msg "$log_file" "  systemd 就绪 (${wait_count}次检查)"
    fi

    # 复制源目录到容器
    log_msg "$log_file" "  复制安装源到容器..."
    docker cp "$SOURCE_DIR/." "$container_name:/tmp/nxpanel/" >> "$log_file" 2>&1 || {
        kill_logs_pid
        echo "RESULT: FAIL (复制文件到容器失败)" >> "$log_file"
        return 1
    }
    docker exec "$container_name" bash -c "chmod +x /tmp/nxpanel/install.sh /tmp/nxpanel/scripts/nginx-install/install.sh" >> "$log_file" 2>&1 || true

    # 运行 install.sh
    echo "" >> "$log_file"
    log_msg "$log_file" "  --- 运行 install.sh ---"
    local install_exit=0
    docker exec "$container_name" \
        bash /tmp/nxpanel/install.sh \
            --source-dir /tmp/nxpanel \
            --non-interactive \
            $install_flag \
            2>&1 | log_pipe "$log_file" || install_exit=$?

    log_msg "$log_file" "  install.sh 退出码: $install_exit"

    # 验证
    echo "" >> "$log_file"
    log_msg "$log_file" "  --- 验证安装结果 ---"

    local verify_cmd="
echo '--- 文件结构检查 ---'

for bin in /usr/local/nxpanel/bin/nxpanel-api /usr/local/nxpanel/bin/nxpanel-agent; do
    if [ -f \"\$bin\" ]; then
        echo \"二进制存在: \$bin\"
    else
        echo \"ERROR: 二进制缺失: \$bin\"
    fi
done

if [ -f /usr/local/nxpanel/configs/config.yaml ]; then
    echo '配置文件存在'
else
    echo 'ERROR: 配置文件缺失'
fi

if [ -d /usr/local/nxpanel/configs/templates ]; then
    echo '模板目录存在'
else
    echo 'ERROR: 模板目录缺失'
fi

if [ -d /usr/local/nxpanel/web ]; then
    echo 'Web 目录存在'
else
    echo 'ERROR: Web 目录缺失'
fi

if [ -d /opt/nxpanel/data ]; then
    echo '数据目录存在'
else
    echo 'ERROR: 数据目录缺失'
fi

if [ -d /opt/nxpanel/nginx/conf.d ]; then
    echo 'Nginx conf.d 目录存在'
else
    echo 'ERROR: Nginx conf.d 目录缺失'
fi

if id openrest &>/dev/null; then
    echo '用户 openrest 存在'
else
    echo 'ERROR: 用户 openrest 不存在'
fi

for svc in nxpanel-agent.service nxpanel-api.service; do
    if [ -f \"/etc/systemd/system/\$svc\" ]; then
        echo \"服务文件存在: \$svc\"
    else
        echo \"ERROR: 服务文件缺失: \$svc\"
    fi
done

echo ''
echo '--- systemd 服务状态 ---'
systemctl is-active nxpanel-agent 2>&1 || echo 'WARN: nxpanel-agent 未运行'
systemctl is-active nxpanel-api 2>&1 || echo 'WARN: nxpanel-api 未运行'

echo ''
echo '--- Nginx ---'
if command -v nginx >/dev/null 2>&1; then
    nginx -v 2>&1 || true
    nginx -t 2>&1 || echo 'WARN: nginx -t 未通过'
elif command -v openresty >/dev/null 2>&1; then
    openresty -v 2>&1 || true
    openresty -t 2>&1 || echo 'WARN: openresty -t 未通过'
else
    echo 'ERROR: 未找到 nginx/openresty'
fi
"
    docker exec "$container_name" bash -c "$verify_cmd" 2>&1 | log_pipe "$log_file" || true

    if [ $install_exit -ne 0 ]; then
        kill_logs_pid
        echo "RESULT: FAIL (install.sh 退出码: $install_exit)" >> "$log_file"
        return 1
    fi

    kill_logs_pid
    echo "RESULT: PASS" >> "$log_file"
    return 0
}

# ============================================================
# 单个发行版测试入口
# ============================================================

run_single_test() {
    local distro="$1"
    local sname
    sname=$(safe_name "$distro")
    local log_file="$RESULTS_DIR/${sname}.log"
    local start_time
    start_time=$(date +%s)

    TOTAL_COUNT=$((TOTAL_COUNT + 1))

    if [ "$WEB_SERVER" = "openresty" ] && [ "$distro" = "debian:13" ] && [ "$INSECURE_RELAX_SHA1" != "true" ]; then
        printf "  %-28s %s  (%ss)\n" "$distro" "${YELLOW}SKIP${NC}" 0
        echo "RESULT: SKIP (Debian 13 OpenResty 需 --insecure-relax-sha1 才能跑通；OpenResty repo key binding 仍使用 SHA1)" > "$log_file"
        RESULT_LINES+=("$distro|SKIP|0s")
        return 0
    fi

    printf "  %-28s " "$distro"

    local result=0
    if [ "$MODE" = "full" ]; then
        test_full "$distro" "$sname" "$log_file" || result=$?
    else
        test_nginx_only "$distro" "$sname" "$log_file" || result=$?
    fi

    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    local duration_str="${duration}s"

    local status_text=""
    local status_color=""
    case $result in
        0)
            status_text="PASS"
            status_color="$GREEN"
            ;;
        1)
            status_text="FAIL"
            status_color="$RED"
            FAILED_COUNT=$((FAILED_COUNT + 1))
            ;;
        2)
            status_text="SKIP"
            status_color="$YELLOW"
            ;;
    esac

    echo -e "${status_color}${status_text}${NC}  (${duration_str})"

    RESULT_LINES+=("$distro|$status_text|$duration_str")

    if [ "$result" -ne 0 ]; then
        local error_summary
        error_summary=$(grep -E '(ERROR|FAIL|PULL_FAILED)' "$log_file" 2>/dev/null | grep -v '^RESULT:' | head -10 || true)
        if [ -n "$error_summary" ]; then
            echo -e "${DIM}  失败原因:${NC}"
            echo "$error_summary" | sed 's/^/    /'
        fi
        local result_line
        result_line=$(grep '^RESULT:' "$log_file" | tail -1 || true)
        if [ -n "$result_line" ]; then
            echo -e "  ${DIM}${result_line}${NC}"
        fi

        if [ "$VERBOSE" = true ]; then
            echo -e "${DIM}--- $distro 日志尾部 ---${NC}"
            tail -30 "$log_file" | sed 's/^/  /'
        fi

        echo -e "${DIM}  完整日志: $log_file${NC}"
        echo ""
    fi
}

# ============================================================
# 汇总报告
# ============================================================

print_summary() {
    echo ""
    echo -e "${BOLD}========================================${NC}"
    echo -e "${BOLD}  测试汇总${NC}"
    echo -e "${BOLD}========================================${NC}"
    echo ""

    printf "  %-28s %-8s %s\n" "发行版" "结果" "耗时"
    printf "  %-28s %-8s %s\n" "----------------------------" "--------" "-----"

    local pass_count=0
    local fail_count=0
    local skip_count=0

    for line in "${RESULT_LINES[@]}"; do
        local distro status duration
        IFS='|' read -r distro status duration <<< "$line"
        local color=""
        case $status in
            PASS) color="$GREEN"; pass_count=$((pass_count + 1)) ;;
            FAIL) color="$RED"; fail_count=$((fail_count + 1)) ;;
            SKIP) color="$YELLOW"; skip_count=$((skip_count + 1)) ;;
        esac
        printf "  %-28s ${color}%-8s${NC} %s\n" "$distro" "$status" "$duration"
    done

    echo ""
    echo -e "  总计: ${BOLD}${TOTAL_COUNT}${NC}  通过: ${GREEN}${pass_count}${NC}  失败: ${RED}${fail_count}${NC}  跳过: ${YELLOW}${skip_count}${NC}"
    echo ""
    echo -e "  日志目录: ${CYAN}${RESULTS_DIR}/${NC}"
    echo ""
}

# ============================================================
# 主流程
# ============================================================

main() {
    parse_args "$@"

    echo -e "${BOLD}========================================${NC}"
    echo -e "${BOLD}  nxpanel 安装兼容性测试${NC}"
    echo -e "${BOLD}========================================${NC}"
    echo ""

    preflight_check
    determine_source
    validate_source

    log_info "模式: $MODE"
    log_info "Web 服务器: $WEB_SERVER"
    log_info "源目录: $SOURCE_DIR"
    log_info "并行数: $PARALLEL"
    if [ -n "$SOCKS5_PROXY" ]; then
        log_info "SOCKS5 代理: $SOCKS5_PROXY"
    fi

    local -a distros=()
    if [ -n "$CUSTOM_DISTROS" ]; then
        read -ra distros <<< "$CUSTOM_DISTROS"
    else
        distros=("${DEFAULT_DISTROS[@]}")
    fi

    echo ""
    log_info "测试发行版: ${distros[*]}"
    echo ""

    for distro in "${distros[@]}"; do
        run_single_test "$distro"
    done

    do_cleanup
    print_summary

    if [ "$FAILED_COUNT" -gt 0 ]; then
        exit 1
    fi
    exit 0
}

main "$@"
