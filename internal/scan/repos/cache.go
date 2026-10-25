package repos

import (
	"context"
	"sync"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// contentCache holds, for one ScanAllRepos call, the secret matches of the
// contents already scanned, so that worktrees and clones of one repository
// do not scan the same content again. It lives in memory only and is never
// persisted. It holds secrets.SecretMatch values (pattern, type and
// redacted preview), never a secret in clear. Safe for concurrent use: two
// workers scanning the same content compute the same matches, and the first
// stored wins.
type contentCache struct {
	mu      sync.Mutex
	blobs   map[blobKey][]secrets.SecretMatch
	commits map[string][]secrets.SecretMatch
}

// blobKey identifies a tracked file's content: its blob id in the index and
// the size of the file on disk (a different size, e.g. from line-ending
// conversion, is scanned again).
type blobKey struct {
	blob string
	size int64
}

func newContentCache() *contentCache {
	return &contentCache{blobs: map[blobKey][]secrets.SecretMatch{}, commits: map[string][]secrets.SecretMatch{}}
}

func (c *contentCache) blob(k blobKey) ([]secrets.SecretMatch, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.blobs[k]
	return m, ok
}

func (c *contentCache) putBlob(k blobKey, m []secrets.SecretMatch) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.blobs[k]; !ok {
		c.blobs[k] = m
	}
}

func (c *contentCache) commit(h string) ([]secrets.SecretMatch, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.commits[h]
	return m, ok
}

func (c *contentCache) putCommit(h string, m []secrets.SecretMatch) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.commits[h]; !ok {
		c.commits[h] = m
	}
}

type cacheKey struct{}

func withCache(ctx context.Context, c *contentCache) context.Context {
	return context.WithValue(ctx, cacheKey{}, c)
}

// cacheOf is the run's cache; nil (no caching) when ScanRepo runs alone.
func cacheOf(ctx context.Context) *contentCache {
	c, _ := ctx.Value(cacheKey{}).(*contentCache)
	return c
}
