// Package imageproxy signs image URLs and fetches them through checked egress.
package imageproxy

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/sanitize"
	"golang.org/x/net/html"
)

const maxImageSize = 10 << 20
const imageTimeout = 30 * time.Second

// Mode controls whether images load automatically. Use ClickToLoad for
// agent-selected entries unless their bytes were cached at ingest: otherwise
// a publisher can observe which entry an agent selected.
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
}

// New creates a proxy using a deployment secret and the public origin at
// which /img/ is served. The secret must have at least 32 bytes of entropy.
func New(secret []byte, client *outbound.Client, origin string) (*Proxy, error) {
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
	return &Proxy{key: key, client: client, base: u}, nil
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
	rewritten, err := sanitize.RewriteImages(input, p.URL)
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
	result, err := p.client.Get(r.Context(), string(raw), outbound.Policy{Purpose: outbound.Image, MaxBodySize: maxImageSize, Timeout: imageTimeout, ExtraPorts: p.extraPorts})
	if err != nil || result == nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode != http.StatusOK {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	data, err := io.ReadAll(result.Response.Body)
	if err != nil {
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
		http.Error(w, "unsupported image", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if etag := result.Response.Header.Get("ETag"); etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Write(data)
}

func isAVIF(data []byte) bool {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return false
	}
	// Accept only the major AVIF brand; never trust the upstream MIME label.
	return string(data[8:12]) == "avif" || string(data[8:12]) == "avis"
}
