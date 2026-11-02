package sanitize

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// ImageURLs returns the image URLs in sanitised HTML in document order:
// each img's src followed by its srcset candidates, without duplicates. It
// does not validate or resolve the URLs.
func ImageURLs(htmlStr string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var urls []string
	seen := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			if src, ok := attributeValue(n, "src"); ok {
				appendUnique(&urls, seen, src)
			}
			if srcset, ok := attributeValue(n, "srcset"); ok {
				for _, candidate := range srcsetURLs(srcset) {
					appendUnique(&urls, seen, candidate)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return urls, nil
}

func appendUnique(urls *[]string, seen map[string]struct{}, value string) {
	if value == "" {
		return
	}
	if _, dup := seen[value]; dup {
		return
	}
	seen[value] = struct{}{}
	*urls = append(*urls, value)
}

// RewriteImages returns html with every image URL replaced by rewrite.
// rewrite receives the parsed source URL and returns the replacement; a nil
// URL drops the image, or that srcset candidate. Rewritten URLs must use
// http or https.
//
// The input must already be sanitised, and is treated as a fragment: the
// output is a fragment too. RewriteImages changes URLs only and does not
// re-run Sanitize. It is meant for serve time, where the image proxy
// replaces each source with a signed proxy URL.
func RewriteImages(htmlStr string, rewrite func(*url.URL) (*url.URL, error)) (string, error) {
	if rewrite == nil {
		return "", errors.New("sanitize: nil image rewrite function")
	}

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}
	body := findElement(doc, "body")
	if body == nil {
		return "", nil
	}

	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "img" {
			if err := rewriteImg(n, rewrite); err != nil {
				return err
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(body); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&buf, child); err != nil {
			return "", fmt.Errorf("render html: %w", err)
		}
	}
	return buf.String(), nil
}

// rewriteImg applies rewrite to one img element, in place. An img whose src
// is dropped disappears; an img with neither src nor srcset is removed.
func rewriteImg(img *html.Node, rewrite func(*url.URL) (*url.URL, error)) error {
	hasSource := false

	if src, ok := attributeValue(img, "src"); ok {
		replacement, err := rewriteSource(rewrite, src)
		if err != nil {
			return err
		}
		if replacement == nil {
			removeAttribute(img, "src")
		} else {
			setAttribute(img, "src", replacement.String())
			hasSource = true
		}
	}

	if srcset, ok := attributeValue(img, "srcset"); ok {
		candidates, _ := parseSrcset(srcset)
		var out []string
		for _, candidate := range candidates {
			replacement, err := rewriteSource(rewrite, candidate.url)
			if err != nil {
				return err
			}
			if replacement == nil {
				continue
			}
			s := replacement.String()
			if candidate.descriptor != "" {
				s += " " + candidate.descriptor
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			removeAttribute(img, "srcset")
		} else {
			setAttribute(img, "srcset", strings.Join(out, ", "))
			hasSource = true
		}
	}

	if !hasSource && img.Parent != nil {
		img.Parent.RemoveChild(img)
	}
	return nil
}

// rewriteSource parses one stored URL and applies rewrite, enforcing the
// same scheme policy as Sanitize.
func rewriteSource(rewrite func(*url.URL) (*url.URL, error), src string) (*url.URL, error) {
	u, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse image url %q: %w", src, err)
	}
	replacement, err := rewrite(u)
	if err != nil {
		return nil, fmt.Errorf("rewrite image url %q: %w", src, err)
	}
	if replacement != nil && replacement.Scheme != "http" && replacement.Scheme != "https" {
		return nil, fmt.Errorf("rewrite image url %q: scheme %q is not http or https", src, replacement.Scheme)
	}
	return replacement, nil
}

func setAttribute(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func removeAttribute(n *html.Node, key string) {
	n.Attr = slices.DeleteFunc(n.Attr, func(attr html.Attribute) bool {
		return attr.Key == key
	})
}
