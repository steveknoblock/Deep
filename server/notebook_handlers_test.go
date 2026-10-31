package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
	"github.com/steveknoblock/Deep/internal/notebook"
)

// listNotebooks calls GET /notebooks on Deep against a fake Hatcheck whose
// /names endpoint answers with the given status and body.
func listNotebooks(t *testing.T, hatcheckStatus int, hatcheckBody string) *httptest.ResponseRecorder {
	t.Helper()
	hatcheck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(hatcheckStatus)
		w.Write([]byte(hatcheckBody))
	}))
	t.Cleanup(hatcheck.Close)

	svc := notebook.NewService(hatcheckclient.New(hatcheck.URL))
	req := httptest.NewRequest(http.MethodGet, "/notebooks", nil)
	req.Header.Set("Authorization", "Bearer test-jwt")
	rec := httptest.NewRecorder()
	listNotebooksHandler(svc).ServeHTTP(rec, req)
	return rec
}

func TestUpstreamAuthRejectionsPassThrough(t *testing.T) {
	cases := []struct {
		name           string
		hatcheckStatus int
		want           int
	}{
		{"expired session is 401", http.StatusUnauthorized, http.StatusUnauthorized},
		{"bad capability is 403", http.StatusForbidden, http.StatusForbidden},
		{"hatcheck server error is 502", http.StatusInternalServerError, http.StatusBadGateway},
		{"hatcheck not found is 502", http.StatusNotFound, http.StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := listNotebooks(t, c.hatcheckStatus, "denied")
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
			if !strings.Contains(rec.Body.String(), "denied") {
				t.Errorf("body %q should carry Hatcheck's message", rec.Body.String())
			}
		})
	}
}

func TestHatcheckUnreachableIs502(t *testing.T) {
	hatcheck := httptest.NewServer(http.NotFoundHandler())
	url := hatcheck.URL
	hatcheck.Close() // nothing is listening any more

	svc := notebook.NewService(hatcheckclient.New(url))
	req := httptest.NewRequest(http.MethodGet, "/notebooks", nil)
	req.Header.Set("Authorization", "Bearer test-jwt")
	rec := httptest.NewRecorder()
	listNotebooksHandler(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestListNotebooksAlwaysIncludesUncategorizedFirst(t *testing.T) {
	rec := listNotebooks(t, http.StatusOK, `[{"label":"film","hash":"h1"}]`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `["uncategorized","film"]`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
