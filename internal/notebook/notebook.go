// Package notebook implements the Notebook application on top of Hatcheck's
// generic primitives (stashes, tags, names, relations). Hatcheck itself has
// no notion of a "document" or a "post" — those concepts, and the rules for
// how they compose (a document is a post referenced by notebook/<tag>; a
// version is superseded by writing a supersedes relation), live entirely
// here, not in Hatcheck.
package notebook

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
)

// Namespace is the Hatcheck name namespace used for notebook documents:
// each tag's current document is notebook/<tag> in Hatcheck's NameIndex.
const Namespace = "notebook"

// Document is the current, editable text for a tag.
type Document struct {
	Hash    string `json:"hash"`
	Content string `json:"content"`
}

// Post is a single entry in a tag's stream.
type Post struct {
	Hash    string `json:"hash"`
	Content string `json:"content"`
	Created string `json:"created,omitempty"`
	Failed  bool   `json:"failed,omitempty"`
	// Merged is true if this post has been merged into some version of the
	// document (possibly an earlier one than the current version). It
	// reflects that a merge happened, not that the post's text is still
	// present in the document now — later edits could have removed it.
	Merged bool `json:"merged,omitempty"`
	// Archived is true if the post is currently archived. Archived posts are
	// only returned when the caller asked for them (PageOptions.IncludeArchived).
	Archived bool `json:"archived,omitempty"`
}

// RelArchived is the relation predicate that records a post's archived
// state. It is a single predicate, with the state in the relation's "to"
// field (ArchivedTrue / ArchivedFalse), rather than separate "archived" and
// "unarchived" predicates: relations carry no timestamp, so entries under two
// different predicates can't be ordered against each other, whereas entries
// under one predicate keep their log order — and for a given post the last
// one wins. Archiving never changes the post itself (its hash and text stay
// as they were), so it is reversible and applies to the post in every
// notebook it appears in.
const (
	RelArchived   = "deep:archived"
	ArchivedTrue  = "true"
	ArchivedFalse = "false"
)

// Deep's other relation predicates. Hatcheck's relation index is one global
// keyspace shared by every client of the store, so Deep's vocabulary carries
// a "deep:" prefix to keep another app's "archived" (or similar) from being
// read as Deep's.
const (
	RelSupersedes = "deep:supersedes"
	RelMergedFrom = "deep:merged-from"
)

// Unprefixed predicates Deep wrote before the "deep:" prefix was adopted.
// They are still read so existing data keeps working, but never written. For
// archived state, legacy entries are replayed before prefixed ones: relations
// carry no timestamp, so entries under different predicates can't be ordered
// against each other, but every legacy entry was written before any prefixed
// one, so replaying them in that order lets the newer entry win.
const (
	legacyRelArchived   = "archived"
	legacyRelSupersedes = "supersedes"
	legacyRelMergedFrom = "merged-from"
)

// View is the composed response for a tag: its current document (nil if
// none exists yet) and its live stream, with every past document version
// already excluded.
type View struct {
	Tag      string    `json:"tag"`
	Document *Document `json:"document"`
	// Stream is one page of the tag's posts, newest first.
	Stream []Post `json:"stream"`
	// Total is how many posts the tag has in all that this view covers, not
	// just on this page: past document versions are never counted, and
	// archived posts are counted only when PageOptions.IncludeArchived is set.
	Total int `json:"total"`
	// ArchivedCount is how many of the tag's posts are archived, whether or
	// not this view includes them, so the UI can offer "Show N archived".
	ArchivedCount int `json:"archived_count"`
	// HasMore reports whether older posts exist beyond this page; if so,
	// NextBefore is the cursor to pass as PageOptions.Before to get them.
	HasMore    bool   `json:"has_more"`
	NextBefore string `json:"next_before,omitempty"`
}

// DefaultPageSize and MaxPageSize bound how many posts one GetNotebook call
// fetches. Each post costs Hatcheck round-trips, so an unbounded stream makes
// a large notebook unusably slow to open.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// fetchConcurrency is how many posts are fetched from Hatcheck in parallel.
const fetchConcurrency = 8

