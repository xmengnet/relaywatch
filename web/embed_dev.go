//go:build dev

// Package web 的 dev 构建：跳过 go:embed（前端由 Vite dev server 提供，:5173）。
package web

import "io/fs"

// Dist 在 dev 构建下返回 nil——main 据此跳过静态资源路由。
func Dist() fs.FS {
	return nil
}
