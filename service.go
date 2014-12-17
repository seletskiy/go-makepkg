package main

import "text/template"

var serviceTemplate = template.Must(
	template.New("service").Parse(`[Unit]
Description={{.Description}}

[Service]
ExecStart=/usr/bin/{{.ExecName}}
Restart=always

[Install]
WantedBy=multi-user.target
`))
