package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
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

// writeUpstreamError reports a failed Hatcheck call. If Hatcheck rejected the
// caller's session or capability (401/403), that status is passed through so
// the browser can react — a 401 signs the user out and asks them to sign in
// again. Everything else (Hatcheck unreachable, a server error, a malformed
// response) is a 502, since the problem is upstream rather than the caller's.
func writeUpstreamError(w http.ResponseWriter, err error) {
	var se *hatcheckclient.StatusError
	if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden) {
		writeError(w, se.Code, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
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
			writeUpstreamError(w, err)
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
			writeUpstreamError(w, err)
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

		mergedHashes := parseMergedPostsHeader(req.Header.Get("X-Merged-Posts"))

		doc, err := svc.SaveDocument(req.Context(), auth, tag, string(body), mergedHashes)
		if err != nil {
			// SaveDocument can return a partial success (the document itself
			// saved, but the supersedes relation failed) alongside an error —
			// still report 502, since the caller needs to know something
			// went wrong, but the document write itself may have landed.
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

// parseMergedPostsHeader parses a comma-separated list of post hashes,
// skipping empty entries.
func parseMergedPostsHeader(header string) []string {
	if header == "" {
		return nil
	}
	var hashes []string
	for _, h := range strings.Split(header, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hashes = append(hashes, h)
		}
	}
	return hashes
}

// createPlainPostHandler handles POST /posts — a generic post with no
// forced tag, for the quick-compose editor shown when no notebook is open.
func createPlainPostHandler(svc *notebook.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authFromRequest(req)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
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

		post, err := svc.CreatePlainPost(req.Context(), auth, string(body))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, post)
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
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, post)
	}
}
