#NXPANEL-SITE-START site_id={{.SiteID}} primary_domain={{.PrimaryDomain}}
server {
    #NXPANEL-LISTEN-START
    listen {{.HTTPPort}};
    #NXPANEL-LISTEN-END

    #NXPANEL-SERVER-NAME-START
    server_name {{.ServerNames}};
    #NXPANEL-SERVER-NAME-END

    #NXPANEL-ROOT-START
    root {{.RootPath}};
    index {{.IndexFiles}};
    #NXPANEL-ROOT-END

    #NXPANEL-REWRITE-START
    include {{.RewritePath}};
    #NXPANEL-REWRITE-END

    #NXPANEL-DOCUMENT-START
    autoindex off;
    #NXPANEL-DOCUMENT-END

    #NXPANEL-LOG-START
    access_log {{.AccessLogPath}};
    error_log {{.ErrorLogPath}};
    #NXPANEL-LOG-END
}
#NXPANEL-SITE-END
