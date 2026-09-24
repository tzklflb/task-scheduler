package output

import (
	"embed"
	"html/template"
	"strings"
)

//go:embed templates/gantt.html.tmpl assets/gantt.css assets/gantt.js
var files embed.FS

var ganttTemplate = template.Must(template.ParseFS(files, "templates/gantt.html.tmpl"))

// HTML は、CSS/JSを埋め込んだ単一ファイルで完結する対話的なスケジュール画面を返す。
func HTML(view View) (string, error) {
	var rendered strings.Builder
	if err := ganttTemplate.ExecuteTemplate(&rendered, "gantt.html.tmpl", struct {
		View View
		CSS  template.CSS
		JS   template.JS
	}{
		View: view,
		CSS:  template.CSS(mustRead("assets/gantt.css")),
		JS:   template.JS(mustRead("assets/gantt.js")),
	}); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func mustRead(name string) string {
	content, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(content)
}
