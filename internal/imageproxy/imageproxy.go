// Package imageproxy signs image URLs and fetches them through checked egress.
package imageproxy

import (
	"bytes"
	"container/list"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/sanitize"
	"golang.org/x/net/html"
)

const maxImageSize = 10 << 20
const imageTimeout = 30 * time.Second

const (
	// imageCacheBytes and imageCacheEntries bound the in-memory image cache.
	imageCacheBytes   = 64 << 20
	imageCacheEntries = 512
	// imageCacheTTL is how long a cached body answers views and browser
	// revalidations before the proxy revalidates with the publisher. It
	// exceeds the browser max-age the proxy advertises (86400s), so daily
	// revalidations land inside the cache window and never reach the
	// publisher; repeat views cost no egress and reveal nothing upstream.
	imageCacheTTL = 72 * time.Hour
)

// cachedImage is one image body with its validators.
type cachedImage struct {
	body    []byte
	kind    string
	etag    string // body-derived validator sent to clients
	upETag  string // the publisher's validator, for cheap revalidation
	lastMod string
	added   time.Time
}

// imageCache is a bounded LRU over image bodies keyed by source URL. It is
// process-local: a restart simply refetches.
type imageCache struct {
	mu      sync.Mutex
	maxByte int64
	bytes   int64
	items   map[string]*list.Element
	order   *list.List // front is most recent
}

type imageCacheItem struct {
	key   string
	image cachedImage
}

func newImageCache() *imageCache {
	return &imageCache{maxByte: imageCacheBytes, items: map[string]*list.Element{}, order: list.New()}
}

func (c *imageCache) get(key string) (cachedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return cachedImage{}, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*imageCacheItem).image, true
}

func (c *imageCache) put(key string, image cachedImage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		item := elem.Value.(*imageCacheItem)
		c.bytes += int64(len(image.body)) - int64(len(item.image.body))
		item.image = image
		c.order.MoveToFront(elem)
	} else {
		c.items[key] = c.order.PushFront(&imageCacheItem{key: key, image: image})
		c.bytes += int64(len(image.body))
	}
	for c.bytes > c.maxByte || c.order.Len() > imageCacheEntries {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		item := oldest.Value.(*imageCacheItem)
		c.order.Remove(oldest)
		delete(c.items, item.key)
		c.bytes -= int64(len(item.image.body))
	}
}

// Mode controls whether images load automatically. Use ClickToLoad for
// agent-selected entries: the proxy's cache is a view-time optimization, not
// an ingest-time copy of the bytes, so a cache miss would still let a
// publisher observe which entry an agent selected.
type Mode uint8

const (
	ClickToLoad Mode = iota
	Proxied
)

// Proxy issues signed URLs and serves checked image responses.
type Proxy struct {
	key        []byte
	client     *outbound.Client
	base       *url.URL
	extraPorts []uint16
	cache      *imageCache
	ttl        time.Duration
}

// New creates a proxy using a deployment secret and the public origin at
// which /img/ is served. The secret must have at least 32 bytes of entropy.
// Extra ports must correspond to explicitly allowed outbound destinations.
func New(secret []byte, client *outbound.Client, origin string, extraPorts ...uint16) (*Proxy, error) {
	if len(secret) < 32 || client == nil {
		return nil, errors.New("imageproxy: secret or client missing")
	}
	u, err := url.Parse(origin)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("imageproxy: invalid origin")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "earnest/image-proxy/v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive image key: %w", err)
	}
	return &Proxy{key: key, client: client, base: u, extraPorts: append([]uint16(nil), extraPorts...), cache: newImageCache(), ttl: imageCacheTTL}, nil
}

// URL returns a signed proxy URL for a source image.
func (p *Proxy) URL(source *url.URL) (*url.URL, error) {
	if source == nil || (source.Scheme != "http" && source.Scheme != "https") || source.Host == "" || source.User != nil {
		return nil, errors.New("imageproxy: invalid source URL")
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(source.String()))
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(encoded))
	u := *p.base
	u.Path = "/img/" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "/" + encoded
	return &u, nil
}

