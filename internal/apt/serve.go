package apt

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ServeConfig holds the configuration for serving a mirrored APT repository.
type ServeConfig struct {
	Directory   string
	ExternalURL string
	Listen      string
}

// Serve starts an HTTP server that serves the mirrored repository files.
func Serve(cfg ServeConfig) error {
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:8080"
	}
	if cfg.ExternalURL == "" {
		cfg.ExternalURL = "http://" + cfg.Listen + "/"
	}

	u, err := url.Parse(cfg.ExternalURL)
	if err != nil {
		return fmt.Errorf("invalid external URL: %w", err)
	}
	basePath := u.Path
	if basePath == "" {
		basePath = "/"
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}

	log.Printf("Serving %s at http://%s%s. External URL is set to %s", cfg.Directory, cfg.Listen, basePath, cfg.ExternalURL)

	mux := http.NewServeMux()

	handler := newMirrorHandler(cfg.Directory, basePath)
	mux.Handle(basePath, handler)

	// Redirect root to the base path if they differ.
	if basePath != "/" {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, basePath, http.StatusTemporaryRedirect)
		})
	}

	return http.ListenAndServe(cfg.Listen, mux)
}

type mirrorHandler struct {
	dir      string
	basePath string
}

func newMirrorHandler(dir, basePath string) *mirrorHandler {
	return &mirrorHandler{dir: dir, basePath: basePath}
}

func (h *mirrorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the base path prefix to get the path relative to the mirror root.
	relPath := strings.TrimPrefix(r.URL.Path, h.basePath)
	fullPath := filepath.Join(h.dir, filepath.FromSlash(relPath))

	// Prevent directory traversal.
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(h.dir)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		h.serveDirectory(w, r, fullPath, relPath)
		return
	}

	http.ServeFile(w, r, fullPath)
}

type dirEntry struct {
	Name  string
	URL   string
	Size  int64
	IsDir bool
}

type dirListingData struct {
	Path    string
	Entries []dirEntry
}

func (h *mirrorHandler) serveDirectory(w http.ResponseWriter, r *http.Request, fullPath, relPath string) {
	// Redirect to trailing slash for consistent relative links.
	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, "error reading directory", http.StatusInternalServerError)
		return
	}

	displayPath := "/" + relPath
	if !strings.HasSuffix(displayPath, "/") {
		displayPath += "/"
	}

	data := dirListingData{
		Path: displayPath,
	}

	// Add parent directory link if not at the root.
	if relPath != "" {
		data.Entries = append(data.Entries, dirEntry{
			Name:  "../",
			URL:   "../",
			IsDir: true,
		})
	}

	for _, entry := range entries {
		name := entry.Name()
		entryURL := name
		info, err := entry.Info()
		if err != nil {
			continue
		}
		isDir := entry.IsDir()
		if isDir {
			name += "/"
			entryURL += "/"
		}
		data.Entries = append(data.Entries, dirEntry{
			Name:  name,
			URL:   path.Join(".", entryURL),
			Size:  info.Size(),
			IsDir: isDir,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dirListingTemplate.Execute(w, data); err != nil {
		log.Printf("Error rendering directory listing: %v", err)
	}
}

// TODO this template looks atrocious, at last it can be edited though.
var dirListingTemplate = template.Must(template.New("dirListing").Funcs(template.FuncMap{
	"formatSize": func(size int64) string {
		return formatSize(size)
	},
}).Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Index of {{.Path}}</title>
<style>
  body {
    font-family: system-ui, -apple-system, sans-serif;
    margin: 0;
    padding: 20px 40px;
    color: #333;
    background: #fafafa;
  }
  h1 {
    font-size: 1.3em;
    font-weight: 600;
    border-bottom: 1px solid #ddd;
    padding-bottom: 10px;
  }
  table {
    border-collapse: collapse;
    width: 100%;
    max-width: 900px;
  }
  th, td {
    text-align: left;
    padding: 6px 12px;
  }
  th {
    font-weight: 600;
    font-size: 0.85em;
    text-transform: uppercase;
    color: #666;
    border-bottom: 2px solid #ddd;
  }
  tr:hover {
    background: #f0f0f0;
  }
  td.size {
    text-align: right;
    color: #888;
    font-variant-numeric: tabular-nums;
  }
  a {
    color: #0366d6;
    text-decoration: none;
  }
  a:hover {
    text-decoration: underline;
  }
</style>
</head>
<body>
<h1>Index of {{.Path}}</h1>
<table>
  <thead>
    <tr><th>Name</th><th style="text-align:right">Size</th></tr>
  </thead>
  <tbody>
  {{range .Entries -}}
    <tr>
      <td><a href="{{.URL}}">{{.Name}}</a></td>
      <td class="size">{{if .IsDir}}-{{else}}{{formatSize .Size}}{{end}}</td>
    </tr>
  {{end -}}
  </tbody>
</table>
</body>
</html>
`))
