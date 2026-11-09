package media

import (
	"bytes"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// MaxCandidates bounds the image URLs tried for one article thumbnail.
const MaxCandidates = 3

// minCandidateSide mirrors the thumbnail minimum: a declared side below it
// means the publisher says the image is too small to be article art.
const minCandidateSide = 64

// maxIconLinks bounds the favicon candidates returned for one page.
const maxIconLinks = 4

// trackedImagePattern marks an image URL as a tracker, emoji or avatar
// rather than article art. A non-empty host matches the URL host exactly and
// case-insensitively; a non-empty pathPart matches anywhere in the URL path.
type trackedImagePattern struct {
	host     string
	pathPart string
}

// trackedImagePatterns is the one list of known tracker, emoji and avatar
// images. Hosts and path fragments encode the same policy: these URLs are
// never article thumbnails.
var trackedImagePatterns = []trackedImagePattern{
	{host: "feeds.feedburner.com"},
	{host: "feeds.wordpress.com"},
	{host: "pixel.wp.com"},
	{host: "stats.wordpress.com"},
	{host: "www.gravatar.com"},
	{host: "gravatar.com"},
	{host: "secure.gravatar.com"},
	{pathPart: "/emoji/"},
	{pathPart: "/smilies/"},
	{pathPart: "/wp-includes/images/"},
}

// ThumbnailCandidates returns up to MaxCandidates distinct absolute http(s)
// image URLs for one article, best first. explicit is the publisher's declared
// image and comes first when it is a valid candidate. The remaining
// candidates are the <img> elements of contentHTML in document order.
// contentHTML is an already-sanitised fragment with absolute URLs; malformed
// markup is tolerated. It returns nil when nothing qualifies.
func ThumbnailCandidates(explicit string, contentHTML string) []string {
	var out []string
	seen := make(map[string]struct{})

	add := func(raw string, img *html.Node, content bool) {
		if len(out) >= MaxCandidates {
			return
		}
		raw = strings.TrimSpace(raw)
		if raw == "" || !validThumbnailURL(raw, img, content) {
			return
		}
		if _, dup := seen[raw]; dup {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}

	add(explicit, nil, false)

	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(contentHTML), ctx)
	if err != nil {
		return out
	}

	// Iterative preorder walk: the parser does not bound nesting depth, so a
	// recursive walk could exhaust the stack on hostile markup.
	stack := make([]*html.Node, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0; i-- {
		stack = append(stack, nodes[i])
	}
	for len(stack) > 0 && len(out) < MaxCandidates {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == html.ElementNode && n.Data == "img" {
			src, _ := thumbnailNodeAttr(n, "src")
			src = strings.TrimSpace(src)
			if src == "" {
				srcset, _ := thumbnailNodeAttr(n, "srcset")
				src = bestThumbnailSrcset(srcset)
			}
			add(src, n, true)
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}
	return out
}

// validThumbnailURL reports whether raw is usable as a thumbnail candidate.
// Content images also get the tracker and declared-size checks; explicit
// publisher URLs get only the URL and SVG checks, because the publisher chose
// them.
func validThumbnailURL(raw string, img *html.Node, content bool) bool {
	u, err := url.Parse(raw)
	if err != nil || !isAbsoluteWebURL(u) || pathIsSVG(u) {
		return false
	}
	if !content {
		return true
	}
	if isTrackedImageURL(u) {
		return false
	}
	return img == nil || !declaresSmallSide(img)
}

// isAbsoluteWebURL reports whether u is an http(s) URL with a host and no
// userinfo.
func isAbsoluteWebURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && u.User == nil
}

// pathIsSVG reports whether u's path ends in .svg or .svgz, ignoring case.
func pathIsSVG(u *url.URL) bool {
	path := strings.ToLower(u.Path)
	return strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".svgz")
}

// isTrackedImageURL reports whether u is a known tracker, emoji or avatar
// image. Hosts match exactly and case-insensitively; path fragments match
// anywhere in the path.
func isTrackedImageURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	for _, pattern := range trackedImagePatterns {
		if pattern.host != "" && host == pattern.host {
			return true
		}
		if pattern.pathPart != "" && strings.Contains(u.Path, pattern.pathPart) {
			return true
		}
	}
	return false
}

// declaresSmallSide reports whether n declares a numeric width or height
// below minCandidateSide. Only values that consist entirely of digits count;
// anything else (including surrounding whitespace) is ignored.
func declaresSmallSide(n *html.Node) bool {
	for _, key := range []string{"width", "height"} {
		raw, ok := thumbnailNodeAttr(n, key)
		if !ok || !allASCIIDigits(raw) {
			continue
		}
		side, err := strconv.Atoi(raw)
		if err == nil && side < minCandidateSide {
			return true
		}
	}
	return false
}