// ErrBadCursor is returned when PageOptions.Before names a post that is not
// in the tag's live stream (for example, a mistyped or stale cursor).
var ErrBadCursor = errors.New("notebook: unknown page cursor")

// PageOptions selects which page of a tag's stream GetNotebook returns.
type PageOptions struct {
	// Limit is the page size; 0 means DefaultPageSize, and anything above
	// MaxPageSize is capped.
	Limit int
	// Before is the cursor from the previous page's NextBefore: the page
	// returned holds the posts older than that post. Empty means the newest.
	Before string
	// IncludeArchived keeps archived posts in the stream (each flagged
	// Archived). By default they are hidden.
	IncludeArchived bool
}

// Service implements the Notebook application against a Hatcheck server.
type Service struct {
	Hatcheck *hatcheckclient.Client

	// versionInfoCache memoizes, per hash, its supersedes-predecessor and
	// the posts merged into it. Both are immutable facts once the relevant
	// relations are written — they never need invalidating — so a single
	// process-wide cache, shared across every request and every user, is
	// strictly correct, not just an optimization. (The equivalent cache in
	// notebook.html's earlier, client-side version was necessarily
	// per-browser-tab; here it's shared for real.)
	versionInfoCache sync.Map // hash (string) -> versionInfo
}

// versionInfo holds what a single hash's relations say about it: what it
// superseded (if anything) and what was merged into it (if anything).
type versionInfo struct {
	predecessor string   // "" if this is the first version
	mergedFrom  []string // post hashes merged into this version, if any
}

// NewService creates a Notebook Service backed by the given Hatcheck client.
func NewService(hc *hatcheckclient.Client) *Service {
	return &Service{Hatcheck: hc}
}

// ListNotebooks returns every tag that currently has a document, preceded by
// the always-present uncategorized tag.
func (s *Service) ListNotebooks(ctx context.Context, auth hatcheckclient.AuthContext) ([]string, error) {
	names, err := s.Hatcheck.Names(ctx, auth, Namespace)
	if err != nil {
		return nil, fmt.Errorf("listing notebooks: %w", err)
	}
	// #uncategorized is always listed first, even before it has a document,
	// so untagged quick notes always have a visible place to be found.
	tags := make([]string, 0, len(names)+1)
	tags = append(tags, UncategorizedTag)
	for _, n := range names {
		if n.Label != UncategorizedTag {
			tags = append(tags, n.Label)
		}
	}
	return tags, nil
}

// resolveVersionInfo returns hash's supersedes-predecessor and the posts
// merged into it, fetching /relations once and caching the result forever
// per Service's doc comment.
func (s *Service) resolveVersionInfo(ctx context.Context, auth hatcheckclient.AuthContext, hash string) (versionInfo, error) {
	if cached, ok := s.versionInfoCache.Load(hash); ok {
		return cached.(versionInfo), nil
	}

	outgoing, _, err := s.Hatcheck.Relations(ctx, auth, hash)
	if err != nil {
		// Deliberately not cached — a transient error here shouldn't be
		// remembered as "nothing found" forever.
		return versionInfo{}, err
	}

	var info versionInfo
	for _, r := range outgoing {
		switch r.Rel {
		case RelSupersedes, legacyRelSupersedes:
			if info.predecessor == "" {
				info.predecessor = r.To
			}
		case RelMergedFrom, legacyRelMergedFrom:
			info.mergedFrom = append(info.mergedFrom, r.To)
		}
	}
	s.versionInfoCache.Store(hash, info)
	return info, nil
}

