package sanitize

// The stored subset is defined by three sets:
//
//   - allowedElements: copied into the output, with the attributes listed in
//     elementAttrs plus the global dir and lang.
//   - dropElements: removed together with their content. These execute code,
//     load active resources, are raw-text containers, or are document
//     metadata that has no place in stored article content.
//   - everything else: unwrapped, keeping its sanitised children. Unknown
//     elements carry no meaning Earnest's clients rely on, but their text
//     does.
//
// Event handlers, style, class and id are absent from elementAttrs on
// purpose. Handlers run script; style can load remote resources and restyle
// the reader itself; class and id would let stored content match the
// client's own selectors, collide with its DOM, or smuggle readability's
// class-based heuristics back in. None of them are needed to read an
// article.

// allowedElements is the element allowlist.
var allowedElements = stringSet(
	// Block structure.
	"p", "div", "br", "hr",
	"h1", "h2", "h3", "h4", "h5", "h6",
	"ul", "ol", "li",
	"dl", "dt", "dd",
	"blockquote", "pre",
	"figure", "figcaption",
	"table", "caption", "thead", "tbody", "tfoot", "tr", "td", "th",
	"colgroup", "col",
	// details and summary are kept: they are inert without script, give
	// FAQ and long-article structure, and the renderers support them
	// natively. open is kept so a source can start expanded; ontoggle and
	// every other handler are stripped with all event attributes.
	"details", "summary",
	"article", "section", "main",
	// Inline phrasing.
	"a", "em", "strong", "code", "sup", "sub", "abbr", "time", "span",
	"b", "i", "u", "s", "small", "mark", "q", "cite", "del", "ins",
	"kbd", "samp", "var",
	"img",
)

// dropElements are removed with their subtree.
var dropElements = stringSet(
	// Script, style, raw-text and foreign-content containers. Their content
	// is code, styling or markup that a later parse could reinterpret.
	"script", "style", "template", "noscript",
	"svg", "math",
	"iframe", "object", "embed", "applet",
	"frame", "frameset", "noframes", "noembed",
	"fencedframe", "portal",
	// Document metadata and resource hints.
	"base", "meta", "link", "title", "basefont", "bgsound",
	// Form controls and media elements that load or submit resources.
	"input", "button", "select", "textarea", "option", "optgroup",
	"datalist", "keygen", "param", "source", "track",
)

// elementAttrs lists the attributes kept per element, beyond the global dir
// and lang. URL-bearing attributes are validated by cleanAttr.
var elementAttrs = map[string][]string{
	"a":        {"href"},
	"img":      {"src", "alt", "width", "height", "srcset"},
	"abbr":     {"title"},
	"time":     {"datetime"},
	"del":      {"datetime"},
	"ins":      {"datetime"},
	"details":  {"open"},
	"td":       {"colspan", "rowspan"},
	"th":       {"colspan", "rowspan", "scope"},
	"ol":       {"start", "reversed"},
	"li":       {"value"},
	"col":      {"span"},
	"colgroup": {"span"},
}

// blockElements get a paragraph break on each side in Text.
var blockElements = stringSet(
	"p", "div", "hr",
	"h1", "h2", "h3", "h4", "h5", "h6",
	"ul", "ol", "li",
	"dl", "dt", "dd",
	"blockquote", "pre",
	"figure", "figcaption",
	"table", "caption", "thead", "tbody", "tfoot", "tr", "td", "th",
	"details", "summary",
	"article", "section", "main",
)

// invisibleElements are skipped by Text even though Sanitize removes them.
var invisibleElements = stringSet(
	"script", "style", "template", "noscript", "title",
)

func stringSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}
