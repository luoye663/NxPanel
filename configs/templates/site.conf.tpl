#NXPANEL-SITE-START site_id={{.SiteID}} primary_domain={{.PrimaryDomain}}
server {
    #NXPANEL-LISTEN-START
{{.ListenBlock}}
    #NXPANEL-LISTEN-END

    #NXPANEL-SERVER-NAME-START
    server_name {{.ServerNames}};
    #NXPANEL-SERVER-NAME-END

{{if .SSLBlock}}
    #NXPANEL-SSL-START
{{.SSLBlock}}
    #NXPANEL-SSL-END

{{end}}{{if .ForceHTTPSBlock}}
    #NXPANEL-FORCE-HTTPS-START
{{.ForceHTTPSBlock}}
    #NXPANEL-FORCE-HTTPS-END

{{end}}
    #NXPANEL-ROOT-START
    root {{.RootPath}};
    index {{.IndexFiles}};
    #NXPANEL-ROOT-END

    #NXPANEL-REWRITE-START
    include {{.RewritePath}};
    #NXPANEL-REWRITE-END

    #NXPANEL-DOCUMENT-START
{{.DocumentBlock}}
    #NXPANEL-DOCUMENT-END

{{if .ACMEChallengeBlock}}
    #NXPANEL-ACME-CHALLENGE-START
{{.ACMEChallengeBlock}}
    #NXPANEL-ACME-CHALLENGE-END

{{end}}{{if .MainLocation}}
    #NXPANEL-MAIN-LOCATION-START
{{.MainLocation}}
    #NXPANEL-MAIN-LOCATION-END

{{end}}{{if .ExtraLocations}}
    #NXPANEL-EXTRA-LOCATIONS-START
{{.ExtraLocations}}
    #NXPANEL-EXTRA-LOCATIONS-END

{{end}}
    #NXPANEL-LOG-START
{{.LogBlock}}
    #NXPANEL-LOG-END
}
#NXPANEL-SITE-END
