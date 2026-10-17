package main

import (
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
)

// linkRe finds the candidates for a link in a Post: http(s) URLs only.
var linkRe = regexp.MustCompile("https?://[^\\s<>\"`]+")

// trailingPunctuation stays outside a link: "see https://x.org/a." links to
// "https://x.org/a".
const trailingPunctuation = ".,;:!?'"

// closers pairs the brackets that may close a link, so "(https://x.org/a)"
// loses its ")" but "https://x.org/a_(b)" keeps it.
var closers = map[byte]byte{')': '(', ']': '[', '}': '{'}

// postLinks returns the [start, end) offsets of the links in a Post's text.
func postLinks(text string) [][2]int {
	var spans [][2]int
	for _, loc := range linkRe.FindAllStringIndex(text, -1) {
		start := loc[0]
		link := trimLink(text[start:loc[1]])
		if u, err := url.Parse(link); err != nil || u.Host == "" {
			continue // "https://" alone is not a link
		}
		spans = append(spans, [2]int{start, start + len(link)})
	}
	return spans
}

// firstLink is the first link of a Post's text, or "" when it has none — the
// target of the Big Screen's QR.
func firstLink(text string) string {
	spans := postLinks(text)
	if len(spans) == 0 {
		return ""
	}
	return text[spans[0][0]:spans[0][1]]
}

// linkify renders a Post's text as safe HTML: everything is escaped and only
// http(s) links become anchors, which open in a new tab with
// rel="noopener noreferrer". Newlines are kept for the page to style.
func linkify(text string) template.HTML {
	var b strings.Builder
	at := 0
	for _, span := range postLinks(text) {
		b.WriteString(html.EscapeString(text[at:span[0]]))
		link := html.EscapeString(text[span[0]:span[1]])
		b.WriteString(`<a href="` + link + `" target="_blank" rel="noopener noreferrer" class="underline break-all">` + link + `</a>`)
		at = span[1]
	}
	b.WriteString(html.EscapeString(text[at:]))
	return template.HTML(b.String()) //nolint:gosec // G203: every segment is html.EscapeString'd; only the constant anchor markup is raw
}

// trimLink cuts the punctuation and the closing brackets that are not part of
// the link off its end. It never trims into the scheme: "https://" ends in a
// slash.
func trimLink(link string) string {
	for {
		last := link[len(link)-1]
		open, isCloser := closers[last]
		switch {
		case strings.IndexByte(trailingPunctuation, last) >= 0:
		case isCloser && strings.Count(link, string(last)) > strings.Count(link, string(open)):
		default:
			return link
		}
		link = link[:len(link)-1]
	}
}
