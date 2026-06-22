# ============================================================
# lib/log.sh — 日志输出、错误处理、临时文件清理
#
# 仅依赖 bash 内建命令，可被任何 adapter 安全 source。
# 不调用 set -e：由入口脚本决定 shell 选项。
# ============================================================

if [ -t 1 ]; then
    LOG_RED='\033[0;31m'
    LOG_GREEN='\033[0;32m'
    LOG_YELLOW='\033[1;33m'
    LOG_CYAN='\033[0;36m'
    LOG_NC='\033[0m'
else
    LOG_RED=''
    LOG_GREEN=''
    LOG_YELLOW=''
    LOG_CYAN=''
    LOG_NC=''
fi

# 临时文件注册表（cleanup_temp 时统一删除）
_CLEANUP_TEMP_FILES=()

# 清理钩子注册表（函数名数组，cleanup_temp 时按注册顺序调用）
_CLEANUP_HOOKS=()

log_info() {
    printf "${LOG_GREEN}[INFO]${LOG_NC} %s\n" "$1"
}

log_warn() {
    printf "${LOG_YELLOW}[WARN]${LOG_NC} %s\n" "$1" >&2
}

log_error() {
    printf "${LOG_RED}[ERROR]${LOG_NC} %s\n" "$1" >&2
}

log_step() {
    printf "${LOG_CYAN}[STEP]${LOG_NC} %s\n" "$1"
}

# die <msg> [exit-code]
die() {
    log_error "$1"
    exit "${2:-1}"
}

# register_temp_file <path>
#   登记一个临时文件路径，进程退出时清理（存在才删）
register_temp_file() {
    _CLEANUP_TEMP_FILES+=("$1")
}

# register_cleanup_hook <func_name>
#   登记一个清理函数，进程退出时调用（在临时文件删除之前）。
#   钩子函数必须幂等，且不应再调用 register_cleanup_hook（避免递归）。
register_cleanup_hook() {
    _CLEANUP_HOOKS+=("$1")
}

# cleanup_temp
#   1. 按注册顺序调用所有清理钩子（cleanup hooks）
#   2. 删除所有已登记的临时文件
#   幂等，可在 trap 中反复调用
cleanup_temp() {
    local h f
    # 先跑钩子（钩子可能依赖某些临时文件）
    for h in "${_CLEANUP_HOOKS[@]:-}"; do
        if [ -n "$h" ] && declare -F "$h" >/dev/null 2>&1; then
            "$h" || true
        fi
    done
    _CLEANUP_HOOKS=()

    # 再删临时文件
    for f in "${_CLEANUP_TEMP_FILES[@]:-}"; do
        [ -n "$f" ] && [ -e "$f" ] && rm -f "$f" 2>/dev/null || true
    done
    _CLEANUP_TEMP_FILES=()
}
