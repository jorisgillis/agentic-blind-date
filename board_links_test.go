package main

import (
	"strings"
	"testing"
)

const linkAttrs = ` target="_blank" rel="noopener noreferrer" class="underline break-all"`

func anchor(url string) string {
	return `<a href="` + url + `"` + linkAttrs + `>` + url + `</a>`
}

func TestLinkify(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"no link", "just words", "just words"},
		{"one link", "see https://example.org/a", "see " + anchor("https://example.org/a")},
		{"http works too", "http://example.org", anchor("http://example.org")},
		{"several links", "a https://x.org b https://y.org c", "a " + anchor("https://x.org") + " b " + anchor("https://y.org") + " c"},
		{"trailing punctuation stays outside", "go to https://x.org/a.", "go to " + anchor("https://x.org/a") + "."},
		{"several trailing marks", "really https://x.org/a?!", "really " + anchor("https://x.org/a") + "?!"},
		{"a closing parenthesis around a link", "(https://x.org/a)", "(" + anchor("https://x.org/a") + ")"},
		{"a balanced parenthesis stays in the link", "https://x.org/a_(b)", anchor("https://x.org/a_(b)")},
		{"a query keeps its ampersand, escaped", "https://x.org/?a=1&b=2", anchor("https://x.org/?a=1&amp;b=2")},
		{"javascript is just text", "javascript:alert(1)", "javascript:alert(1)"},
		{"a bare domain is just text", "x.org/a", "x.org/a"},
		{"a scheme alone is just text", "https:// nothing", "https:// nothing"},
		{"a scheme without a host is just text", "https:///path", "https:///path"},
		{"tags are escaped", "<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"tags next to a link are escaped", "<b>https://x.org</b>", "&lt;b&gt;" + anchor("https://x.org") + "&lt;/b&gt;"},
		{"quotes are escaped", `say "hi" & 'bye'`, "say &#34;hi&#34; &amp; &#39;bye&#39;"},
		{"newlines are kept", "one\ntwo", "one\ntwo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(linkify(tc.text)); got != tc.want {
				t.Errorf("linkify(%q)\n got  %s\n want %s", tc.text, got, tc.want)
			}
		})
	}
}

func TestFirstLink(t *testing.T) {
	for text, want := range map[string]string{
		"no link":                         "",
		"see https://x.org/a. And more":   "https://x.org/a",
		"https://one.org then http://two": "https://one.org",
		"javascript:alert(1)":             "",
	} {
		if got := firstLink(text); got != want {
			t.Errorf("firstLink(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestLinkify_IsAvailableToTheTemplates(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	var b strings.Builder
	tmpl, err := deps.handler.tmpl.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.New("probe").Parse(`{{linkify .}}`); err != nil {
		t.Fatalf("the template function should exist: %v", err)
	}
	if err := tmpl.ExecuteTemplate(&b, "probe", "https://x.org"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `rel="noopener noreferrer"`) {
		t.Errorf("got %q", b.String())
	}
}
