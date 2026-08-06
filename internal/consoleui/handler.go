package consoleui

import (
	"embed"
	"io/fs"
	"net/http"
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

		// Map the /console/* URL space onto the embedded dist directory.
		relative := strings.TrimPrefix(request.URL.Path, "/console")
		relative = strings.Trim(relative, "/")
		if relative == "" {
			relative = "index.html"
		}
		if _, err := fs.Stat(dist, relative); err != nil {
			// SPA route: serve the app shell so client-side routing takes over.
			relative = "index.html"
		}
		setCacheControl(writer, relative)

		clone := request.Clone(request.Context())
		if relative == "index.html" {
			// http.FileServer deliberately redirects any request ending in
			// "/index.html" to "./"; serve the directory root instead so its
			// index.html is rendered without a redirect loop.
			clone.URL.Path = "/"
		} else {
			clone.URL.Path = "/" + relative
		}
		files.ServeHTTP(writer, clone)
	})
}

// setCacheControl applies the static asset caching policy:
//   - The SPA entry (index.html, including SPA fallback routes) must never be
//     cached so browsers always fetch the latest shell after a deployment.
//   - Everything else is a Vite content-hashed asset (assets/index-<hash>.*),
//     which is immutable by name and can be cached for a long time.
func setCacheControl(writer http.ResponseWriter, relative string) {
	if relative == "index.html" {
		writer.Header().Set("Cache-Control", "no-store")
		return
	}
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}

func setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
