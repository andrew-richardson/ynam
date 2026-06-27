package audible

import (
	"regexp"
	"strings"

	"ynam/pkg/email"
	emailmime "ynam/pkg/email/mime"

	"github.com/shopspring/decimal"
)

type audibleParser struct{}

func NewParser() email.Parser { return &audibleParser{} }

func (p *audibleParser) Name() string { return "audible" }

// Parse extracts transactions from Audible order confirmation emails.
// Handles two formats:
//  1. Paid purchases: "Total paid: $16.18" with "Order number: D01-..."
//  2. Pre-orders: "$7.47 + applicable tax will be charged..."
//
// Credit-only orders (Total paid: $0.00) are skipped — no charge occurred.
func (p *audibleParser) Parse(emailBody string) ([]email.Transaction, error) {
	var txns []email.Transaction

	lower := strings.ToLower(emailBody)
	if !strings.Contains(lower, "audible") {
		return txns, nil
	}

	body := emailmime.ExtractHTMLText(emailmime.DecodeQPBody(emailBody))

	// Pre-order emails: amount charged on release date, no "Total paid" line.
	if strings.Contains(lower, "pre-order") || strings.Contains(lower, "pre order") {
		if txn, ok := parsePreOrder(emailBody, body); ok {
			txns = append(txns, txn)
		}
		return txns, nil
	}

	// Standard order confirmation.
	totalStr := extractAudibleTotal(body)
	if totalStr == "" {
		return txns, nil
	}
	amount, err := decimal.NewFromString(strings.ReplaceAll(totalStr, ",", ""))
	if err != nil || amount.IsZero() {
		return txns, nil // skip credit-only ($0.00) orders
	}

	orderNum := extractAudibleOrderNum(body)
	title := extractAudibleTitle(body)
	date := extractAudibleDate(body, emailBody)

	memo := title
	if memo == "" {
		memo = "Audible Purchase"
	}

	txns = append(txns, email.Transaction{
		Service: "audible",
		Payee:   "Audible",
		Amount:  amount,
		Date:    date,
		Memo:    memo,
		Details: map[string]string{
			"order_number": orderNum,
			"title":        title,
		},
	})
	return txns, nil
}

func parsePreOrder(raw, body string) (email.Transaction, bool) {
	// "$7.47 + applicable tax will be charged to your credit card when..."
	re := regexp.MustCompile(`\$([\d.]+)\s*\+\s*applicable tax will be charged`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return email.Transaction{}, false
	}
	amount, err := decimal.NewFromString(m[1])
	if err != nil || amount.IsZero() {
		return email.Transaction{}, false
	}
	title := extractAudibleTitle(body)
	date := extractAudibleDate(body, raw)
	orderNum := extractAudibleOrderNum(body)
	memo := title
	if memo == "" {
		memo = "Audible Pre-order"
	}
	return email.Transaction{
		Service: "audible",
		Payee:   "Audible",
		Amount:  amount,
		Date:    date,
		Memo:    memo,
		Details: map[string]string{
			"order_number": orderNum,
			"title":        title,
			"type":         "preorder",
		},
	}, true
}

// extractAudibleTotal finds the "Total paid:\n$16.18" value.
// Audible HTML emails render each cell on its own line after tag stripping.
func extractAudibleTotal(body string) string {
	// After HTML tag stripping, label and amount may be separated by blank lines
	// and &nbsp; entity lines. Use [\s\S]*? to skip over them.
	for _, pat := range []string{
		`(?i)Total paid:[\s\S]{0,60}?\$?([\d,]+\.\d{2})`,
		`(?i)Total[:\s]+\$?([\d,]+\.\d{2})`,
	} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// extractAudibleOrderNum finds "Order number: D01-0196618-3945841".
func extractAudibleOrderNum(body string) string {
	if m := regexp.MustCompile(`(?i)Order\s+number[:\s]+([A-Z0-9\-]{10,})`).FindStringSubmatch(body); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractAudibleTitle finds the audiobook/product title.
// Audible HTML emails render as "Title ... Amount ... <title> ... $price ... Summary"
// with many blank/entity lines between each element after tag stripping.
// We collapse whitespace-only lines and &nbsp; lines, then match.
func extractAudibleTitle(body string) string {
	// Collapse runs of blank / whitespace-only / &nbsp;-only lines into a single newline.
	junkLine := regexp.MustCompile(`(?m)^[\s&;a-z0-9]*$`)
	clean := junkLine.ReplaceAllString(body, "")
	clean = regexp.MustCompile(`\n{2,}`).ReplaceAllString(clean, "\n")

	// Now the structure is: "Title\nAmount\n<title>\n$price\nSummary"
	re := regexp.MustCompile(`(?i)Amount\n([^\n$][^\n]{2,})\n\$`)
	if m := re.FindStringSubmatch(clean); len(m) > 1 {
		t := strings.TrimSpace(m[1])
		if t != "" {
			return t
		}
	}
	// Fallback: line before "Summary".
	re2 := regexp.MustCompile(`(?m)^([^\n$]{5,})\nSummary`)
	if m := re2.FindStringSubmatch(clean); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractAudibleDate extracts the date from the HTML-stripped body or email header.
func extractAudibleDate(body, raw string) string {
	// Try date patterns in body text.
	for _, pat := range []string{
		`([A-Za-z]+ \d{1,2}, \d{4})`,
		`(\d{4}-\d{2}-\d{2})`,
	} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return emailmime.HeaderDateString(raw)
}

func init() { email.Register(NewParser()) }
