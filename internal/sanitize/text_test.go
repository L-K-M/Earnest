package sanitize

import "testing"

func TestText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "paragraph break",
			input: "<p>Hello world.</p><p>Second paragraph.</p>",
			want:  "Hello world.\n\nSecond paragraph.",
		},
		{
			name:  "line break",
			input: "<p>Line one<br>Line two</p>",
			want:  "Line one\nLine two",
		},
		{
			name:  "headings are paragraphs",
			input: "<h1>Title</h1><p>Body</p>",
			want:  "Title\n\nBody",
		},
		{
			name:  "list items are paragraphs",
			input: "<ul><li>one</li><li>two</li></ul>",
			want:  "one\n\ntwo",
		},
		{
			name:  "inline elements join",
			input: "<p>a <b>b</b> c</p>",
			want:  "a b c",
		},
		{
			name:  "whitespace collapses",
			input: "<p>  a \n\t b  </p>",
			want:  "a b",
		},
		{
			name:  "script text ignored",
			input: "<script>alert(1)</script><p>x</p>",
			want:  "x",
		},
		{
			name:  "style text ignored",
			input: "<style>body { display: none }</style><p>x</p>",
			want:  "x",
		},
		{
			name:  "table cells are paragraphs",
			input: "<table><tr><td>a</td><td>b</td></tr></table>",
			want:  "a\n\nb",
		},
		{
			name:  "blockquote",
			input: "<blockquote>quote</blockquote>",
			want:  "quote",
		},
		{
			name:  "details and summary",
			input: "<details><summary>More</summary><p>Hidden</p></details>",
			want:  "More\n\nHidden",
		},
		{
			name:  "definition list",
			input: "<dl><dt>term</dt><dd>definition</dd></dl>",
			want:  "term\n\ndefinition",
		},
		{
			name:  "preformatted whitespace collapses",
			input: "<pre>  code\n  more</pre>",
			want:  "code more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.input); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
