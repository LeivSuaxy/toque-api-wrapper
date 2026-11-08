package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

//go:embed templates/*.html static/*
var files embed.FS

type Templates struct {
	templates *template.Template
}

func New() (*Templates, error) {
	funcs := template.FuncMap{
		"humanBytes": humanBytes,
		"formatDate": formatDate,
		"maskKey":    maskKey,
	}

	templates, err := template.New("").Funcs(funcs).ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Templates{templates: templates}, nil
}

func (t *Templates) Render(w http.ResponseWriter, status int, name string, data any) error {
	var buffer bytes.Buffer
	if err := t.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write(buffer.Bytes())
	return err
}

func (t *Templates) Static() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}

	server := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		server.ServeHTTP(w, r)
	})
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	size := float64(value)
	for _, unit := range units {
		size /= 1024
		if size < 1024 {
			return fmt.Sprintf("%.1f %s", size, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", size/1024)
}

func formatDate(value string) string {
	if value == "" {
		return "—"
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("02/01/2006 15:04")
		}
	}
	return value
}

func maskKey(key string) string {
	if len(key) <= 16 {
		return key
	}
	return key[:12] + "…" + key[len(key)-4:]
}
