package main

import (
	"net/http"

	"github.com/steveknoblock/Deep/internal/notebook"
)

// registerRoutes wires every HTTP handler to its path. It is the single
// place routes are defined, matching Hatcheck's own registerRoutes.
func registerRoutes(mux *http.ServeMux, svc *notebook.Service, cfg Config) {
	mux.Handle("GET /notebooks", listNotebooksHandler(svc))
	mux.Handle("GET /notebook/{tag}", getNotebookHandler(svc))
	mux.Handle("POST /notebook/{tag}/document", saveDocumentHandler(svc))
	mux.Handle("POST /notebook/{tag}/posts", createPostHandler(svc))

	// Deep serves its own UI, same static-file pattern as Hatcheck's /ui/.
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir(cfg.UIPath))))
}
