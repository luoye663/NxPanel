#!/usr/bin/with-contenv /bin/sh
set -e

# 渲染主 nginx.conf
#
# 模板源：/usr/local/nxpanel/config/nginx.conf.tpl
#         （nginx 变体=nginx.conf.production，openresty 变体=nginx.conf.openresty）
#
# 替换变量：
#   {{WEB_USER}}   ← NXPANEL_NGINX_WEB_USER（nginx 变体=nginx，openresty 变体=nobody）
#   {{PID_PATH}}   ← NXPANEL_NGINX_PID_PATH（默认 /run/nginx.pid）
#   {{PANEL_DIR}}  ← NXPANEL_NGINX_PANEL_DIR（默认 /opt/nxpanel/nginx）
#   {{ERROR_LOG_PATH}} ← NXPANEL_NGINX_ERROR_LOG_PATH（默认 /var/log/nginx/error.log）
#
# 用法：
#   render-nginx-conf.sh                       # 渲染到 NXPANEL_NGINX_CONF_PATH
#   render-nginx-conf.sh /custom/path.conf     # 渲染到指定路径

TPL="${NXPANEL_NGINX_CONF_TPL:-/usr/local/nxpanel/config/nginx.conf.tpl}"
TARGET="${1:-${NXPANEL_NGINX_CONF_PATH:-/opt/nxpanel/nginx/nginx.conf}}"
WEB_USER="${NXPANEL_NGINX_WEB_USER:-nginx}"
PID_PATH="${NXPANEL_NGINX_PID_PATH:-/run/nginx.pid}"
PANEL_DIR="${NXPANEL_NGINX_PANEL_DIR:-/opt/nxpanel/nginx}"
ERROR_LOG_PATH="${NXPANEL_NGINX_ERROR_LOG_PATH:-/var/log/nginx/error.log}"

if [ ! -f "${TPL}" ]; then
    echo "[render-nginx-conf] template not found: ${TPL}" >&2
    exit 1
fi

# 确保 target 父目录存在
mkdir -p "$(dirname "${TARGET}")"

# 渲染到临时文件后原子替换
TMP="${TARGET}.nxpanel-tmp"
sed \
    -e "s|{{WEB_USER}}|${WEB_USER}|g" \
    -e "s|{{PID_PATH}}|${PID_PATH}|g" \
    -e "s|{{PANEL_DIR}}|${PANEL_DIR}|g" \
    -e "s|{{ERROR_LOG_PATH}}|${ERROR_LOG_PATH}|g" \
    "${TPL}" > "${TMP}"
mv -f "${TMP}" "${TARGET}"

echo "[render-nginx-conf] rendered ${TARGET} (web_user=${WEB_USER}, pid=${PID_PATH}, panel_dir=${PANEL_DIR}, error_log=${ERROR_LOG_PATH})"
