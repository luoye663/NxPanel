# ============================================================
# lib/matrix.sh — 支持矩阵通用判断工具
#
# 提供：
#   matrix_contains <matrix_str> <target>
#
# 用法：
#   RUNTIME_SUPPORTED_MATRIX="
#   debian:12 debian:13
#   ubuntu:22 ubuntu:24
#   "
#   if matrix_contains "$RUNTIME_SUPPORTED_MATRIX" "${OS_ID}:${OS_VERSION_ID_MAJOR}"; then
#       ...
#   fi
#
# 依赖：无（纯 bash 内建）
# ============================================================

# matrix_contains <matrix_str> <target>
#   matrix_str 是空白分隔的 token 列表（可含换行/制表/多空格）
#   target 是要查找的精确 token
#   命中返回 0，未命中返回 1
matrix_contains() {
    local matrix="$1"
    local target="$2"
    local item

    for item in $matrix; do
        [ "$item" = "$target" ] && return 0
    done
    return 1
}
