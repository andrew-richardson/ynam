package mime

import (
	"strings"
	"testing"
)

func TestDecodeQPContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "soft break joined",
			input: "hello =\nworld",
			want:  "hello world",
		},
		{
			name:  "hex sequence",
			input: "=C2=A0",
			want:  " ",
		},
		{
			name:  "newline hex",
			input: "line1=0Aline2",
			want:  "line1\nline2",
		},
		{
			name:  "no encoding",
			input: "plain text",
			want:  "plain text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeQPContent(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeQPBody(t *testing.T) {
	t.Run("CRLF boundary", func(t *testing.T) {
		raw := "From: test@example.com\r\nSubject: Hi\r\n\r\nhello =\r\nworld"
		got := DecodeQPBody(raw)
		if !strings.Contains(got, "hello world") {
			t.Errorf("expected joined body, got %q", got)
		}
		if !strings.Contains(got, "From: test@example.com") {
			t.Errorf("headers should be preserved, got %q", got)
		}
	})

	t.Run("LF boundary", func(t *testing.T) {
		raw := "Subject: Test\n\nhello =\nworld"
		got := DecodeQPBody(raw)
		if !strings.Contains(got, "hello world") {
			t.Errorf("expected joined body, got %q", got)
		}
	})

	t.Run("no boundary returns raw", func(t *testing.T) {
		raw := "no boundary here"
		got := DecodeQPBody(raw)
		if got != raw {
			t.Errorf("expected raw unchanged, got %q", got)
		}
	})

	t.Run("headers not decoded", func(t *testing.T) {
		// A '=' at end of a header line should NOT be joined with the body
		raw := "Subject: test=\r\n\r\nbody"
		got := DecodeQPBody(raw)
		if !strings.Contains(got, "Subject: test=") {
			t.Errorf("header '=' should not be removed, got %q", got)
		}
	})
}

func TestHeaderBodyBoundary(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"CRLF", "A: B\r\n\r\nbody", 8},
		{"LF", "A: B\n\nbody", 6},
		{"none", "no boundary", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := headerBodyBoundary(tt.input)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHeaderDate(t *testing.T) {
	t.Run("valid RFC2822 date", func(t *testing.T) {
		raw := "Date: Fri, 26 Jun 2026 18:06:23 +0000\r\n\r\nbody"
		tm, ok := HeaderDate(raw)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if tm.Year() != 2026 || tm.Month().String() != "June" || tm.Day() != 26 {
			t.Errorf("unexpected date: %v", tm)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		_, ok := HeaderDate("Subject: test\n\nbody")
		if ok {
			t.Error("expected ok=false for missing Date header")
		}
	})
}

func TestHeaderDateString(t *testing.T) {
	raw := "Date: Mon, 1 Jan 2024 00:00:00 +0000\n\n"
	got := HeaderDateString(raw)
	if got != "2024-01-01" {
		t.Errorf("got %q, want %q", got, "2024-01-01")
	}

	got = HeaderDateString("no date")
	if got != "" {
		t.Errorf("expected empty string for missing date, got %q", got)
	}
}

func TestExtractHTMLText(t *testing.T) {
	t.Run("strips HTML tags", func(t *testing.T) {
		input := "<html><body><p>Hello World</p></body></html>"
		got := ExtractHTMLText(input)
		if !strings.Contains(got, "Hello World") {
			t.Errorf("expected 'Hello World' in output, got %q", got)
		}
		if strings.Contains(got, "<p>") {
			t.Errorf("expected tags stripped, got %q", got)
		}
	})

	t.Run("strips style blocks", func(t *testing.T) {
		input := "<style>body { color: red; }</style><p>Text</p>"
		got := ExtractHTMLText(input)
		if strings.Contains(got, "color: red") {
			t.Errorf("style block should be stripped, got %q", got)
		}
		if !strings.Contains(got, "Text") {
			t.Errorf("expected 'Text' in output, got %q", got)
		}
	})

	t.Run("QP hex decoded", func(t *testing.T) {
		// =C2=A0 is a non-breaking space; should be decoded not left literal
		input := "Amount=C2=A0USD"
		got := ExtractHTMLText(input)
		if strings.Contains(got, "=C2=A0") {
			t.Errorf("QP hex should be decoded, got %q", got)
		}
		if !strings.Contains(got, " ") {
			t.Errorf("expected non-breaking space, got %q", got)
		}
	})

	t.Run("extracts HTML part from multipart", func(t *testing.T) {
		raw := "MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=bound\r\n\r\n" +
			"--bound\r\nContent-Type: text/plain\r\n\r\nplain text\r\n" +
			"--bound\r\nContent-Type: text/html\r\n\r\n<p>HTML content</p>\r\n--bound--"
		got := ExtractHTMLText(raw)
		if !strings.Contains(got, "HTML content") {
			t.Errorf("expected HTML part content, got %q", got)
		}
	})
}
