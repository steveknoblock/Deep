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

	// predecessorCache memoizes a hash's supersedes-predecessor. This is an
	// immutable fact once a supersedes relation is written — it never needs
	// invalidating — so a single process-wide cache, shared across every
	// request and every user, is strictly correct, not just an optimization.
	// (The equivalent cache in notebook.html's earlier, client-side version
	// was necessarily per-browser-tab; here it's shared for real.)
	predecessorCache sync.Map // hash (string) -> predecessor hash (string, "" if none)
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

// resolvePredecessor returns hash's supersedes-predecessor, or "" if it has
// none (either it's the first version, or the relation lookup found
// nothing). Results are cached forever, per Service's doc comment.
func (s *Service) resolvePredecessor(ctx context.Context, auth hatcheckclient.AuthContext, hash string) (string, error) {
	if cached, ok := s.predecessorCache.Load(hash); ok {
		return cached.(string), nil
	}

	outgoing, _, err := s.Hatcheck.Relations(ctx, auth, hash)
	if err != nil {
		// Deliberately not cached — a transient error here shouldn't be
		// remembered as "no predecessor" forever.
		return "", err
	}

	pred := ""
	for _, r := range outgoing {
		if r.Rel == "supersedes" {
			pred = r.To
			break
		}
	}
	s.predecessorCache.Store(hash, pred)
	return pred, nil
}

// documentVersionChain walks the supersedes chain backward from startHash,
// returning the set of every hash in it (startHash included). The name
// index only ever holds the current hash, but nothing in an event-sourced
// log actually discards history — this just reads back what SaveDocument's
// relations record, so the stream can exclude every past version, not only
// the current one.
func (s *Service) documentVersionChain(ctx context.Context, auth hatcheckclient.AuthContext, startHash string) (map[string]bool, error) {
	chain := make(map[string]bool)
	current := startHash
	for current != "" && !chain[current] {
		chain[current] = true
		pred, err := s.resolvePredecessor(ctx, auth, current)
		if err != nil {
			// Stop walking rather than fail the whole view — a partial
			// chain still excludes what it found, which is strictly
			// better than excluding nothing.
			break
		}
		current = pred
	}
	return chain, nil
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
	if docHash != "" {
		superseded, err = s.documentVersionChain(ctx, auth, docHash)
		if err != nil {
			return nil, fmt.Errorf("walking document version chain: %w", err)
		}
	}

	posts := make([]Post, 0, len(hashes))
	for _, hash := range hashes {
		if superseded[hash] {
			continue
		}
		posts = append(posts, s.fetchPost(ctx, auth, hash))
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
// version back to it.
func (s *Service) SaveDocument(ctx context.Context, auth hatcheckclient.AuthContext, tag, content string) (Document, error) {
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

	return Document{Hash: stashed.Hash, Content: content}, nil
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
