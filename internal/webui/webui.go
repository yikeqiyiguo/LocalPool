package webui

import "embed"

// FS 内嵌编译后的前端静态资源（由 vite build 产出到 dist/）
//
//go:embed all:dist
var FS embed.FS