// thumbnailNodeAttr returns the value of the named attribute on n.
func thumbnailNodeAttr(n *html.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

// thumbnailSrcsetCandidate is one srcset URL plus its optional width or
// pixel-density descriptor.
type thumbnailSrcsetCandidate struct {
	url        string
	width      int
	hasWidth   bool
	density    float64
	hasDensity bool
}

// bestThumbnailSrcset picks the srcset candidate with the largest width
// descriptor, then the largest density descriptor, then the first candidate.
// Malformed descriptors are ignored.
func bestThumbnailSrcset(raw string) string {
	candidates := parseThumbnailSrcset(raw)
	if len(candidates) == 0 {
		return ""
	}

	bestWidth, bestWidthAt := 0, -1
	bestDensity, bestDensityAt := 0.0, -1
	for i, candidate := range candidates {
		if candidate.hasWidth && (bestWidthAt < 0 || candidate.width > bestWidth) {
			bestWidth, bestWidthAt = candidate.width, i
		}
		if candidate.hasDensity && (bestDensityAt < 0 || candidate.density > bestDensity) {
			bestDensity, bestDensityAt = candidate.density, i
		}
	}

	switch {
	case bestWidthAt >= 0:
		return candidates[bestWidthAt].url
	case bestDensityAt >= 0:
		return candidates[bestDensityAt].url
	default:
		return candidates[0].url
	}
}

// parseThumbnailSrcset splits a srcset attribute into candidates. Commas
// separate candidates; a comma at the end of a URL is a separator, not part
// of the URL. Descriptors that are neither widths nor densities are ignored.
func parseThumbnailSrcset(raw string) []thumbnailSrcsetCandidate {
	var candidates []thumbnailSrcsetCandidate
	i, n := 0, len(raw)
	for {
		for i < n && (isHTMLWhitespace(raw[i]) || raw[i] == ',') {
			i++
		}
		if i >= n {
			return candidates
		}

		start := i
		for i < n && !isHTMLWhitespace(raw[i]) {
			i++
		}
		src := raw[start:i]
		if src == "" {
			continue
		}
		if strings.HasSuffix(src, ",") {
			src = strings.TrimRight(src, ",")
			if src != "" {
				candidates = append(candidates, thumbnailSrcsetCandidate{url: src})
			}
			continue
		}

		var descriptors []string
		for {
			for i < n && isHTMLWhitespace(raw[i]) {
				i++
			}
			if i >= n {
				break
			}
			if raw[i] == ',' {
				i++
				break
			}
			start := i
			for i < n && !isHTMLWhitespace(raw[i]) && raw[i] != ',' {
				i++
			}
			descriptors = append(descriptors, raw[start:i])
		}

		candidate := thumbnailSrcsetCandidate{url: src}
		for _, descriptor := range descriptors {
			if width, ok := parseThumbnailWidth(descriptor); ok {
				candidate.width, candidate.hasWidth = width, true
			} else if density, ok := parseThumbnailDensity(descriptor); ok {
				candidate.density, candidate.hasDensity = density, true
			}
		}
		candidates = append(candidates, candidate)
	}
}

// isHTMLWhitespace reports whether b is HTML ASCII whitespace.
func isHTMLWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

// allASCIIDigits reports whether s is non-empty and contains only ASCII
// digits.
func allASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseThumbnailWidth parses a "123w" width descriptor.
func parseThumbnailWidth(s string) (int, bool) {
	if len(s) < 2 || (s[len(s)-1] != 'w' && s[len(s)-1] != 'W') {
		return 0, false
	}
	digits := s[:len(s)-1]
	if !allASCIIDigits(digits) {
		return 0, false
	}
	width, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return width, true
}

// parseThumbnailDensity parses a "1.5x" pixel-density descriptor.
func parseThumbnailDensity(s string) (float64, bool) {
	if len(s) < 2 || (s[len(s)-1] != 'x' && s[len(s)-1] != 'X') {
		return 0, false
	}
	number := s[:len(s)-1]
	integer, fraction, found := strings.Cut(number, ".")
	if !allASCIIDigits(integer) || (found && !allASCIIDigits(fraction)) {
		return 0, false
	}
	density, err := strconv.ParseFloat(number, 64)
	if err != nil || density <= 0 {
		return 0, false
	}
	return density, true
}

// IconLinks returns at most four favicon candidate URLs from an HTML
// document, best first, resolved against base. The first <base href>
// overrides base for this purpose. Parsing stops at the first <body> start
// tag or the end of input. It returns nil when nothing qualifies.
func IconLinks(document []byte, base *url.URL) []string {
	type iconLink struct {
		href   string
		typ    string
		tokens []string
		size   int
		url    string
	}

	var (
		baseHref string
		haveBase bool
		links    []iconLink
	)

	z := html.NewTokenizer(bytes.NewReader(document))
	stop := false
	for !stop {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, _ := z.TagName()
		switch string(name) {
		case "body":
			stop = true
		case "base":
			if haveBase {
				continue
			}
			attrs := readIconTagAttrs(z)
			if href, ok := attrs["href"]; ok {
				baseHref = strings.TrimSpace(href)
				haveBase = true
			}
		case "link":
			attrs := readIconTagAttrs(z)
			links = append(links, iconLink{
				href:   strings.TrimSpace(attrs["href"]),
				typ:    attrs["type"],
				tokens: iconRelTokens(attrs["rel"]),
				size:   parseIconSize(attrs["sizes"]),
			})
		}
	}

	effective := base
	if haveBase {
		effective = resolveIconBase(base, baseHref)
	}

	var big, apple, unknown, small []iconLink
	for _, link := range links {
		if link.href == "" || !isIconRelLink(link.tokens) || iconHasToken(link.tokens, "mask-icon") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(link.typ), "image/svg+xml") {
			continue
		}
		u := resolveIconLink(effective, link.href)
		if u == nil || pathIsSVG(u) {
			continue
		}
		link.url = u.String()
		switch {
		case iconHasToken(link.tokens, "apple-touch-icon"),
			iconHasToken(link.tokens, "apple-touch-icon-precomposed"):
			apple = append(apple, link)
		case iconHasToken(link.tokens, "icon") && link.size >= 32:
			big = append(big, link)
		case iconHasToken(link.tokens, "icon") && link.size == 0:
			unknown = append(unknown, link)
		case iconHasToken(link.tokens, "icon"):
			small = append(small, link)
		}
	}

	sort.SliceStable(big, func(i, j int) bool { return big[i].size > big[j].size })
	sort.SliceStable(small, func(i, j int) bool { return small[i].size > small[j].size })

	var out []string
	seen := make(map[string]struct{})
	emit := func(link iconLink) {
		if len(out) >= maxIconLinks {
			return
		}
		if _, dup := seen[link.url]; dup {
			return
		}
		seen[link.url] = struct{}{}
		out = append(out, link.url)
	}
	for _, link := range big {
		emit(link)
	}
	for _, link := range apple {
		emit(link)
	}
	for _, link := range unknown {
		emit(link)
	}
	for _, link := range small {
		emit(link)
	}
	return out
}

