#!/bin/sh -e

# NxPanel 独享模式容器入口
#
# 职责：
#   1. 兜底创建关键目录（s6 nxpanel-init oneshot 也会做，这里防止用户绕过 s6-rc 直接运行）
#   2. exec s6-overlay（/init），它成为 PID1：
#        - 捕获 SIGTERM/SIGINT 分发给所有服务
#        - 运行 cont-init 阶段
#        - 激活 s6-rc 默认 bundle（user）
#        - 监督所有 longrun，崩溃自动重启
#
# 单进程重启（无需重启容器）：
#   docker exec <container> s6-svc -r /run/service/nginx-service   # 重启 nginx
#   docker exec <container> s6-svc -r /run/service/nxpanel-agent   # 重启 agent
#   docker exec <container> s6-svc -r /run/service/nxpanel-api     # 重启 api

# 1. 兜底目录（幂等）
mkdir -p \
  /opt/nxpanel/data \
  /opt/nxpanel/nginx/conf.d \
  /opt/nxpanel/nginx/sites-enabled \
  /opt/nxpanel/nginx/ssl \
  /opt/nxpanel/nginx/site-backups \
  /www/wwwroot \
  /www/wwwlogs \
  /var/log/nginx \
  /run/nxpanel 2>/dev/null || true

# 2. 交由 s6-overlay 接管 PID1
exec /init "$@"
