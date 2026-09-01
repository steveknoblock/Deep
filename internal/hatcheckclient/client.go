// Package hatcheckclient is a thin HTTP client for Hatcheck's web API.
//
// Deep holds no credentials of its own and authenticates nothing itself —
// every call here takes an AuthContext carrying the session JWT and
// capability token the browser already obtained by logging into Hatcheck
// directly. Deep's job is orchestration on top of Hatcheck's primitives,
// not identity or access control, which stay entirely Hatcheck's concern.
package hatcheckclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// AuthContext carries the caller's identity through to Hatcheck. SessionJWT
// is required for every call. CapabilityToken is the ambient capability the
// caller holds — typically a wildcard read capability from login — and is
// used for any call that doesn't need a more specific one.
type AuthContext struct {
	SessionJWT      string
	CapabilityToken string
}

// Client talks to a single Hatcheck server over HTTP.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New creates a Client for the given Hatcheck base URL (e.g.
// "http://localhost:8090"), with no trailing slash expected.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{},
	}
}

// ObjectMeta mirrors Hatcheck's GET /object-meta response.
type ObjectMeta struct {
	Kind    string   `json:"kind"`
	Created string   `json:"created"`
	Tags    []string `json:"tags,omitempty"`
}

// NameEntry mirrors one entry of Hatcheck's GET /names response.
type NameEntry struct {
	Label string `json:"label"`
	Hash  string `json:"hash"`
	Kind  string `json:"kind,omitempty"`
}

// Relation mirrors a single Hatcheck relation, as returned by GET /relations.
type Relation struct {
	Hash string `json:"hash"`
	From string `json:"from"`
	Rel  string `json:"rel"`
	To   string `json:"to"`
}

// StashResult mirrors Hatcheck's POST /stash response. Capability is the
// hash-scoped write capability Hatcheck issues for the object just created —
// callers that need to write-protect a follow-up call (e.g. SetName) should
// pass this through as that call's capability override, not the ambient one.
type StashResult struct {
	Hash       string          `json:"hash"`
	Capability json.RawMessage `json:"capability"`
}

// doRequest performs an HTTP request against Hatcheck, attaching the given
// auth. If capabilityOverride is non-empty, it's used as X-Capability-Token
// instead of auth.CapabilityToken — needed for calls like SetName that
// require a specific object-scoped capability rather than the caller's
// ambient one.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte, auth AuthContext, capabilityOverride string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	if auth.SessionJWT == "" {
		return nil, fmt.Errorf("hatcheckclient: missing session JWT")
	}
	req.Header.Set("Authorization", "Bearer "+auth.SessionJWT)

	capToken := auth.CapabilityToken
	if capabilityOverride != "" {
		capToken = capabilityOverride
	}
	if capToken != "" {
		req.Header.Set("X-Capability-Token", capToken)
	}

	return c.HTTP.Do(req)
}

// readAndCheck reads the full response body and returns an error if the
// status code isn't 2xx, including the body text (Hatcheck's handlers
// return plain-text error messages) for context.
func readAndCheck(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("hatcheck returned %d: %s", res.StatusCode, string(body))
	}
	return body, nil
}

// Stash stores content as a new CAS object and returns its hash plus the
// hash-scoped write capability Hatcheck issues for it.
func (c *Client) Stash(ctx context.Context, auth AuthContext, content string) (StashResult, error) {
	res, err := c.doRequest(ctx, http.MethodPost, "/stash", []byte(content), auth, "")
	if err != nil {
		return StashResult{}, err
	}
	body, err := readAndCheck(res)
	if err != nil {
		return StashResult{}, err
	}
	var result StashResult
	if err := json.Unmarshal(body, &result); err != nil {
		return StashResult{}, fmt.Errorf("hatcheckclient: malformed stash response: %w", err)
	}
	return result, nil
}

