// Package notebook implements the Notebook application on top of Hatcheck's
// generic primitives (stashes, tags, names, relations). Hatcheck itself has
// no notion of a "document" or a "post" — those concepts, and the rules for
// how they compose (a document is a post referenced by notebook/<tag>; a
// version is superseded by writing a supersedes relation), live entirely
// here, not in Hatcheck.
package notebook

import (
	"context"
	"fmt"
	"sort"
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
}

// View is the composed response for a tag: its current document (nil if
// none exists yet) and its live stream, with every past document version
// already excluded.
type View struct {
	Tag      string    `json:"tag"`
	Document *Document `json:"document"`
	Stream   []Post    `json:"stream"`
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

// ListNotebooks returns every tag that currently has a document.
func (s *Service) ListNotebooks(ctx context.Context, auth hatcheckclient.AuthContext) ([]string, error) {
	names, err := s.Hatcheck.Names(ctx, auth, Namespace)
	if err != nil {
		return nil, fmt.Errorf("listing notebooks: %w", err)
	}
	tags := make([]string, 0, len(names))
	for _, n := range names {
		tags = append(tags, n.Label)
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
		case "supersedes":
			if info.predecessor == "" {
				info.predecessor = r.To
			}
		case "merged-from":
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

// GetNotebook assembles the full view for a tag: its current document (if
// any) plus its stream, with every past document version excluded. This
// single call replaces what used to be five-plus independent, unordered
// round-trips from the browser.
func (s *Service) GetNotebook(ctx context.Context, auth hatcheckclient.AuthContext, tag string) (*View, error) {
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

	posts := make([]Post, 0, len(hashes))
	for _, hash := range hashes {
		if superseded[hash] {
			continue
		}
		post := s.fetchPost(ctx, auth, hash)
		post.Merged = merged[hash]
		posts = append(posts, post)
	}

	sort.Slice(posts, func(i, j int) bool {
		// Posts with no known creation time (failed loads) sort last.
		if posts[i].Created == "" {
			return false
		}
		if posts[j].Created == "" {
			return true
		}
		return posts[i].Created > posts[j].Created
	})

	return &View{Tag: tag, Document: doc, Stream: posts}, nil
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
		if err := s.Hatcheck.CreateRelation(ctx, auth, stashed.Hash, "supersedes", previousHash); err != nil {
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
		if err := s.Hatcheck.CreateRelation(ctx, auth, stashed.Hash, "merged-from", sourceHash); err != nil {
			// Same reasoning as above: the document itself saved fine, but
			// this post won't show as merged until this relation exists.
			return Document{Hash: stashed.Hash, Content: content},
				fmt.Errorf("document saved, but recording a merge from %s failed (it may not show as merged): %w", sourceHash, err)
		}
	}

	return Document{Hash: stashed.Hash, Content: content}, nil
}

// CreatePlainPost stashes content exactly as given, with no forced tag —
// unlike CreatePost, which always ensures a specific tag's notebook is
// present. This is for the quick-compose editor: the user can just start
// typing without opening (or even knowing) any particular tag first. If
// the content happens to mention a tag inline, it'll naturally show up in
// that tag's stream later — nothing extra is needed to wire that up, since
// tag membership is just a property of the content itself.
func (s *Service) CreatePlainPost(ctx context.Context, auth hatcheckclient.AuthContext, content string) (Post, error) {
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
