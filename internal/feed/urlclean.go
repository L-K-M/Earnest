package feed

import (
	"net/url"
	"strconv"
	"strings"
)

// ResolveURL resolves ref against base following RFC 3986, which handles
// protocol-relative references, dot segments and query-only references. An
// absolute ref wins. A relative ref is returned unchanged when base is not
// absolute, because there is nothing to anchor it to.
func ResolveURL(base, ref string) string {
	base = strings.TrimSpace(base)
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return base
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if !b.IsAbs() {
		return ref
	}
	return b.ResolveReference(r).String()
}

// absURL resolves ref against base and canonicalises the result. When the
// value cannot be made into an absolute http(s) URL the resolved value is
// kept, so the caller still sees what the publisher wrote.
func absURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	resolved := ResolveURL(base, ref)
	if cleaned := CleanURL(resolved); cleaned != "" {
		return cleaned
	}
	return resolved
}

// CleanURL canonicalises an absolute http(s) URL and strips tracking
// parameters. It returns "" for anything else: a canonical URL must be usable
// as an identity component and a link a browser can open.
//
// Canonical form: lower-case scheme and host, no default port, no userinfo,
// no fragment, dot segments removed, tracking parameters dropped and the
// remaining query parameters sorted. The path keeps its case and trailing
// slash; percent-encoding is left as written.
func CleanURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()
	if p, convErr := strconv.Atoi(port); convErr == nil {
		if (scheme == "http" && p == 80) || (scheme == "https" && p == 443) {
			port = ""
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	u.Scheme = scheme
	u.User = nil
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = removeDotSegments(u.Path)
	u.RawPath = ""
	u.RawQuery = cleanQuery(u.RawQuery)
	return u.String()
}

// removeDotSegments applies the RFC 3986 path normalisation algorithm to a
// decoded path. Double slashes and trailing slashes are preserved.
func removeDotSegments(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		switch seg {
		case ".":
			if i == len(segments)-1 {
				out = append(out, "")
			}
		case "..":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
			if i == len(segments)-1 {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}
	res := strings.Join(out, "/")
	if res == "" && strings.HasPrefix(path, "/") {
		return "/"
	}
	return res
}

// cleanQuery drops tracking parameters and re-encodes what remains in sorted
// order, so two spellings of one URL canonicalise identically.
func cleanQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	kept := make(url.Values, len(values))
	for name, vals := range values {
		if IsTrackingParam(name) {
			continue
		}
		kept[name] = vals
	}
	if len(kept) == 0 {
		return ""
	}
	return kept.Encode()
}

// trackingParamFamilies are parameter prefixes that always carry campaign or
// click identifiers.
var trackingParamFamilies = []string{
	"utm_",
	"mtm_",
	"pk_",
	"piwik_",
	"matomo_",
	"sc_",
	"hmb_",
	"elq",
	"trk",
}

// trackingParams are well-known click and campaign identifiers from ad
// networks, social platforms and marketing suites. Matching is
// case-insensitive and ignores surrounding whitespace.
var trackingParams = map[string]bool{
	"fbclid":            true,
	"gclid":             true,
	"dclid":             true,
	"gclsrc":            true,
	"mc_cid":            true,
	"mc_eid":            true,
	"igshid":            true,
	"yclid":             true,
	"_openstat":         true,
	"_hsenc":            true,
	"_hsmi":             true,
	"hsctatracking":     true,
	"vero_id":           true,
	"vero_conv":         true,
	"wt_zmc":            true,
	"wt_mc":             true,
	"cmpid":             true,
	"si":                true,
	"spm":               true,
	"ref_src":           true,
	"ref_url":           true,
	"fb_action_ids":     true,
	"fb_action_types":   true,
	"fb_ref":            true,
	"fb_source":         true,
	"action_object_map": true,
	"action_type_map":   true,
	"action_ref_map":    true,
	"gs_l":              true,
	"elqtrackid":        true,
	"elqtrack":          true,
}

// IsTrackingParam reports whether a query parameter name is a known tracking
// parameter.
func IsTrackingParam(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, family := range trackingParamFamilies {
		if strings.HasPrefix(name, family) {
			return true
		}
	}
	return trackingParams[name]
}
