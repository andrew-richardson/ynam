package paypal

import (
	"regexp"
	"strings"

	"ynam/pkg/email"
	emailmime "ynam/pkg/email/mime"

	"github.com/shopspring/decimal"
)

type paypalParser struct{}

func NewParser() email.Parser { return &paypalParser{} }

func (p *paypalParser) Name() string { return "paypal" }

// Parse extracts transactions from PayPal receipt emails.
// PayPal sends HTML-only emails, so all extraction is done against the
// stripped plain-text representation of the HTML part.
func (p *paypalParser) Parse(emailBody string) ([]email.Transaction, error) {
	var txns []email.Transaction

	// PayPal emails are HTML-only. Pass raw to ExtractHTMLText so its internal
	// full QP decode (DecodeQPContent) can handle both soft breaks and hex
	// sequences cleanly. Pre-running DecodeQPBody breaks the QP structure.
	body := emailmime.ExtractHTMLText(emailBody)

	payee := extractPayPalPayee(body)
	if payee == "" {
		return txns, nil
	}

	amountStr := extractPayPalAmount(body)
	if amountStr == "" {
		return txns, nil
	}

	amount, err := decimal.NewFromString(strings.ReplaceAll(amountStr, ",", ""))
	if err != nil {
		return txns, nil
	}

	txnDate := extractPayPalDate(body)
	if txnDate == "" {
		txnDate = emailmime.HeaderDateString(emailBody)
	}

	txnID := extractTransactionID(body)
	description := extractPayPalDescription(body)

	memo := description
	if memo == "" {
		memo = payee
	}
	if txnID != "" {
		memo += " (" + txnID + ")"
	}

	txns = append(txns, email.Transaction{
		Service: "paypal",
		Payee:   payee,
		Amount:  amount,
		Date:    txnDate,
		Memo:    memo,
		Details: map[string]string{
			"transaction_id": txnID,
			"type":           "payment",
		},
	})
	return txns, nil
}

var payeePatterns = []*regexp.Regexp{
	// "Thank you for your payment to TickTick Limited"
	regexp.MustCompile(`(?i)Thank you for your (?:automatic )?payment to ([^\n]+)`),
	// "You authorized $10.46[NBSP]USD to Cloudflare Inc" (=C2=A0 decodes to  )
	regexp.MustCompile(`(?i)You authorized \$?[\d.,]+[\s\x{00A0}]+USD to ([^\n]+)`),
	// "You sent $X to Payee" or "You sent an automatic payment to Payee"
	regexp.MustCompile(`(?i)You sent (?:an automatic payment |a payment )?to ([A-Za-z][^\n]+)`),
	// "Payment to Payee"
	regexp.MustCompile(`(?i)Payment to ([A-Za-z][^\n]+)`),
}

func extractPayPalPayee(body string) string {
	for _, re := range payeePatterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			payee := strings.TrimSpace(m[1])
			// Drop trailing email addresses or punctuation that got swept in.
			if idx := strings.Index(payee, " "); idx > 0 {
				word := payee[:idx]
				if strings.Contains(word, "@") || strings.Contains(word, ".com") {
					payee = ""
				}
			}
			if payee != "" {
				return payee
			}
		}
	}
	return ""
}

var amountPatterns = []*regexp.Regexp{
	// "Payment amount\n$3.99 USD"
	regexp.MustCompile(`(?i)Payment amount\s*\n\s*\$?([\d,]+\.\d{2})`),
	// "Total amount of this transaction\n$3.99 USD"
	regexp.MustCompile(`(?i)Total amount of this transaction\s*\n\s*\$?([\d,]+\.\d{2})`),
	// "You authorized $10.46[NBSP]USD to"
	regexp.MustCompile(`(?i)You authorized \$?([\d,]+\.\d{2})[\s\x{00A0}]*USD`),
	// "Total\n$10.46 USD" (standalone label)
	regexp.MustCompile(`(?m)^\s*Total\s*\n\s*\$?([\d,]+\.\d{2})`),
	// "Total $10.46" (inline)
	regexp.MustCompile(`(?i)\bTotal\b[:\s]+\$?([\d,]+\.\d{2})`),
}

func extractPayPalAmount(body string) string {
	for _, re := range amountPatterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

var datePatterns = []*regexp.Regexp{
	// "Transaction date June 10, 2026" or "Transaction date\nJune 10, 2026"
	regexp.MustCompile(`(?i)Transaction date\s*\n?\s*([A-Za-z]+ \d{1,2}, \d{4})`),
	regexp.MustCompile(`(?i)Transaction date\s*\n?\s*([A-Za-z]{3} \d{1,2}, \d{4})`),
	// Generic "Month D, YYYY"
	regexp.MustCompile(`((?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{1,2}, \d{4})`),
}

func extractPayPalDate(body string) string {
	for _, re := range datePatterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// extractPayPalDescription looks for a subscription or item description that
// provides more context than the payee name alone.
var descriptionPatterns = []*regexp.Regexp{
	// "Payments for\nTickTick Premium Monthly Subscription"
	regexp.MustCompile(`(?i)Payments for\s*\n\s*([^\n]+)`),
	// "Here are the details about your ... payment for X."
	regexp.MustCompile(`(?i)payment for ([^.\n]{5,80})\.`),
}

func extractPayPalDescription(body string) string {
	for _, re := range descriptionPatterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			desc := strings.TrimSpace(m[1])
			if len(desc) > 5 {
				return desc
			}
		}
	}
	return ""
}

func extractTransactionID(body string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)Transaction ID\s*\n?\s*([A-Z0-9]{10,})`),
		regexp.MustCompile(`(?i)Transaction ID[:\s]+([A-Z0-9]{10,})`),
	} {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func init() { email.Register(NewParser()) }
