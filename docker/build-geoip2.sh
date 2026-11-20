#!/bin/sh
set -eu

binary=$1
version_output=$("$binary" -V 2>&1)
release=$(printf '%s\n' "$version_output" | sed -n 's/^nginx version: //p')
options=$(printf '%s\n' "$version_output" | sed -n 's/^configure arguments: //p')

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
wget -q -T 40 -t 3 -O geoip2.tar.gz https://github.com/leev/ngx_http_geoip2_module/archive/refs/tags/3.4.tar.gz
tar -xzf geoip2.tar.gz
case "$release" in
    nginx/*)
        version=${release#nginx/}
        wget -q -T 40 -t 3 -O nginx.tar.gz "https://nginx.org/download/nginx-${version}.tar.gz"
        tar -xzf nginx.tar.gz
        cd "nginx-${version}"
        ;;
    openresty/*)
        version=${release#openresty/}
        wget -q -T 40 -t 3 -O openresty.tar.gz "https://openresty.org/download/openresty-${version}.tar.gz"
        tar -xzf openresty.tar.gz
        cd "openresty-${version}"/bundle/nginx-*
        ;;
    *) printf '%s\n' "Unsupported nginx version: $release" >&2; exit 1 ;;
esac

set --
for option in $options; do
    case "$option" in
        --with-compat|--with-debug|--with-threads|--with-file-aio|--with-http_ssl_module|--with-http_v2_module|--with-http_v3_module|--with-http_realip_module|--with-http_dav_module|--with-http_gzip_static_module|--with-http_gunzip_module|--without-http_gzip_module|--without-http_proxy_module|--without-http_fastcgi_module|--without-http_uwsgi_module|--without-http_scgi_module|--without-http_memcached_module|--without-http-cache|--without-http_upstream_zone_module|--without-pcre)
            set -- "$@" "$option"
            ;;
    esac
done
./configure "$@" --add-dynamic-module="$work/ngx_http_geoip2_module-3.4"
make -j "$(getconf _NPROCESSORS_ONLN)" modules
install -Dm644 objs/ngx_http_geoip2_module.so /out/ngx_http_geoip2_module.so
printf '%s\n' 'load_module /out/ngx_http_geoip2_module.so;' "error_log stderr;" "pid $work/nginx.pid;" 'events {}' 'http {}' > "$work/test.conf"
"$binary" -t -c "$work/test.conf"
