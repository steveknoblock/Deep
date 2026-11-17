package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
	"github.com/steveknoblock/Deep/internal/notebook"
)

// fakeHatcheck serves just enough of Hatcheck for GET /notebook/{tag}: tag
// "t" holds posts p001..pNNN (stored oldest first, as Hatcheck returns
// them). If docHash is set, notebook/t points at it and the version chain
// says it superseded oldVersion, which is also tagged t.
type fakeHatcheck struct {
	n          int
	docHash    string
	oldVersion string
	fetched    atomic.Int64 // post fetches, so tests can see paging skip work
}

func (f *fakeHatcheck) hashes() []string {
	var hs []string
	for i := 1; i <= f.n; i++ {
		hs = append(hs, fmt.Sprintf("p%03d", i))
	}
	if f.oldVersion != "" {
		hs = append(hs, f.oldVersion)
	}
	return hs
}

func (f *fakeHatcheck) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch r.URL.Path {
	case "/names":
		names := []map[string]string{}
		if f.docHash != "" {
			names = append(names, map[string]string{"label": "t", "hash": f.docHash})
		}
		json.NewEncoder(w).Encode(names)
	case "/query":
		json.NewEncoder(w).Encode(f.hashes())
	case "/relations":
		out := []hatcheckclient.Relation{}
		if q.Get("hash") == f.docHash && f.oldVersion != "" {
			out = append(out, hatcheckclient.Relation{From: f.docHash, Rel: "supersedes", To: f.oldVersion})
		}
		if q.Get("hash") == f.docHash {
			out = append(out, hatcheckclient.Relation{From: f.docHash, Rel: "merged-from", To: "p002"})
		}
		json.NewEncoder(w).Encode(map[string]any{"outgoing": out, "incoming": []any{}})
	case "/fetch":
		h := q.Get("hash")
		if h != f.docHash {
			f.fetched.Add(1)
		}
		if h == "p004" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "content of %s", h)
	case "/object-meta":
		json.NewEncoder(w).Encode(map[string]string{"kind": "text", "created": "2026-01-01T00:00:00Z"})
	default:
		http.NotFound(w, r)
	}
}

type pageResult struct {
	Tag    string `json:"tag"`
	Stream []struct {
		Hash    string `json:"hash"`
		Content string `json:"content"`
		Failed  bool   `json:"failed"`
		Merged  bool   `json:"merged"`
	} `json:"stream"`
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
	NextBefore string `json:"next_before"`
}

func getPage(t *testing.T, f *fakeHatcheck, query string) (*httptest.ResponseRecorder, pageResult) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	svc := notebook.NewService(hatcheckclient.New(srv.URL))

	target := "/notebook/t"
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("tag", "t")
	req.Header.Set("Authorization", "Bearer test-jwt")
	rec := httptest.NewRecorder()
	getNotebookHandler(svc).ServeHTTP(rec, req)

	var res pageResult
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
		}
	}
	return rec, res
}

func TestStreamIsPagedNewestFirst(t *testing.T) {
	f := &fakeHatcheck{n: 120}
	rec, p := getPage(t, f, "limit=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(p.Stream) != 50 || p.Total != 120 || !p.HasMore {
		t.Fatalf("got %d posts, total %d, has_more %v", len(p.Stream), p.Total, p.HasMore)
	}
	if p.Stream[0].Hash != "p120" || p.Stream[49].Hash != "p071" {
		t.Errorf("page should run p120..p071, got %s..%s", p.Stream[0].Hash, p.Stream[49].Hash)
	}
	if p.NextBefore != "p071" {
		t.Errorf("next_before = %q, want p071", p.NextBefore)
	}
	// The point of paging: the 70 older posts were never fetched.
	if got := f.fetched.Load(); got != 50 {
		t.Errorf("fetched %d posts from Hatcheck, want 50", got)
	}
}

func TestPagesWalkThroughWholeStream(t *testing.T) {
	f := &fakeHatcheck{n: 120}
	seen := map[string]bool{}
	before := ""
	pages := 0
	for {
		q := "limit=50"
		if before != "" {
			q += "&before=" + url.QueryEscape(before)
		}
		rec, p := getPage(t, f, q)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		pages++
		for _, post := range p.Stream {
			if seen[post.Hash] {
				t.Fatalf("post %s returned twice", post.Hash)
			}
			seen[post.Hash] = true
		}
		if !p.HasMore {
			break
		}
		before = p.NextBefore
	}
	if pages != 3 || len(seen) != 120 {
		t.Errorf("pages = %d, posts = %d; want 3 pages, 120 posts", pages, len(seen))
	}
}

func TestDefaultPageSizeAndCap(t *testing.T) {
	f := &fakeHatcheck{n: 300}
	_, p := getPage(t, f, "")
	if len(p.Stream) != notebook.DefaultPageSize || !p.HasMore {
		t.Errorf("default page: %d posts, has_more %v", len(p.Stream), p.HasMore)
	}
	_, p = getPage(t, &fakeHatcheck{n: 300}, "limit=100000")
	if len(p.Stream) != notebook.MaxPageSize {
		t.Errorf("limit should be capped at %d, got %d posts", notebook.MaxPageSize, len(p.Stream))
	}
}

func TestShortStreamHasNoMore(t *testing.T) {
	_, p := getPage(t, &fakeHatcheck{n: 3}, "")
	if len(p.Stream) != 3 || p.HasMore || p.NextBefore != "" || p.Total != 3 {
		t.Errorf("unexpected page: %+v", p)
	}
}

func TestBadParametersAre400(t *testing.T) {
	for _, q := range []string{"limit=abc", "limit=0", "limit=-5", "before=nosuchpost"} {
		rec, _ := getPage(t, &fakeHatcheck{n: 5}, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", q, rec.Code)
		}
	}
}

func TestPastVersionsExcludedAndMergedFlagged(t *testing.T) {
	f := &fakeHatcheck{n: 6, docHash: "docv2", oldVersion: "docv1"}
	rec, p := getPage(t, f, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, post := range p.Stream {
		if post.Hash == "docv1" {
			t.Error("a past document version appeared in the stream")
		}
	}
	if p.Total != 6 {
		t.Errorf("total = %d, want 6 (the old version isn't a post)", p.Total)
	}
	for _, post := range p.Stream {
		if post.Hash == "p002" && !post.Merged {
			t.Error("p002 should be flagged merged")
		}
	}
}

func TestFailedPostStaysInPlaceAndPageOrderIsStable(t *testing.T) {
	f := &fakeHatcheck{n: 8}
	_, p := getPage(t, f, "")
	want := []string{"p008", "p007", "p006", "p005", "p004", "p003", "p002", "p001"}
	for i, w := range want {
		if p.Stream[i].Hash != w {
			t.Fatalf("position %d = %s, want %s", i, p.Stream[i].Hash, w)
		}
	}
	if !p.Stream[4].Failed {
		t.Error("p004 (whose fetch errors) should be marked failed, in place")
	}
}
