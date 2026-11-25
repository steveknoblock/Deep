package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
	"github.com/steveknoblock/Deep/internal/notebook"
)

// archive records an "archived" entry for each hash, as Deep would have
// written it, in the order given.
func (f *fakeHatcheck) archive(state string, hashes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range hashes {
		f.archiveLog = append(f.archiveLog, hatcheckclient.Relation{From: h, Rel: notebook.RelArchived, To: state})
	}
}

func streamHashes(p pageResult) []string {
	var hs []string
	for _, post := range p.Stream {
		hs = append(hs, post.Hash)
	}
	return hs
}

func TestArchivedPostsAreHiddenByDefault(t *testing.T) {
	f := &fakeHatcheck{n: 10}
	f.archive(notebook.ArchivedTrue, "p003", "p007")

	rec, p := getPage(t, f, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, post := range p.Stream {
		if post.Hash == "p003" || post.Hash == "p007" {
			t.Errorf("archived post %s appeared in the default stream", post.Hash)
		}
	}
	if len(p.Stream) != 8 || p.Total != 8 {
		t.Errorf("got %d posts, total %d; want 8 and 8", len(p.Stream), p.Total)
	}
	if p.ArchivedCount != 2 {
		t.Errorf("archived_count = %d, want 2", p.ArchivedCount)
	}
}

func TestIncludeArchivedShowsThemFlagged(t *testing.T) {
	f := &fakeHatcheck{n: 10}
	f.archive(notebook.ArchivedTrue, "p003", "p007")

	_, p := getPage(t, f, "archived=true")
	if len(p.Stream) != 10 || p.Total != 10 || p.ArchivedCount != 2 {
		t.Fatalf("got %d posts, total %d, archived_count %d; want 10, 10, 2", len(p.Stream), p.Total, p.ArchivedCount)
	}
	for _, post := range p.Stream {
		want := post.Hash == "p003" || post.Hash == "p007"
		if post.Archived != want {
			t.Errorf("post %s archived = %v, want %v", post.Hash, post.Archived, want)
		}
	}
}

// The latest entry for a post decides, whichever way it points.
func TestLatestArchivedEntryWins(t *testing.T) {
	f := &fakeHatcheck{n: 5}
	f.archive(notebook.ArchivedTrue, "p001", "p002", "p003")
	f.archive(notebook.ArchivedFalse, "p002") // unarchived again
	f.archive(notebook.ArchivedTrue, "p002")  // ...and archived once more
	f.archive(notebook.ArchivedFalse, "p003") // unarchived for good

	_, p := getPage(t, f, "")
	got := strings.Join(streamHashes(p), ",")
	if got != "p005,p004,p003" {
		t.Errorf("stream = %s, want p005,p004,p003 (p001 and p002 archived, p003 restored)", got)
	}
	if p.ArchivedCount != 2 {
		t.Errorf("archived_count = %d, want 2", p.ArchivedCount)
	}
}

// Filtering must happen before the list is cut into pages: every page is
// full, nothing is skipped or repeated, and Total counts only what can be
// paged through.
func TestPagingSkipsArchivedWithoutGaps(t *testing.T) {
	f := &fakeHatcheck{n: 120}
	var archived []string
	for i := 1; i <= 120; i += 3 { // every third post: 40 archived
		archived = append(archived, fmt.Sprintf("p%03d", i))
	}
	f.archive(notebook.ArchivedTrue, archived...)
	isArchived := map[string]bool{}
	for _, h := range archived {
		isArchived[h] = true
	}

	seen := map[string]bool{}
	before := ""
	var sizes []int
	for {
		q := "limit=30"
		if before != "" {
			q += "&before=" + url.QueryEscape(before)
		}
		rec, p := getPage(t, f, q)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if p.Total != 80 {
			t.Errorf("total = %d, want 80 (120 posts minus 40 archived)", p.Total)
		}
		sizes = append(sizes, len(p.Stream))
		for _, post := range p.Stream {
			if isArchived[post.Hash] {
				t.Fatalf("archived post %s appeared in a page", post.Hash)
			}
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
	if len(seen) != 80 {
		t.Errorf("walked %d posts, want 80", len(seen))
	}
	if len(sizes) != 3 || sizes[0] != 30 || sizes[1] != 30 || sizes[2] != 20 {
		t.Errorf("page sizes = %v, want [30 30 20]: pages must be full", sizes)
	}
}

// A cursor that names a post archived after the previous page was fetched
// must still work, not turn into a 400.
func TestCursorSurvivesBeingArchived(t *testing.T) {
	f := &fakeHatcheck{n: 10}
	_, first := getPage(t, f, "limit=4")
	if first.NextBefore != "p007" {
		t.Fatalf("setup: next_before = %q, want p007", first.NextBefore)
	}

	f.archive(notebook.ArchivedTrue, "p007") // archived between the two requests

	rec, next := getPage(t, f, "limit=4&before=p007")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(streamHashes(next), ","); got != "p006,p005,p004,p003" {
		t.Errorf("next page = %s, want p006,p005,p004,p003", got)
	}
}

func TestEverythingArchivedGivesEmptyStreamAndCount(t *testing.T) {
	f := &fakeHatcheck{n: 3}
	f.archive(notebook.ArchivedTrue, "p001", "p002", "p003")

	_, p := getPage(t, f, "")
	if len(p.Stream) != 0 || p.Total != 0 || p.HasMore || p.ArchivedCount != 3 {
		t.Errorf("got %d posts, total %d, has_more %v, archived_count %d; want 0, 0, false, 3",
			len(p.Stream), p.Total, p.HasMore, p.ArchivedCount)
	}
}

func TestBadArchivedParameterIs400(t *testing.T) {
	rec, _ := getPage(t, &fakeHatcheck{n: 3}, "archived=maybe")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func postArchive(t *testing.T, f *fakeHatcheck, action, hash string) *httptest.ResponseRecorder {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	svc := notebook.NewService(hatcheckclient.New(srv.URL))

	req := httptest.NewRequest(http.MethodPost, "/posts/"+hash+"/"+action, nil)
	req.SetPathValue("hash", hash)
	req.Header.Set("Authorization", "Bearer test-jwt")
	rec := httptest.NewRecorder()
	setArchivedHandler(svc, action == "archive").ServeHTTP(rec, req)
	return rec
}

func TestArchiveAndUnarchiveRecordARelation(t *testing.T) {
	f := &fakeHatcheck{n: 5}

	if rec := postArchive(t, f, "archive", "p002"); rec.Code != http.StatusOK {
		t.Fatalf("archive: status %d: %s", rec.Code, rec.Body.String())
	}
	_, p := getPage(t, f, "")
	if strings.Contains(strings.Join(streamHashes(p), ","), "p002") {
		t.Error("p002 should be hidden after archive")
	}

	if rec := postArchive(t, f, "unarchive", "p002"); rec.Code != http.StatusOK {
		t.Fatalf("unarchive: status %d: %s", rec.Code, rec.Body.String())
	}
	_, p = getPage(t, f, "")
	if !strings.Contains(strings.Join(streamHashes(p), ","), "p002") {
		t.Error("p002 should be back after unarchive")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posted) != 2 {
		t.Fatalf("posted %d relations, want 2: %+v", len(f.posted), f.posted)
	}
	want := []hatcheckclient.Relation{
		{From: "p002", Rel: "archived", To: "true"},
		{From: "p002", Rel: "archived", To: "false"},
	}
	for i, w := range want {
		if f.posted[i].From != w.From || f.posted[i].Rel != w.Rel || f.posted[i].To != w.To {
			t.Errorf("relation %d = %+v, want %+v", i, f.posted[i], w)
		}
	}
}

func TestArchiveRequiresAuth(t *testing.T) {
	svc := notebook.NewService(hatcheckclient.New("http://unused.invalid"))
	req := httptest.NewRequest(http.MethodPost, "/posts/p001/archive", nil)
	req.SetPathValue("hash", "p001")
	rec := httptest.NewRecorder()
	setArchivedHandler(svc, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}
