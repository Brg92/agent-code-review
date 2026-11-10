package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
)

// DefaultCacheBytes bounds the in-memory ETag cache of one Client.
const DefaultCacheBytes = 64 << 20

// cachedResponse is a 2xx response kept for conditional requests.
type cachedResponse struct {
	etag   string
	status int
	header http.Header
	body   []byte
	url    string
}

// response returns a copy the caller may change freely.
func (e cachedResponse) response() *Response {
	return &Response{
		StatusCode: e.status,
		Header:     e.header.Clone(),
		Body:       append([]byte(nil), e.body...),
		URL:        e.url,
		FromCache:  true,
	}
}

// etagCache is a per-process, in-memory cache; nothing is written to disk.
// When an entry does not fit, the cache starts over.
type etagCache struct {
	mu      sync.Mutex
	max     int
	size    int
	entries map[string]cachedResponse
}

func newETagCache(maxBytes int) *etagCache {
	return &etagCache{max: maxBytes, entries: map[string]cachedResponse{}}
}

func (c *etagCache) get(key string) (cachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *etagCache) put(key string, e cachedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.size -= len(old.body)
		delete(c.entries, key)
	}
	if len(e.body) > c.max {
		return
	}
	if c.size+len(e.body) > c.max {
		c.entries = map[string]cachedResponse{}
		c.size = 0
	}
	c.entries[key] = e
	c.size += len(e.body)
}

// cacheKey separates entries by URL, Accept and token, so one identity
// never sees what another one read. Only a hash of the token is kept.
func cacheKey(rawURL, accept, token string) string {
	sum := sha256.Sum256([]byte(token))
	return accept + "\n" + rawURL + "\n" + hex.EncodeToString(sum[:])
}
