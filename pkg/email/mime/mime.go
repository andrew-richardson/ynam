// Package mime provides shared helpers for parsing raw RFC 2822 email
// messages: quoted-printable decoding, HTML-to-text conversion, and header
// extraction. All functions are safe for concurrent use.
package mime

import (
	"io"
	stdmime "mime/quotedprintable"
	"regexp"
	"strings"
	"time"
)

var (
	reQPSoftBreak = regexp.MustCompile(`=\r?\n`)
	reStyleBlock  = regexp.MustCompile(`(?si)<style[^>]*>.*?</style>`)
	reHTMLComment = regexp.MustCompile(`<!--.*?-->`)
	reHTMLTag     = regexp.MustCompile(`<[^>]+>`)
	reMultiSpace  = regexp.MustCompile(`[ \t]{2,}`)
	reBlankLines  = regexp.MustCompile(`\n{3,}`)

	// rfc2822Date matches "Date: Fri, 26 Jun 2026 18:06:23 +0000"
	rfc2822Date = regexp.MustCompile(`(?m)^Date:\s+\w+,\s+(\d{1,2}\s+\w+\s+\d{4})`)

	// dateLayouts tried in order by HeaderDate.
	dateLayouts = []string{
		"2 Jan 2006",
		"2 January 2006",
	}
)

// DecodeQPContent fully decodes a quoted-printable encoded string, handling
// both soft line breaks (=\n) and hex sequences (=E2=9C=88 → ✈).
func DecodeQPContent(s string) string {
	b, err := io.ReadAll(stdmime.NewReader(strings.NewReader(s)))
	if err != nil {
		return s
	}
	return string(b)
}

// DecodeQPBody decodes quoted-printable soft line breaks (=\n) in the MIME
// body of a raw RFC 2822 message while leaving the headers untouched.
// Applying QP decoding to headers corrupts them because a body line that ends
// with '=' can be joined into the following header line.
func DecodeQPBody(raw string) string {
	boundary := headerBodyBoundary(raw)
	if boundary < 0 {
		return raw
	}
	return raw[:boundary] + reQPSoftBreak.ReplaceAllString(raw[boundary:], "")
}

// ExtractHTMLText locates the text/html MIME part in a raw RFC 2822 message,
// decodes quoted-printable soft breaks, strips CSS style blocks and HTML tags,
// and returns clean plain text suitable for regex-based field extraction.
func ExtractHTMLText(raw string) string {
	body := extractHTMLPart(raw)
	if body == "" {
		// Fallback: treat the entire payload as HTML (e.g. when only an HTML
		// fragment is passed directly in tests).
		body = raw
	}

	// Fully decode QP (soft breaks + hex sequences like =0A, =C2=A0, =E2=9C=88).
	s := DecodeQPContent(body)
	s = reStyleBlock.ReplaceAllString(s, "")
	s = reHTMLComment.ReplaceAllString(s, "")
	s = reHTMLTag.ReplaceAllString(s, "\n")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return s
}

// HeaderDate parses the RFC 2822 Date header from a raw message and returns
// the result as a time.Time. Returns the zero Time and false when the header
// is absent or its value cannot be parsed.
func HeaderDate(raw string) (time.Time, bool) {
	m := rfc2822Date.FindStringSubmatch(raw)
	if len(m) < 2 {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, m[1]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// HeaderDateString returns the RFC 2822 Date header formatted as "2006-01-02",
// or "" if the header is absent or unparseable.
func HeaderDateString(raw string) string {
	if t, ok := HeaderDate(raw); ok {
		return t.Format("2006-01-02")
	}
	return ""
}

// headerBodyBoundary returns the index of the first byte of the body section
// (the character after the blank line that separates headers from body).
// Returns -1 if no boundary is found.
func headerBodyBoundary(raw string) int {
	if i := strings.Index(raw, "\r\n\r\n"); i >= 0 {
		return i + 4
	}
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		return i + 2
	}
	return -1
}

// extractHTMLPart locates the text/html MIME part in a raw RFC 2822 message
// and returns its raw content (still QP-encoded). Returns "" if not found.
// Handles both LF and CRLF line endings.
func extractHTMLPart(raw string) string {
	lower := strings.ToLower(raw)
	idx := strings.Index(lower, "content-type: text/html")
	if idx < 0 {
		return ""
	}
	rest := raw[idx:]

	// Find end of this part's headers.
	partBodyStart := -1
	if i := strings.Index(rest, "\r\n\r\n"); i >= 0 {
		partBodyStart = i + 4
	} else if i := strings.Index(rest, "\n\n"); i >= 0 {
		partBodyStart = i + 2
	}
	if partBodyStart < 0 {
		return ""
	}
	content := rest[partBodyStart:]

	// Trim at the next MIME boundary.
	if end := strings.Index(content, "\r\n--"); end >= 0 {
		content = content[:end]
	} else if end := strings.Index(content, "\n--"); end >= 0 {
		content = content[:end]
	}
	return content
}