// documentVersionChain walks the supersedes chain backward from startHash,
// returning the set of every hash in it (startHash included) and the set
// of every post hash merged into any version in that chain. The name
// index only ever holds the current hash, but nothing in an event-sourced
// log actually discards history — this just reads back what SaveDocument's
// relations record, so the stream can exclude every past version (not only
// the current one) and flag every post that was ever merged in, even if
// via an earlier version than the one currently live.
func (s *Service) documentVersionChain(ctx context.Context, auth hatcheckclient.AuthContext, startHash string) (versions map[string]bool, merged map[string]bool, err error) {
	versions = make(map[string]bool)
	merged = make(map[string]bool)
	current := startHash
	for current != "" && !versions[current] {
		versions[current] = true
		info, err := s.resolveVersionInfo(ctx, auth, current)
		if err != nil {
			// Stop walking rather than fail the whole view — a partial
			// chain still excludes/flags what it found, which is strictly
			// better than finding nothing.
			break
		}
		for _, m := range info.mergedFrom {
			merged[m] = true
		}
		current = info.predecessor
	}
	return versions, merged, nil
}

// currentDocumentHash resolves notebook/<tag> to its current hash, or ""
// if the tag has no document yet.
func (s *Service) currentDocumentHash(ctx context.Context, auth hatcheckclient.AuthContext, tag string) (string, error) {
	names, err := s.Hatcheck.Names(ctx, auth, Namespace)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if n.Label == tag {
			return n.Hash, nil
		}
	}
	return "", nil
}

// GetNotebook assembles the view for a tag: its current document (if any)
// plus one page of its stream, with every past document version excluded.
// This single call replaces what used to be five-plus independent, unordered
// round-trips from the browser.
//
// The stream is paged because opening a post costs Hatcheck round-trips:
// fetching every post in a tag with thousands of them made the notebook
// effectively impossible to open. Hatcheck's tag query returns hashes in the
// order they were stored, so Deep pages from the newest end without fetching
// the posts it skips, and the cursor is the hash of the last post returned.
func (s *Service) GetNotebook(ctx context.Context, auth hatcheckclient.AuthContext, tag string, page PageOptions) (*View, error) {
	docHash, err := s.currentDocumentHash(ctx, auth, tag)
	if err != nil {
		return nil, fmt.Errorf("resolving document: %w", err)
	}

	var doc *Document
	if docHash != "" {
		content, err := s.Hatcheck.Fetch(ctx, auth, docHash)
		if err != nil {
			return nil, fmt.Errorf("fetching document: %w", err)
		}
		doc = &Document{Hash: docHash, Content: content}
	}

	hashes, err := s.Hatcheck.QueryTag(ctx, auth, tag)
	if err != nil {
		return nil, fmt.Errorf("querying tag stream: %w", err)
	}

	var superseded map[string]bool
	var merged map[string]bool
	if docHash != "" {
		superseded, merged, err = s.documentVersionChain(ctx, auth, docHash)
		if err != nil {
			return nil, fmt.Errorf("walking document version chain: %w", err)
		}
	}

	archived, err := s.archivedPosts(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("reading archived posts: %w", err)
	}

	// Newest first, with every past document version left out.
	live := make([]string, 0, len(hashes))
	for i := len(hashes) - 1; i >= 0; i-- {
		if !superseded[hashes[i]] {
			live = append(live, hashes[i])
		}
	}

	// The cursor is resolved against the live list *before* archived posts
	// are removed, so a cursor that names a post archived since the previous
	// page was fetched still works instead of turning into ErrBadCursor.
	cursor := -1
	if page.Before != "" {
		for i, h := range live {
			if h == page.Before {
				cursor = i
				break
			}
		}
		if cursor < 0 {
			return nil, ErrBadCursor
		}
	}

	// Archived posts are dropped here, before the list is sliced into pages,
	// so every page is full and Total counts what the caller can actually
	// page through. (Filtering after slicing would leave gaps in pages.)
	total, archivedCount := 0, 0
	var remaining []string // visible posts older than the cursor
	for i, h := range live {
		isArchived := archived[h]
		if isArchived {
			archivedCount++
			if !page.IncludeArchived {
				continue
			}
		}
		total++
		if i > cursor {
			remaining = append(remaining, h)
		}
	}

	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	end := limit
	if end > len(remaining) {
		end = len(remaining)
	}

	posts := s.fetchPosts(ctx, auth, remaining[:end])
	for i := range posts {
		posts[i].Merged = merged[posts[i].Hash]
		posts[i].Archived = archived[posts[i].Hash]
	}

	view := &View{Tag: tag, Document: doc, Stream: posts, Total: total, ArchivedCount: archivedCount}
	if end < len(remaining) && len(posts) > 0 {
		view.HasMore = true
		view.NextBefore = posts[len(posts)-1].Hash
	}
	return view, nil
}

