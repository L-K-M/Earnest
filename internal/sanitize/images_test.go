package sanitize

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestImageURLs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "no images",
			input: "<p>text</p>",
			want:  nil,
		},
		{
			name:  "src only",
			input: `<img src="https://e.example/a.png">`,
			want:  []string{"https://e.example/a.png"},
		},
		{
			name:  "src and srcset deduplicated",
			input: `<img src="https://e.example/a.png" srcset="https://e.example/a.png 1x, https://e.example/b.png 2x">`,
			want:  []string{"https://e.example/a.png", "https://e.example/b.png"},
		},
		{
			name:  "srcset only",
			input: `<img srcset="https://e.example/a.png 1x, https://e.example/b.png 2x">`,
			want:  []string{"https://e.example/a.png", "https://e.example/b.png"},
		},
		{
			name:  "two images in order",
			input: `<img src="https://e.example/a.png"><p>x</p><img src="https://e.example/b.png">`,
			want:  []string{"https://e.example/a.png", "https://e.example/b.png"},
		},
		{
			name:  "duplicate sources listed once",
			input: `<img src="https://e.example/a.png"><img src="https://e.example/a.png">`,
			want:  []string{"https://e.example/a.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImageURLs(tt.input)
			if err != nil {
				t.Fatalf("ImageURLs: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ImageURLs(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRewriteImages(t *testing.T) {
	proxy := func(u *url.URL) (*url.URL, error) {
		proxyURL, err := url.Parse("https://proxy.example/i?u=" + url.QueryEscape(u.String()))
		if err != nil {
			return nil, err
		}
		return proxyURL, nil
	}

	t.Run("src rewritten", func(t *testing.T) {
		input := `<img src="https://e.example/a.png" alt="a" loading="lazy" referrerpolicy="no-referrer"/>`
		got, err := RewriteImages(input, proxy)
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		want := `<img src="https://proxy.example/i?u=https%3A%2F%2Fe.example%2Fa.png" alt="a" loading="lazy" referrerpolicy="no-referrer"/>`
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("srcset candidates rewritten", func(t *testing.T) {
		input := `<img src="https://e.example/a.png" srcset="https://e.example/a.png 1x, https://e.example/b.png 2x" loading="lazy" referrerpolicy="no-referrer"/>`
		got, err := RewriteImages(input, proxy)
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		for _, u := range []string{"https://e.example/a.png", "https://e.example/b.png"} {
			if !strings.Contains(got, "u="+url.QueryEscape(u)) {
				t.Errorf("rewritten srcset %q does not contain %q", got, u)
			}
		}
		if !strings.Contains(got, "1x") || !strings.Contains(got, "2x") {
			t.Errorf("descriptors lost: %q", got)
		}
	})

	t.Run("nil replacement drops image", func(t *testing.T) {
		input := `<p>before</p><img src="https://e.example/a.png"><p>after</p>`
		got, err := RewriteImages(input, func(*url.URL) (*url.URL, error) { return nil, nil })
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		want := `<p>before</p><p>after</p>`
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("nil srcset candidate dropped", func(t *testing.T) {
		input := `<img src="https://e.example/a.png" srcset="https://e.example/a.png 1x, https://e.example/b.png 2x" loading="lazy" referrerpolicy="no-referrer"/>`
		got, err := RewriteImages(input, func(u *url.URL) (*url.URL, error) {
			if strings.Contains(u.String(), "b.png") {
				return nil, nil
			}
			return proxy(u)
		})
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		if strings.Contains(got, "b.png") {
			t.Errorf("dropped candidate survived: %q", got)
		}
		if !strings.Contains(got, "1x") {
			t.Errorf("kept candidate lost: %q", got)
		}
	})

	t.Run("all candidates dropped removes srcset", func(t *testing.T) {
		input := `<img src="https://e.example/a.png" srcset="https://e.example/b.png 1x" loading="lazy" referrerpolicy="no-referrer"/>`
		got, err := RewriteImages(input, func(u *url.URL) (*url.URL, error) {
			if strings.Contains(u.String(), "b.png") {
				return nil, nil
			}
			return proxy(u)
		})
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		if strings.Contains(got, "srcset") {
			t.Errorf("empty srcset survived: %q", got)
		}
	})

	t.Run("srcset only image dropped when all candidates nil", func(t *testing.T) {
		input := `<img srcset="https://e.example/b.png 1x" loading="lazy" referrerpolicy="no-referrer"/>`
		got, err := RewriteImages(input, func(*url.URL) (*url.URL, error) { return nil, nil })
		if err != nil {
			t.Fatalf("RewriteImages: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("rewrite error propagated", func(t *testing.T) {
		wantErr := "boom"
		_, err := RewriteImages(`<img src="https://e.example/a.png">`, func(*url.URL) (*url.URL, error) {
			return nil, errTest(wantErr)
		})
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("err = %v, want it to contain %q", err, wantErr)
		}
	})

	t.Run("non http replacement rejected", func(t *testing.T) {
		_, err := RewriteImages(`<img src="https://e.example/a.png">`, func(*url.URL) (*url.URL, error) {
			return &url.URL{Scheme: "data", Opaque: "text/html,<script>alert(1)</script>"}, nil
		})
		if err == nil {
			t.Fatal("RewriteImages accepted a data: replacement")
		}
	})

	t.Run("nil rewrite function rejected", func(t *testing.T) {
		if _, err := RewriteImages(`<img src="https://e.example/a.png">`, nil); err == nil {
			t.Fatal("RewriteImages accepted a nil rewrite function")
		}
	})
}

type errTest string

func (e errTest) Error() string { return string(e) }
