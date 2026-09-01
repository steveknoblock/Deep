package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/steveknoblock/Deep/internal/notebook"
)

// writeJSON writes v as a JSON response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a plain-text error message, matching Hatcheck's own
// handler style.
func writeError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

// listNotebooksHandler handles GET /notebooks — every tag with a document.
func listNotebooksHandler(svc *notebook.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authFromRequest(req)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		tags, err := svc.ListNotebooks(req.Context(), auth)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

// getNotebookHandler handles GET /notebook/{tag} — the composed document +
// stream view.
func getNotebookHandler(svc *notebook.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authFromRequest(req)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		tag := req.PathValue("tag")
		if tag == "" {
			writeError(w, http.StatusBadRequest, "tag is required")
			return
		}

		view, err := svc.GetNotebook(req.Context(), auth, tag)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// saveDocumentHandler handles POST /notebook/{tag}/document — the request
// body is the document's new full content.
func saveDocumentHandler(svc *notebook.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authFromRequest(req)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		tag := req.PathValue("tag")
		if tag == "" {
			writeError(w, http.StatusBadRequest, "tag is required")
			return
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read request body")
			return
		}

		doc, err := svc.SaveDocument(req.Context(), auth, tag, string(body))
		if err != nil {
			// SaveDocument can return a partial success (the document itself
			// saved, but the supersedes relation failed) alongside an error —
			// still report 502, since the caller needs to know something
			// went wrong, but the document write itself may have landed.
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

// createPostHandler handles POST /notebook/{tag}/posts — the request body
// is the new post's content.
func createPostHandler(svc *notebook.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authFromRequest(req)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		tag := req.PathValue("tag")
		if tag == "" {
			writeError(w, http.StatusBadRequest, "tag is required")
			return
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read request body")
			return
		}
		if len(body) == 0 {
			writeError(w, http.StatusBadRequest, "post content is required")
			return
		}

		post, err := svc.CreatePost(req.Context(), auth, tag, string(body))
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, post)
	}
}
