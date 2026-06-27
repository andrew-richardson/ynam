package venmo

import (
	"regexp"
	"strings"

	"ynam/pkg/email"
	emailmime "ynam/pkg/email/mime"
	"ynam/pkg/email/regex"

	"github.com/shopspring/decimal"
)

type venmoParser struct{}

func NewParser() email.Parser { return &venmoParser{} }

func (p *venmoParser) Name() string { return "venmo" }

// Parse extracts transactions from Venmo payment confirmation emails.
func (p *venmoParser) Parse(emailBody string) ([]email.Transaction, error) {
	var txns []email.Transaction

	emailBody = emailmime.DecodeQPBody(emailBody)

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`You paid\s+([A-Za-z\s]+?)\s*\$?([\d.]+)(?:\s|,|$)`),
		regexp.MustCompile(`([A-Za-z\s]+?)\s+paid you\s*\$?([\d.]+)(?:\s|,|$)`),
		regexp.MustCompile(`You requested\s*\$?([\d.]+)\s+from\s+([A-Za-z\s]+?)(?:\s|,|$)`),
		regexp.MustCompile(`([A-Za-z\s]+?)\s+requested\s*\$?([\d.]+)\s+from you(?:\s|,|$)`),
	}

	var payee, amountStr string
	found := false
	for _, pattern := range patterns {
		m := pattern.FindStringSubmatch(emailBody)
		if len(m) < 3 {
			continue
		}
		if strings.Contains(pattern.String(), "paid you") ||
			(strings.Contains(pattern.String(), "requested") && strings.Contains(pattern.String(), "from you")) {
			payee = strings.TrimSpace(m[1])
			amountStr = m[2]
		} else {
			payee = strings.TrimSpace(m[1])
			amountStr = m[2]
		}
		found = true
		break
	}
	if !found {
		return txns, nil
	}

	amount, err := decimal.NewFromString(strings.ReplaceAll(amountStr, ",", ""))
	if err != nil {
		return txns, nil
	}

	txnDate := regex.ExtractDate(emailBody)
	if txnDate == "" {
		if m := regexp.MustCompile(`([A-Za-z]+ \d{1,2}, \d{4})`).FindStringSubmatch(emailBody); len(m) > 0 {
			txnDate = m[1]
		}
	}
	if txnDate == "" {
		txnDate = emailmime.HeaderDateString(emailBody)
	}

	memo := extractVenmoMemo(emailBody)

	txns = append(txns, email.Transaction{
		Service: "venmo",
		Payee:   payee,
		Amount:  amount,
		Date:    txnDate,
		Memo:    memo,
		Details: map[string]string{"type": "payment"},
	})
	return txns, nil
}

// reTransactionNote matches the Venmo "transaction-note" HTML element content.
// After QP soft-break removal the class attribute is QP-encoded as class=3D"..."
// or the raw class="..." form depending on the email client, so match both.
var reTransactionNote = regexp.MustCompile(`transaction-note[^>]*>([^<]+)<`)

func extractVenmoMemo(body string) string {
	// Primary: extract from the HTML transaction-note element. The content is
	// still QP hex-encoded (e.g. =E2=9C=88 for ✈) because DecodeQPBody only
	// removes soft breaks, not hex sequences.
	if m := reTransactionNote.FindStringSubmatch(body); len(m) > 1 {
		raw := strings.TrimSpace(m[1])
		if raw != "" {
			decoded := emailmime.DecodeQPContent(raw)
			decoded = strings.TrimSpace(decoded)
			if decoded != "" {
				return decoded
			}
		}
	}

	// Fallback: look for quoted note patterns in plain-text part.
	for _, pattern := range []string{
		`(?:Note|Message|For)[:\s]+"([^"]{1,120})"`,
		`(?:Payment for)[:\s]+([^\n]{1,120})`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) > 1 {
			if memo := strings.TrimSpace(m[1]); memo != "" {
				return memo
			}
		}
	}
	return ""
}

func init() { email.Register(NewParser()) }
