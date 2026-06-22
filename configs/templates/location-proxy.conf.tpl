    location {{.LocationPath}} {
        proxy_pass {{.UpstreamURL}};
        proxy_set_header Host {{.HostHeader}};
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
{{- if .WebSocketEnabled}}
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
{{- end}}
        proxy_connect_timeout {{.ConnectTimeout}}s;
        proxy_send_timeout {{.SendTimeout}}s;
        proxy_read_timeout {{.ReadTimeout}}s;
{{- if .CacheEnabled}}
{{- if eq .CacheType "nginx"}}
        proxy_cache proxy_cache_zone;
        proxy_cache_valid 200 {{.CacheTime}}m;
        proxy_cache_use_stale error timeout updating;
        add_header X-Cache-Status $upstream_cache_status;
{{- else if eq .CacheType "file"}}
        root {{.CachePath}};
        proxy_store on;
        proxy_store_access user:rw group:rw all:r;
        proxy_temp_path {{.CachePath}}/temp;
{{- end}}
{{- end}}
    }
