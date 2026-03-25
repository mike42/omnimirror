package apt

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
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

	fileServer := http.FileServer(http.Dir(cfg.Directory))
	mux.Handle(basePath, http.StripPrefix(basePath, fileServer))

	// Redirect root to the base path if they differ.
	if basePath != "/" {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, basePath, http.StatusTemporaryRedirect)
		})
	}

	return http.ListenAndServe(cfg.Listen, mux)
}
