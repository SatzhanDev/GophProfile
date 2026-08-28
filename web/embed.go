// Package web хранит статические файлы веб-интерфейса (HTML-шаблоны, CSS, JS)
// и вшивает их прямо в скомпилированный бинарник через go:embed. Так сервер
// остаётся одним файлом — не нужно отдельно копировать папку web/ рядом
// с исполняемым файлом ни при локальном запуске, ни в Docker-образе.
package web

import "embed"

// TemplatesFS — HTML-шаблоны страниц (html/template).
//
//go:embed templates
var TemplatesFS embed.FS

// StaticFS — CSS и JS, отдаются как обычные статические файлы.
//
//go:embed static
var StaticFS embed.FS
