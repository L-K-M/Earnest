package sanitize

import "testing"

func TestDropUnusableSrcsets(t *testing.T) {
	const src = `<img src="https://e.test/a.jpg?w=800" alt="Tom &amp; Jerry" width="800" height="450"`
	for _, tc := range []struct {
		name, srcset string
		drop         bool
	}{
		{name: "widths", srcset: "https://e.test/a.jpg 6000w, https://e.test/a.jpg?resize=250,141 250w"},
		{name: "densities", srcset: "https://e.test/a.jpg, https://e.test/b.jpg 2x"},
		{name: "single candidate", srcset: "https://e.test/a.jpg 2x"},
		{name: "commas in paths", srcset: "https://e.test/w_424,c_limit/a.jpg 424w, https://e.test/w_848,c_limit/a.jpg 848w"},

		// What Earnest stored while it split srcset at every comma.
		{name: "split query", drop: true, srcset: "https://e.test/a.jpg 6000w, https://e.test/a.jpg?resize=250, https://e.test/141 250w"},
		{name: "split path", drop: true, srcset: "https://e.test/w_424, https://e.test/post/c_limit, https://e.test/post/f_auto/a.jpg 2x"},
		{name: "split density", drop: true, srcset: "https://e.test/w_424, https://e.test/post/a.jpg 1x"},
		{name: "split without descriptors", drop: true, srcset: "https://e.test/w_424, https://e.test/post/a.jpg"},
		{name: "descriptor resolved into the URL", drop: true, srcset: "https://e.test/a.png%202x"},
		{name: "width resolved into the URL", drop: true, srcset: "https://e.test/a.png%20800w"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const before, after = `<div>`, `</div><p>Text &amp; more</p>`
			input := before + src + ` srcset="` + tc.srcset + `" loading="lazy"/>` + after
			want := input
			if tc.drop {
				want = before + src + ` loading="lazy"/>` + after
			}
			if got := DropUnusableSrcsets(input); got != want {
				t.Fatalf("got  %q\nwant %q", got, want)
			}
		})
	}
}

func TestDropUnusableSrcsetsKeepsTheOnlySource(t *testing.T) {
	for _, input := range []string{
		`<p>No images</p>`,
		`<img srcset="https://e.test/w_424, https://e.test/a.jpg 424w"/>`,
		`<pre>srcset=&#34;a, b 2w&#34;</pre>`,
	} {
		if got := DropUnusableSrcsets(input); got != input {
			t.Errorf("changed %q to %q", input, got)
		}
	}
}
