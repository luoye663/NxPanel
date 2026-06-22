# ============================================================
# lib/command.sh — 网络下载、HTTP 探测工具
#
# 提供：
#   download_to <url> <path>          下载到文件
#   download_to_pipe <url>            输出到 stdout（适合管道）
#   http_head_ok <url>                HEAD/Release 检查，200 返回 0
#
# 依赖：lib/log.sh
# ============================================================

# _curl_available / _wget_available
_curl_available() {
    command -v curl >/dev/null 2>&1
}

_wget_available() {
    command -v wget >/dev/null 2>&1
}

# download_to <url> <path>
download_to() {
    local url="$1"
    local path="$2"

    if _curl_available; then
        curl -fsSL "$url" -o "$path"
        return
    fi
    if _wget_available; then
        wget -qO "$path" "$url"
        return
    fi
    die "缺少 curl/wget，无法下载: $url"
}

# download_to_pipe <url>
#   输出到 stdout，调用方需用管道接收。
#   例：download_to_pipe "$key_url" | gpg --dearmor -o "$keyring"
download_to_pipe() {
    local url="$1"

    if _curl_available; then
        curl -fsSL "$url"
        return
    fi
    if _wget_available; then
        wget -qO - "$url"
        return
    fi
    die "缺少 curl/wget，无法下载: $url"
}

# http_head_ok <url>
#   检查 URL 是否可访问（HTTP 2xx）；失败返回非零。
#   默认超时：连接 5s，总时长 10s。
http_head_ok() {
    local url="$1"

    if _curl_available; then
        curl -fsSL --connect-timeout 5 --max-time 10 "$url" >/dev/null 2>&1
        return
    fi
    if _wget_available; then
        wget -q --spider --timeout=10 "$url" >/dev/null 2>&1
        return
    fi
    die "缺少 curl/wget，无法探测 URL: $url"
}

# fetch_url_list <url>
#   抓取目录列表 HTML 到 stdout，供 grep 解析。
#   失败时返回非零（不 die，调用方负责兜底）。
fetch_url_list() {
    local url="$1"

    if _curl_available; then
        curl -fsSL --connect-timeout 10 --max-time 15 "$url" 2>/dev/null
        return
    fi
    if _wget_available; then
        wget -qO - --timeout=15 "$url" 2>/dev/null
        return
    fi
    return 1
}