// Fetch retrieves the raw content of an object by hash.
func (c *Client) Fetch(ctx context.Context, auth AuthContext, hash string) (string, error) {
	res, err := c.doRequest(ctx, http.MethodGet, "/fetch?hash="+url.QueryEscape(hash), nil, auth, "")
	if err != nil {
		return "", err
	}
	body, err := readAndCheck(res)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ObjectMetaFor retrieves per-hash metadata (kind, creation time, tags).
func (c *Client) ObjectMetaFor(ctx context.Context, auth AuthContext, hash string) (ObjectMeta, error) {
	res, err := c.doRequest(ctx, http.MethodGet, "/object-meta?hash="+url.QueryEscape(hash), nil, auth, "")
	if err != nil {
		return ObjectMeta{}, err
	}
	body, err := readAndCheck(res)
	if err != nil {
		return ObjectMeta{}, err
	}
	var meta ObjectMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return ObjectMeta{}, fmt.Errorf("hatcheckclient: malformed object-meta response: %w", err)
	}
	return meta, nil
}

// QueryTag returns every hash tagged with the given tag.
func (c *Client) QueryTag(ctx context.Context, auth AuthContext, tag string) ([]string, error) {
	path := "/query?index=tag&key=" + url.QueryEscape(tag)
	res, err := c.doRequest(ctx, http.MethodGet, path, nil, auth, "")
	if err != nil {
		return nil, err
	}
	body, err := readAndCheck(res)
	if err != nil {
		return nil, err
	}
	var hashes []string
	if err := json.Unmarshal(body, &hashes); err != nil {
		return nil, fmt.Errorf("hatcheckclient: malformed query response: %w", err)
	}
	return hashes, nil
}

// Names lists every name in the given namespace.
func (c *Client) Names(ctx context.Context, auth AuthContext, namespace string) ([]NameEntry, error) {
	path := "/names?namespace=" + url.QueryEscape(namespace)
	res, err := c.doRequest(ctx, http.MethodGet, path, nil, auth, "")
	if err != nil {
		return nil, err
	}
	body, err := readAndCheck(res)
	if err != nil {
		return nil, err
	}
	var names []NameEntry
	if err := json.Unmarshal(body, &names); err != nil {
		return nil, fmt.Errorf("hatcheckclient: malformed names response: %w", err)
	}
	return names, nil
}

// SetName points namespace/label at hash, creating or updating it as needed.
// capability must be the hash-scoped write capability for hash — typically
// the one returned by the Stash call that produced it — since /name requires
// PermWrite and the caller's ambient capability is usually read-only.
func (c *Client) SetName(ctx context.Context, auth AuthContext, namespace, label, hash string, capability json.RawMessage) error {
	path := fmt.Sprintf("/name?namespace=%s&label=%s&hash=%s",
		url.QueryEscape(namespace), url.QueryEscape(label), url.QueryEscape(hash))
	res, err := c.doRequest(ctx, http.MethodPost, path, nil, auth, string(capability))
	if err != nil {
		return err
	}
	_, err = readAndCheck(res)
	return err
}

// Relations returns every relation with the given hash on either end.
func (c *Client) Relations(ctx context.Context, auth AuthContext, hash string) (outgoing, incoming []Relation, err error) {
	path := "/relations?hash=" + url.QueryEscape(hash)
	res, reqErr := c.doRequest(ctx, http.MethodGet, path, nil, auth, "")
	if reqErr != nil {
		return nil, nil, reqErr
	}
	body, readErr := readAndCheck(res)
	if readErr != nil {
		return nil, nil, readErr
	}
	var parsed struct {
		Outgoing []Relation `json:"outgoing"`
		Incoming []Relation `json:"incoming"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, fmt.Errorf("hatcheckclient: malformed relations response: %w", err)
	}
	return parsed.Outgoing, parsed.Incoming, nil
}

// CreateRelation records a new relation. Hatcheck's /relation endpoint
// doesn't check capability at all, so no capability override is needed here.
func (c *Client) CreateRelation(ctx context.Context, auth AuthContext, from, rel, to string) error {
	body, err := json.Marshal(struct {
		From string `json:"from"`
		Rel  string `json:"rel"`
		To   string `json:"to"`
	}{From: from, Rel: rel, To: to})
	if err != nil {
		return err
	}
	res, err := c.doRequest(ctx, http.MethodPost, "/relation", body, auth, "")
	if err != nil {
		return err
	}
	_, err = readAndCheck(res)
	return err
}