// archivedPosts returns the set of post hashes that are currently archived.
// It reads every RelArchived relation in one request and replays them in log
// order, so for each post the most recent entry decides: archived, then
// unarchived, then archived again leaves it archived.
func (s *Service) archivedPosts(ctx context.Context, auth hatcheckclient.AuthContext) (map[string]bool, error) {
	legacy, err := s.Hatcheck.RelationsByPredicate(ctx, auth, legacyRelArchived)
	if err != nil {
		return nil, err
	}
	current, err := s.Hatcheck.RelationsByPredicate(ctx, auth, RelArchived)
	if err != nil {
		return nil, err
	}
	rels := append(legacy, current...)
	archived := make(map[string]bool)
	for _, r := range rels {
		archived[r.From] = r.To == ArchivedTrue
	}
	// Drop the posts whose latest entry was "unarchived", so the map holds
	// only what is archived now.
	for h, isArchived := range archived {
		if !isArchived {
			delete(archived, h)
		}
	}
	return archived, nil
}

// SetArchived archives or unarchives the post with the given hash by
// recording a relation on it; the post itself is never modified. It is safe
// to repeat — the latest entry wins — and it applies to the post in every
// notebook it appears in.
func (s *Service) SetArchived(ctx context.Context, auth hatcheckclient.AuthContext, hash string, archived bool) error {
	state := ArchivedFalse
	if archived {
		state = ArchivedTrue
	}
	if err := s.Hatcheck.CreateRelation(ctx, auth, hash, RelArchived, state); err != nil {
		return fmt.Errorf("recording archived state: %w", err)
	}
	return nil
}

// fetchPosts fetches the given posts from Hatcheck a few at a time,
// returning them in the same order as hashes.
func (s *Service) fetchPosts(ctx context.Context, auth hatcheckclient.AuthContext, hashes []string) []Post {
	posts := make([]Post, len(hashes))
	sem := make(chan struct{}, fetchConcurrency)
	var wg sync.WaitGroup
	for i, h := range hashes {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			posts[i] = s.fetchPost(ctx, auth, h)
		}(i, h)
	}
	wg.Wait()
	return posts
}

// fetchPost retrieves a post's content and metadata, returning a Post
// marked Failed rather than erroring the whole view if either call fails —
// a load failure should be visible in the response, not silently dropped
// or allowed to break every other post in the stream.
func (s *Service) fetchPost(ctx context.Context, auth hatcheckclient.AuthContext, hash string) Post {
	content, contentErr := s.Hatcheck.Fetch(ctx, auth, hash)
	meta, metaErr := s.Hatcheck.ObjectMetaFor(ctx, auth, hash)
	if contentErr != nil || metaErr != nil {
		return Post{Hash: hash, Failed: true}
	}
	return Post{Hash: hash, Content: content, Created: meta.Created}
}

