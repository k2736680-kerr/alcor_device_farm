package consoleui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(writer)
		if request.URL.Path == "/console" {
			http.Redirect(writer, request, "/console/", http.StatusPermanentRedirect)
			return
		}
		relative := strings.TrimPrefix(path.Clean(request.URL.Path), "/console/")
		if relative == "." || relative == "" {
			relative = "index.html"
		}
		if _, err := fs.Stat(dist, relative); err != nil {
			relative = "index.html"
		}
		clone := request.Clone(request.Context())
		clone.URL.Path = "/" + relative
		files.ServeHTTP(writer, clone)
	})
}

func setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
