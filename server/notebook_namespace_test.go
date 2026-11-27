package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
	"github.com/steveknoblock/Deep/internal/notebook"
)

// nsFake is a Hatcheck fake whose /names answers per namespace, so tests can
// tell the new deep/notebook namespace from the legacy notebook one, and which
// records the names Deep writes.
type nsFake struct {
	mu      sync.Mutex
	names   map[string][]hatcheckclient.NameEntry // namespace -> names
	setName []string                              // "namespace/label->hash"
}

func (f *nsFake) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch r.URL.Path {
	case "/names":
		f.mu.Lock()
		out := f.names[q.Get("namespace")]
		f.mu.Unlock()
		if out == nil {
			out = []hatcheckclient.NameEntry{}
		}
		json.NewEncoder(w).Encode(out)
	case "/stash":
		json.NewEncoder(w).Encode(map[string]any{"hash": "newdoc", "capability": map[string]string{"id": "c"}})
	case "/name":
		f.mu.Lock()
		f.setName = append(f.setName, q.Get("namespace")+"/"+q.Get("label")+"->"+q.Get("hash"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case "/relation":
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"hash":"rel"}`))
	default:
		http.NotFound(w, r)
	}
}

func newNSService(t *testing.T, f *nsFake) *notebook.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return notebook.NewService(hatcheckclient.New(srv.URL))
}

func nsAuth() hatcheckclient.AuthContext {
	return hatcheckclient.AuthContext{SessionJWT: "test-jwt"}
}

func TestNotebookNamespaceIsDeepPrefixed(t *testing.T) {
	if notebook.Namespace != "deep/notebook" {
		t.Errorf("Namespace = %q, want deep/notebook", notebook.Namespace)
	}
	if got := url.QueryEscape(notebook.Namespace); !strings.Contains(got, "deep") {
		t.Errorf("unexpected escaped namespace %q", got)
	}
}

func TestListNotebooksMergesNewAndLegacyNamespaces(t *testing.T) {
	f := &nsFake{names: map[string][]hatcheckclient.NameEntry{
		"deep/notebook": {{Label: "cooking", Hash: "h-new"}, {Label: "Zebra", Hash: "h-z"}},
		"notebook":      {{Label: "audio", Hash: "h-a"}, {Label: "cooking", Hash: "h-old"}},
	}}
	svc := newNSService(t, f)

	tags, err := svc.ListNotebooks(t.Context(), nsAuth())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{notebook.UncategorizedTag, "audio", "cooking", "Zebra"}
	if strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Errorf("tags = %v, want %v (union, no duplicate cooking, sorted case-insensitively)", tags, want)
	}
}

func TestSaveDocumentWritesNewNamespaceEvenFromLegacy(t *testing.T) {
	f := &nsFake{names: map[string][]hatcheckclient.NameEntry{
		"notebook": {{Label: "cooking", Hash: "h-old"}},
	}}
	svc := newNSService(t, f)

	if _, err := svc.SaveDocument(t.Context(), nsAuth(), "cooking", "# new text", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.setName) != 1 || f.setName[0] != "deep/notebook/cooking->newdoc" {
		t.Errorf("names written = %v, want [deep/notebook/cooking->newdoc]", f.setName)
	}
}
