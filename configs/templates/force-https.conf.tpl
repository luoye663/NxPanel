    set $nxpanel_https_redirect $scheme;
    if ($uri ~ ^/\.well-known/acme-challenge/) {
        set $nxpanel_https_redirect off;
    }
    if ($nxpanel_https_redirect = http) {
        return 301 https://$host$request_uri;
    }