// readIconTagAttrs returns the attributes of the current tag token. Keys are
// lower-cased and values are entity-decoded by the tokenizer.
func readIconTagAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, value, more := z.TagAttr()
		if len(key) > 0 {
			// HTML keeps the first of duplicate attributes.
			if _, exists := attrs[string(key)]; !exists {
				attrs[string(key)] = string(value)
			}
		}
		if !more {
			return attrs
		}
	}
}

// iconRelTokens splits a rel attribute on ASCII whitespace and lower-cases
// each token.
func iconRelTokens(raw string) []string {
	fields := strings.Fields(raw)
	tokens := make([]string, len(fields))
	for i, field := range fields {
		tokens[i] = strings.ToLower(field)
	}
	return tokens
}

// iconHasToken reports whether tokens contains want.
func iconHasToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

// isIconRelLink reports whether the rel tokens name a favicon link.
func isIconRelLink(tokens []string) bool {
	return iconHasToken(tokens, "icon") ||
		iconHasToken(tokens, "apple-touch-icon") ||
		iconHasToken(tokens, "apple-touch-icon-precomposed")
}

// parseIconSize returns the largest declared side in a sizes attribute, or 0
// when no usable size is declared. "any" and malformed entries are ignored.
func parseIconSize(raw string) int {
	best := 0
	for _, field := range strings.Fields(raw) {
		width, height, ok := parseIconSizeField(field)
		if !ok {
			continue
		}
		if side := max(width, height); side > best {
			best = side
		}
	}
	return best
}

// parseIconSizeField parses one "WIDTHxHEIGHT" size entry. The x separator
// is case-insensitive.
func parseIconSizeField(s string) (int, int, bool) {
	width, height, found := strings.Cut(strings.ToLower(s), "x")
	if !found || !allASCIIDigits(width) || !allASCIIDigits(height) {
		return 0, 0, false
	}
	w, err := strconv.Atoi(width)
	if err != nil {
		return 0, 0, false
	}
	h, err := strconv.Atoi(height)
	if err != nil {
		return 0, 0, false
	}
	return w, h, true
}

// resolveIconBase resolves a document's <base href> against the page URL.
// An invalid or unresolvable href leaves base unchanged.
func resolveIconBase(base *url.URL, href string) *url.URL {
	ref, err := url.Parse(href)
	if err != nil {
		return base
	}
	resolved := resolveIconReference(base, ref)
	if resolved == nil || !isAbsoluteWebURL(resolved) {
		return base
	}
	return resolved
}

// resolveIconLink resolves a favicon href against base and reports whether
// the result is a usable absolute web URL.
func resolveIconLink(base *url.URL, href string) *url.URL {
	ref, err := url.Parse(href)
	if err != nil {
		return nil
	}
	resolved := resolveIconReference(base, ref)
	if !isAbsoluteWebURL(resolved) {
		return nil
	}
	return resolved
}

// resolveIconReference resolves ref against base. A relative ref with no
// base cannot be resolved and yields nil.
func resolveIconReference(base *url.URL, ref *url.URL) *url.URL {
	if ref.IsAbs() {
		return ref
	}
	if base == nil {
		return nil
	}
	return base.ResolveReference(ref)
}
