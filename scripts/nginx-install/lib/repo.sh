# ============================================================
# lib/repo.sh — APT/YUM 仓库版本回退（codename、releasever）
#
# 检测流程（与旧 scripts/install-nginx.sh 行为 1:1 对齐）：
#   1. curl 当前 codename/releasever → 存在则直接用
#   2. 不存在则查映射表（精确兼容性声明）
#   3. 映射表也未命中则 curl 列仓库目录自动兜底
#   4. 全失败则 die
#
# 依赖：lib/log.sh、lib/command.sh、lib/os.sh（OS_ID/OS_VERSION_ID/OS_CODENAME）
# ============================================================

# ============================================================
# 版本兼容性映射表
# ============================================================

# Ubuntu 代号映射（当前代号 → 仓库中最近的可用代号）
declare -gA UBUNTU_CODENAME_MAP=(
    [oracular]=noble    # 24.10 → 24.04
    [plucky]=noble      # 25.04 → 24.04
    [questing]=noble    # 25.10 → 24.04
    [resolute]=noble    # 26.04 → 24.04
)

# Debian 代号映射
declare -gA DEBIAN_CODENAME_MAP=(
    [trixie]=bookworm   # 13 → 12
    [forky]=bookworm    # 14 → 12
)

# RHEL/CentOS 主版本号映射
declare -gA RHEL_VERSION_MAP=(
    [10]=9
)

# Alibaba Cloud Linux 主版本号映射
#   alinux 3 与 RHEL 8 binary 兼容，因此仓库 releasever 需要回退到 8。
declare -gA ALINUX_VERSION_MAP=(
    [3]=8
)

# resolve_codename <repo_base_url>
#   APT 系：返回仓库中实际可用的 codename。
#   输出到 stdout；探测过程通过 stderr 输出 warn。
resolve_codename() {
    local repo_base="$1"
    local codename
    codename="$OS_CODENAME"

    if [ -z "$codename" ]; then
        codename=$(lsb_release -cs 2>/dev/null || true)
    fi
    if [ -z "$codename" ]; then
        die "无法解析当前发行版 codename（VERSION_CODENAME 与 lsb_release 均为空）"
    fi

    # 1. curl 检测当前代号是否在仓库中存在
    local release_url="${repo_base}/dists/${codename}/Release"
    if http_head_ok "$release_url"; then
        echo "$codename"
        return
    fi

    # 2. 不存在：查映射表（精确兼容性声明）
    local mapped=""
    if [ "$OS_ID" = "ubuntu" ]; then
        mapped="${UBUNTU_CODENAME_MAP[$codename]:-}"
    else
        mapped="${DEBIAN_CODENAME_MAP[$codename]:-}"
    fi

    if [ -n "$mapped" ]; then
        log_warn "${RUNTIME_ID:-runtime} 官方仓库尚未支持 ${OS_ID} ${codename}，使用 ${mapped}" >&2
        echo "$mapped"
        return
    fi

    # 3. 映射表也没有：curl 列仓库目录，自动选最新版本兜底
    local listing latest
    listing=$(fetch_url_list "${repo_base}/dists/") || listing=""
    latest=$(echo "$listing" \
        | grep -oP '(?<=href=")[^/]+(?=/")' \
        | sort -r \
        | head -n 1 || true)

    if [ -n "$latest" ]; then
        log_warn "${RUNTIME_ID:-runtime} 官方仓库尚未支持 ${OS_ID} ${codename}，使用 ${latest}" >&2
        echo "$latest"
        return
    fi

    # 4. 全失败
    die "${RUNTIME_ID:-runtime} 官方仓库尚未支持 ${OS_ID} ${OS_VERSION_ID} (${codename})，且无法获取仓库可用版本列表。"
}

# resolve_releasever <repo_base_url> <current_ver>
#   YUM/DNF 系：返回仓库中实际可用的主版本号（整数）。
resolve_releasever() {
    local repo_base="$1"
    local current_ver="$2"

    # 取主版本号
    local major_ver="${current_ver%%.*}"

    # 1. curl 检测当前版本是否在仓库中存在
    local basearch
    basearch=$(detect_arch)
    local repomd_url="${repo_base}/${major_ver}/${basearch}/repodata/repomd.xml"
    if http_head_ok "$repomd_url"; then
        echo "$major_ver"
        return
    fi

    # 2. 不存在：查映射表（精确兼容性声明）
    local mapped=""
    if [ "$OS_ID" = "alinux" ]; then
        mapped="${ALINUX_VERSION_MAP[$major_ver]:-}"
    else
        mapped="${RHEL_VERSION_MAP[$major_ver]:-}"
    fi

    if [ -n "$mapped" ]; then
        log_warn "${RUNTIME_ID:-runtime} 官方仓库尚未支持 el${major_ver}，使用 el${mapped}" >&2
        echo "$mapped"
        return
    fi

    # 3. 映射表也没有：curl 列仓库目录，自动选最大版本号兜底
    local listing latest
    listing=$(fetch_url_list "${repo_base}/") || listing=""
    latest=$(echo "$listing" \
        | grep -oP '(?<=href=")[0-9]+(?=/")' \
        | sort -rn \
        | head -n 1 || true)

    if [ -n "$latest" ]; then
        log_warn "${RUNTIME_ID:-runtime} 官方仓库尚未支持 el${major_ver}，使用 el${latest}" >&2
        echo "$latest"
        return
    fi

    # 4. 全失败
    die "${RUNTIME_ID:-runtime} 官方仓库尚未支持 ${OS_ID} ${major_ver}，且无法获取仓库可用版本列表。"
}
