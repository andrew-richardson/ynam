package apple

import (
	"regexp"
	"strings"

	"ynam/pkg/email"
	emailmime "ynam/pkg/email/mime"
	"ynam/pkg/email/regex"

	"github.com/shopspring/decimal"
)

type appleParser struct{}

func NewParser() email.Parser { return &appleParser{} }

func (p *appleParser) Name() string { return "apple" }

// Parse extracts transactions from Apple/iTunes receipt emails.
func (p *appleParser) Parse(emailBody string) ([]email.Transaction, error) {
	var txns []email.Transaction

	lower := strings.ToLower(emailBody)
	if !strings.Contains(lower, "itunes") &&
		!strings.Contains(lower, "apple") &&
		!strings.Contains(lower, "app store") &&
		!strings.Contains(lower, "your receipt") {
		return txns, nil
	}

	// HTML emails need tag stripping before regex matching.
	body := emailBody
	if strings.Contains(lower, "content-type: text/html") ||
		strings.Contains(lower, "<html") {
		body = emailmime.ExtractHTMLText(emailBody)
	}

	itemName := extractAppleItem(body)
	if itemName == "" {
		itemName = "Apple Purchase"
	}

	totalAmount := extractAppleTotal(body)
	if totalAmount == "" {
		return txns, nil
	}

	amountStr := strings.ReplaceAll(totalAmount, ",", "")
	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return txns, nil
	}

	txnDate := extractAppleDate(body)
	if txnDate == "" {
		txnDate = emailmime.HeaderDateString(emailBody)
	}
	orderID := extractAppleOrderID(body)

	memo := itemName
	if orderID != "" {
		memo = itemName + " (" + orderID + ")"
	}

	txns = append(txns, email.Transaction{
		Service: "apple",
		Payee:   "Apple Inc.",
		Amount:  amount,
		Date:    txnDate,
		Memo:    memo,
		Details: map[string]string{
			"item":     itemName,
			"order_id": orderID,
			"type":     "purchase",
		},
	})
	return txns, nil
}

// extractAppleItem finds the purchased item/app name.
// Handles both old plain-text receipts ("Item: Name") and the modern HTML
// subscription format where the app name appears before the subscription
// period line (e.g. "Monthly", "Annual", "Renews ...").
func extractAppleItem(body string) string {
	patterns := []string{
		// Modern subscription receipt: app name is on the line immediately
		// before the subscription period/renewal line. Allow one optional blank
		// line between them (each HTML element becomes its own line after
		// tag stripping).
		`([^\n$][^\n]+)\n\n?[^\n]*(?:Monthly|Annual|Yearly|Renews|per month|per year)`,
		// Legacy plain-text receipt labels.
		`Item[:\s]+([^\n]+)`,
		`Product[:\s]+([^\n]+)`,
		`Description[:\s]+([^\n]+)`,
		`(?:App|Content)[:\s]+([^\n]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		matches := re.FindStringSubmatch(body)
		if len(matches) < 2 {
			continue
		}
		item := strings.TrimSpace(matches[1])
		item = regex.CleanText(item)
		item = strings.TrimSpace(item)
		if len(item) > 2 {
			return item
		}
	}
	return ""
}

func extractAppleTotal(body string) string {
	for _, pattern := range []string{
		`\bTotal\b[^S][:\s]*\$?([\d,.]+)`,
		`Grand Total[:\s]*\$?([\d,.]+)`,
		`Amount[:\s]*\$?([\d,.]+)`,
	} {
		re := regexp.MustCompile(`(?i)` + pattern)
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func extractAppleDate(body string) string {
	if date := regex.ExtractDate(body); date != "" {
		return date
	}
	for _, pattern := range []string{
		`([A-Za-z]+ \d{1,2}, \d{4})`,
		`(\d{1,2}\s+[A-Za-z]+\s+\d{4})`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) > 0 {
			return m[1]
		}
	}
	return ""
}

// extractAppleOrderID finds the order/receipt number.
// Uses [:\s]* (zero or more) to handle HTML-stripped content where the label
// and value are adjacent ("Order ID:MM63T5MM3S").
func extractAppleOrderID(body string) string {
	for _, pattern := range []string{
		`Order (?:ID|Number|#)[:\s]*([A-Z0-9]+)`,
		`Receipt (?:Number|ID)[:\s]*([A-Z0-9]+)`,
		`Transaction ID[:\s]*([A-Z0-9]+)`,
		`Confirmation Number[:\s]*([A-Z0-9]+)`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func init() { email.Register(NewParser()) }
