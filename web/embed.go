// Package web 通过 go:embed 嵌入前端构建产物。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回去掉 dist/ 前缀的文件系统，供 HTTP 静态服务使用。
// 构建前端前该包无法编译（embed 要求 dist 存在），Makefile 保证顺序。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
