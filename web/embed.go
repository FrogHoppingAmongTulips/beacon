// Package web хранит вшитую в бинарник веб-панель.
package web

import "embed"

// Files — статические файлы панели, встроенные через go:embed.
//
//go:embed index.html
var Files embed.FS
