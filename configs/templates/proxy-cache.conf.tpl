#NXPANEL-PROXY-CACHE-START
proxy_cache_path {{.CachePath}} levels=1:2 keys_zone=proxy_cache_zone:10m max_size=100m inactive=60m;
#NXPANEL-PROXY-CACHE-END