// Rewrite replaces image URLs in sanitised HTML. ClickToLoad emits links,
// with no image sources that a browser could fetch automatically.
func (p *Proxy) Rewrite(input string, mode Mode) (string, error) {
	if mode != Proxied && mode != ClickToLoad {
		return "", errors.New("imageproxy: invalid mode")
	}
	rewritten, err := sanitize.RewriteImages(input, func(source *url.URL) (*url.URL, error) {
		if source.User != nil || source.Host == "" || (source.Scheme != "http" && source.Scheme != "https") {
			return nil, nil
		}
		return p.URL(source)
	})
	if err != nil {
		return "", fmt.Errorf("rewrite images: %w", err)
	}
	if mode == Proxied {
		return rewritten, nil
	}
	doc, err := html.Parse(strings.NewReader(rewritten))
	if err != nil {
		return "", fmt.Errorf("parse rewritten html: %w", err)
	}
	var body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
		}
		if n.Type == html.ElementNode && n.Data == "img" {
			var target string
			for _, a := range n.Attr {
				if a.Key == "src" {
					target = a.Val
					break
				}
			}
			if target == "" {
				for _, a := range n.Attr {
					if a.Key == "srcset" {
						target = strings.Fields(strings.Split(a.Val, ",")[0])[0]
						break
					}
				}
			}
			n.Data = "a"
			n.DataAtom = 0
			n.Attr = []html.Attribute{{Key: "href", Val: target}, {Key: "rel", Val: "noreferrer noopener"}, {Key: "target", Val: "_blank"}}
			n.AppendChild(&html.Node{Type: html.TextNode, Data: "Load image"})
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	var b bytes.Buffer
	if body != nil {
		for n := body.FirstChild; n != nil; n = n.NextSibling {
			if err := html.Render(&b, n); err != nil {
				return "", fmt.Errorf("render html: %w", err)
			}
		}
	}
	return b.String(), nil
}

// ServeHTTP serves only signed GET requests. Signed destinations still pass
// the outbound client's address and redirect checks on every fetch.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 4 || parts[1] != "img" || len(parts[3]) > 16384 {
		http.NotFound(w, r)
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(parts[3]))
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		http.NotFound(w, r)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	source := string(raw)
	image, hit := p.cache.get(source)
	// stale keeps answering views from the last good body when the publisher
	// is unreachable or changes its mind about the media type, and pushes the
	// next revalidation out so a dead publisher is not retried per view.
	stale := func() {
		image.added = time.Now()
		p.cache.put(source, image)
		p.serve(w, r, image)
	}
	if hit && time.Since(image.added) < p.ttl {
		p.serve(w, r, image)
		return
	}
	policy := outbound.Policy{Purpose: outbound.Image, MaxBodySize: maxImageSize, Timeout: imageTimeout, ExtraPorts: p.extraPorts}
	if hit {
		policy.ETag = image.upETag
		policy.LastModified = image.lastMod
	}
	result, err := p.client.Get(r.Context(), source, policy)
	if err != nil || result == nil {
		if hit {
			stale()
			return
		}
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode == http.StatusNotModified && hit {
		stale()
		return
	}
	if result.Response.StatusCode != http.StatusOK {
		if hit {
			stale()
			return
		}
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	data, err := io.ReadAll(result.Response.Body)
	if err != nil {
		if hit {
			stale()
			return
		}
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	kind := http.DetectContentType(data)
	// DetectContentType does not recognize the AVIF ISO-BMFF brand.
	if kind == "application/octet-stream" && isAVIF(data) {
		kind = "image/avif"
	}
	switch kind {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif":
	default:
		if hit {
			stale()
			return
		}
		http.Error(w, "unsupported image", http.StatusUnsupportedMediaType)
		return
	}
	sum := sha256.Sum256(data)
	validator := result.Response.Header.Get("ETag")
	if validator == "" {
		// Mint a validator when the publisher has none, so browser
		// revalidation can always be answered from the cache.
		validator = `"` + hex.EncodeToString(sum[:8]) + `"`
	}
	image = cachedImage{
		body:    data,
		kind:    kind,
		etag:    validator,
		upETag:  result.Response.Header.Get("ETag"),
		lastMod: result.Response.Header.Get("Last-Modified"),
		added:   time.Now(),
	}
	p.cache.put(source, image)
	p.serve(w, r, image)
}

// serve writes a cached image and answers browser revalidation from the
// cache, so a repeat view never reaches the publisher.
func (p *Proxy) serve(w http.ResponseWriter, r *http.Request, image cachedImage) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", image.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == image.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", image.kind)
	w.Write(image.body)
}

func isAVIF(data []byte) bool {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return false
	}
	// Accept only the major AVIF brand; never trust the upstream MIME label.
	return string(data[8:12]) == "avif" || string(data[8:12]) == "avis"
}
