package docs

import (
	"embed"
	"io/fs"
)

//go:embed sql/*.sql
var embedded embed.FS

// SQL 是本模块的迁移文件.
var SQL = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(embedded, "sql")
	if err != nil {
		panic(err)
	}
	return sub
}