// SaveDocument writes content as the new document for tag: it resolves the
// tag's current document itself (rather than trusting a client-supplied
// "previous hash", which removes an entire class of client-side race),
// stashes the new content, repoints notebook/<tag> at it, and — if a
// previous version existed — records a supersedes relation from the new
// version back to it. mergedHashes are the post hashes the client actually
// merged into this save (tracked client-side, since merging itself is a
// client-side textarea edit Deep has no visibility into) — one merged-from
// relation is recorded per hash, so the stream can later show which posts
// have been merged in.
func (s *Service) SaveDocument(ctx context.Context, auth hatcheckclient.AuthContext, tag, content string, mergedHashes []string) (Document, error) {
	previousHash, err := s.currentDocumentHash(ctx, auth, tag)
	if err != nil {
		return Document{}, fmt.Errorf("resolving previous document: %w", err)
	}

	stashed, err := s.Hatcheck.Stash(ctx, auth, content)
	if err != nil {
		return Document{}, fmt.Errorf("stashing document: %w", err)
	}

	// /name requires PermWrite; the caller's ambient capability is normally
	// a PermRead wildcard. Stash's own hash-scoped write capability for the
	// object just created is the one that satisfies this check.
	if err := s.Hatcheck.SetName(ctx, auth, Namespace, tag, stashed.Hash, stashed.Capability); err != nil {
		return Document{}, fmt.Errorf("pointing notebook name at new document: %w", err)
	}

	if previousHash != "" {
		if err := s.Hatcheck.CreateRelation(ctx, auth, stashed.Hash, RelSupersedes, previousHash); err != nil {
			// The document itself saved successfully — repointing the name
			// already succeeded above — so this isn't fatal to the save.
			// But without this relation, the old version will show up in
			// the stream like an ordinary post, so it's surfaced as an
			// error rather than silently swallowed.
			return Document{Hash: stashed.Hash, Content: content},
				fmt.Errorf("document saved, but recording its predecessor failed (the old version may reappear in the stream): %w", err)
		}
	}

	for _, sourceHash := range mergedHashes {
		if err := s.Hatcheck.CreateRelation(ctx, auth, stashed.Hash, RelMergedFrom, sourceHash); err != nil {
			// Same reasoning as above: the document itself saved fine, but
			// this post won't show as merged until this relation exists.
			return Document{Hash: stashed.Hash, Content: content},
				fmt.Errorf("document saved, but recording a merge from %s failed (it may not show as merged): %w", sourceHash, err)
		}
	}

	return Document{Hash: stashed.Hash, Content: content}, nil
}

// UncategorizedTag is the tag given to a quick-compose post that carries no
// tag of its own, so every post is reachable through some tag's stream.
const UncategorizedTag = "uncategorized"

// hashtagPattern matches an inline #hashtag: a # at the start of the text or
// after whitespace, followed by a word character. A Markdown heading such as
// "# Title" (# then a space) is therefore not a tag.
var hashtagPattern = regexp.MustCompile(`(^|\s)#[\p{L}\p{N}_-]+`)

// hasHashtag reports whether content mentions at least one inline #hashtag.
func hasHashtag(content string) bool {
	return hashtagPattern.MatchString(content)
}

// CreatePlainPost stashes a post from the quick-compose editor, which the
// user can use without opening (or even knowing) any particular tag first.
// Content that already mentions a tag is stashed exactly as given and shows
// up in that tag's stream, since tag membership is just a property of the
// content. Content with no tag at all gets #uncategorized appended, so it
// can still be found in that tag's stream instead of being unreachable.
func (s *Service) CreatePlainPost(ctx context.Context, auth hatcheckclient.AuthContext, content string) (Post, error) {
	if !hasHashtag(content) {
		content = strings.TrimRight(content, "\n") + "\n\n#" + UncategorizedTag
	}
	stashed, err := s.Hatcheck.Stash(ctx, auth, content)
	if err != nil {
		return Post{}, fmt.Errorf("stashing post: %w", err)
	}
	return Post{Hash: stashed.Hash, Content: content}, nil
}

// CreatePost stashes content as a new post tagged with tag, ensuring the
// tag is actually present in the content — Hatcheck only indexes tags it
// finds inline in the text, so a post created without the tag literally
// present wouldn't show up in its own tag's stream.
func (s *Service) CreatePost(ctx context.Context, auth hatcheckclient.AuthContext, tag, content string) (Post, error) {
	tagToken := "#" + tag
	hasTag := false
	for _, word := range strings.Fields(content) {
		if word == tagToken {
			hasTag = true
			break
		}
	}
	if !hasTag {
		content = strings.TrimRight(content, "\n") + "\n\n" + tagToken
	}

	stashed, err := s.Hatcheck.Stash(ctx, auth, content)
	if err != nil {
		return Post{}, fmt.Errorf("stashing post: %w", err)
	}
	return Post{Hash: stashed.Hash, Content: content}, nil
}
