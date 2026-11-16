package web

import "embed"

//go:embed templates static vendor.sha256
var embedded embed.FS
