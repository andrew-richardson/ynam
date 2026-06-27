package amazon

import (
	"fmt"
	"regexp"
	"strings"

	"ynam/pkg/email"
	emailmime "ynam/pkg/email/mime"
	"ynam/pkg/email/regex"

	"github.com/shopspring/decimal"
)

// reQPHex matches quoted-printable hex escape sequences like =3D or =0A.
var reQPHex = regexp.MustCompile(`=[0-9A-Fa-f]{2}`)

type amazonParser struct{}

func NewParser() email.Parser { return &amazonParser{} }

func (p *amazonParser) Name() string { return "amazon" }

// Parse extracts transactions from Amazon order/shipping emails.
func (p *amazonParser) Parse(emailBody string) ([]email.Transaction, error) {
	var txns []email.Transaction

	// QP-decode only the body so headers remain intact for Date extraction.
	// DecodeQPBody removes soft line breaks (=\r\n). We then decode remaining
	// =XX hex sequences for HTML attribute matching (aria-label, cancellations).
	emailBody = emailmime.DecodeQPBody(emailBody)
	emailBodyFull := reQPHex.ReplaceAllStringFunc(emailBody, func(m string) string {
		var b byte
		fmt.Sscanf(m[1:], "%02X", &b)
		return string([]byte{b})
	})

	orderNumPattern := regexp.MustCompile(`Order\s*#\s*(?:[:\r\n\s]*)?([\d\-]{15,})`)
	orderMatches := orderNumPattern.FindAllStringSubmatch(emailBody, -1)
	if len(orderMatches) == 0 {
		return txns, nil
	}
	orderNum := orderMatches[0][1]
	orderDate := extractOrderDate(emailBody)

	// Cancellation emails: item was voided before charge — record as a zero-amount
	// marker so sync can skip the matching YNAB debit if it exists, or ignore.
	// The amount in aria-label is the voided charge value (never actually billed).
	if isCancellationEmail(emailBodyFull) {
		amount := extractAriaLabelAmount(emailBodyFull)
		if amount.IsZero() {
			return txns, nil
		}
		items := extractItemsFromHTML(emailBodyFull)
		itemsText := strings.Join(items, "; ")
		if len(itemsText) > 100 {
			itemsText = itemsText[:100] + "..."
		}
		memo := "Cancelled: " + itemsText
		if itemsText == "" {
			memo = "Amazon Cancellation"
		}
		txns = append(txns, email.Transaction{
			Service: "amazon",
			Payee:   "Amazon",
			Amount:  amount,
			Date:    orderDate,
			Memo:    memo,
			Details: map[string]string{
				"order_number": orderNum,
				"items":        itemsText,
				"cancelled":    "true",
			},
		})
		return txns, nil
	}

	// "Thanks for your order!" confirmation emails render each total as
	// "Grand Total:\n$X" with HTML tags between label and amount. Match on the
	// tag-stripped text.
	cleaned := cleanForTotals(emailBodyFull)
	totals := findOrderTotals(cleaned)

	// Multi-order email: a single message bundling several orders, each with its
	// own Grand Total. YNAB records these as separate charges, so a dedicated
	// handler parses each order section individually (number, amount, items via
	// the "Quantity:" anchor, which is the only reliable per-order attribution).
	if len(totals) > 1 {
		sections := parseOrderSections(cleaned)
		for _, s := range sections {
			num := s.orderNum
			if num == "" {
				num = orderNum
			}
			items := s.items
			if len(items) == 0 {
				items = extractItemsFromHTML(emailBodyFull)
			}
			itemsText := joinItems(items)
			memo := itemsText
			if memo == "" {
				memo = "Amazon Order"
			}
			txns = append(txns, email.Transaction{
				Service: "amazon",
				Payee:   "Amazon",
				Amount:  s.amount,
				Date:    orderDate,
				Memo:    memo,
				Details: map[string]string{
					"order_number": num,
					"items":        itemsText,
				},
			})
		}
		if len(txns) > 0 {
			return txns, nil
		}
	}

	// Single-order confirmation: one Grand Total. The richest item name is the
	// HTML image alt text (full product title). Extract from emailBodyFull — the
	// hex-decoded body — so alt="..." matches (in the raw body it is alt=3D"...").
	// Fall back to the "Quantity:"-anchored visible text if no alt is found.
	if len(totals) == 1 {
		items := extractItems(emailBody)
		if len(items) == 0 {
			items = extractItemsFromHTML(emailBodyFull)
		}
		if len(items) == 0 {
			if secs := parseOrderSections(cleaned); len(secs) == 1 {
				items = secs[0].items
			}
		}
		itemsText := joinItems(items)
		memo := itemsText
		if memo == "" {
			memo = "Amazon Order"
		}
		txns = append(txns, email.Transaction{
			Service: "amazon",
			Payee:   "Amazon",
			Amount:  totals[0].amount,
			Date:    orderDate,
			Memo:    memo,
			Details: map[string]string{
				"order_number": orderNum,
				"items":        itemsText,
			},
		})
		return txns, nil
	}

	// Fallback for shipment emails ("Order Total: $X" / "Total\n29.97 USD")
	// which carry a single order and amount inline in the plain-text part.
	totalPatterns := []*regexp.Regexp{
		regexp.MustCompile(`Order Total[:\s]+\$?([\d,]+\.\d{2})`),
		regexp.MustCompile(`Total:\s*\$?([\d,]+\.\d{2})`),
		// Shipment emails use "Total\n29.97 USD" (no colon, amount on next line).
		regexp.MustCompile(`(?m)^Total\s*\n\s*([\d,]+\.\d{2})\s*(?:USD)?`),
	}
	var totalAmount string
	for _, p := range totalPatterns {
		if m := p.FindStringSubmatch(emailBody); len(m) > 1 {
			totalAmount = m[1]
			break
		}
	}
	if totalAmount == "" {
		return txns, nil
	}

	amount, err := decimal.NewFromString(strings.ReplaceAll(totalAmount, ",", ""))
	if err != nil {
		return txns, nil
	}

	items := extractItems(emailBody)
	if len(items) == 0 {
		items = extractItemsFromHTML(emailBody)
	}
	itemsText := joinItems(items)
	memo := itemsText
	if memo == "" {
		memo = "Amazon Order"
	}

	txns = append(txns, email.Transaction{
		Service: "amazon",
		Payee:   "Amazon",
		Amount:  amount,
		Date:    orderDate,
		Memo:    memo,
		Details: map[string]string{
			"order_number": orderNum,
			"items":        itemsText,
		},
	})
	return txns, nil
}

// orderTotal is a Grand/Order Total amount and its byte offset in the text.
type orderTotal struct {
	amount decimal.Decimal
	pos    int
}

// orderSection is one fully-parsed order extracted from a multi-order email.
type orderSection struct {
	orderNum string
	amount   decimal.Decimal
	items    []string
}

// reGrandTotal matches "Grand Total:" / "Order Total:" followed (possibly across
// stripped-tag whitespace) by a dollar amount.
var reGrandTotal = regexp.MustCompile(`(?i)(?:Grand|Order) Total:[\s\S]{0,40}?\$([\d,]+\.\d{2})`)

// reOrderNum matches the canonical Amazon order number (NNN-NNNNNNN-NNNNNNN).
var reOrderNum = regexp.MustCompile(`\d{3}-\d{7}-\d{7}`)

// parseOrderSections splits a multi-order confirmation's tag-stripped text into
// per-order sections, each delimited by its "Grand Total:" line. For each order
// it extracts the order number, total amount, and visible item names.
//
// The cleaned layout for each order looks like:
//
//	Order #
//	NNN-NNNNNNN-NNNNNNN
//	<product name 1>
//	$<price>
//	... (more priced items) ...
//	Grand Total:
//	$<order total>
func parseOrderSections(cleaned string) []orderSection {
	lines := strings.Split(cleaned, "\n")

	// Index the "Grand Total:" lines; each ends one order section.
	var gtIdx []int
	for i, l := range lines {
		if strings.Contains(strings.ToLower(strings.TrimSpace(l)), "grand total") {
			gtIdx = append(gtIdx, i)
		}
	}

	var sections []orderSection
	start := 0
	for _, gt := range gtIdx {
		sec := parseSectionLines(lines, start, gt)
		start = gt + 1
		if sec.amount.IsZero() {
			continue
		}
		sections = append(sections, sec)
	}
	return sections
}

// parseSectionLines parses one order section spanning lines[start:gt], where gt
// is the index of its "Grand Total:" line (the amount follows on a later line).
func parseSectionLines(lines []string, start, gt int) orderSection {
	var sec orderSection

	// Amount: first "$<num>" line at or after the Grand Total label.
	for i := gt + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if m := regexp.MustCompile(`^\$([\d,]+\.\d{2})`).FindStringSubmatch(l); len(m) > 1 {
			if amt, err := decimal.NewFromString(strings.ReplaceAll(m[1], ",", "")); err == nil {
				sec.amount = amt
			}
		}
		break
	}

	// Order number: last canonical order number within the section.
	for i := start; i < gt; i++ {
		if m := reOrderNum.FindString(lines[i]); m != "" {
			sec.orderNum = m
		}
	}

	// Items: Amazon lists each line item as "<product name>" followed by a
	// "Quantity: N" line. The product name is the nearest non-empty line above
	// each "Quantity:" marker.
	for i := start; i < gt; i++ {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[i])), "quantity") {
			continue
		}
		name := precedingNonEmpty(lines, i, start)
		if name == "" || !isItemNameLine(name) {
			continue
		}
		clean := regex.CleanText(name)
		if !containsItem(sec.items, clean) && len(sec.items) < 5 {
			sec.items = append(sec.items, clean)
		}
	}
	return sec
}

// precedingNonEmpty returns the nearest non-empty trimmed line above index i,
// bounded below by start.
func precedingNonEmpty(lines []string, i, start int) string {
	for j := i - 1; j >= start; j-- {
		if l := strings.TrimSpace(lines[j]); l != "" {
			return l
		}
	}
	return ""
}

// isItemNameLine reports whether a line looks like a product name rather than a
// structural label, price, or order number.
func isItemNameLine(s string) bool {
	if len(s) < 8 || strings.HasPrefix(s, "$") {
		return false
	}
	lower := strings.ToLower(s)
	for _, bad := range []string{"order #", "grand total", "ordered", "amazon.com",
		"payment", "invoice", "by placing", "subtotal", "shipping",
		"view or edit", "arriving", "your order", "buy again", "your account"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	if reOrderNum.MatchString(s) {
		return false
	}
	return strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

func containsItem(items []string, name string) bool {
	for _, it := range items {
		if it == name {
			return true
		}
	}
	return false
}

// reHTMLEntity matches HTML entities like &nbsp; &#xA0; &#8199; left behind
// after tag stripping.
var reHTMLEntity = regexp.MustCompile(`&#?[0-9a-zA-Z]+;`)

// cleanForTotals produces tag-stripped text with HTML entities and junk lines
// removed so that "Grand Total:\n$X" and per-order item names become reliably
// matchable on a line-by-line basis.
func cleanForTotals(htmlBody string) string {
	text := emailmime.ExtractHTMLText(htmlBody)
	text = reHTMLEntity.ReplaceAllString(text, "")
	// Blank out lines that are now empty or pure punctuation/whitespace.
	junk := regexp.MustCompile(`(?m)^[\s\p{P}]*$`)
	text = junk.ReplaceAllString(text, "")
	return regexp.MustCompile(`\n{2,}`).ReplaceAllString(text, "\n")
}

// findOrderTotals returns every Grand/Order Total amount in document order.
func findOrderTotals(cleaned string) []orderTotal {
	var out []orderTotal
	for _, m := range reGrandTotal.FindAllStringSubmatchIndex(cleaned, -1) {
		amtStr := strings.ReplaceAll(cleaned[m[2]:m[3]], ",", "")
		amt, err := decimal.NewFromString(amtStr)
		if err != nil {
			continue
		}
		out = append(out, orderTotal{amount: amt, pos: m[0]})
	}
	return out
}

// joinItems joins item names with "; " and truncates overly long results.
func joinItems(items []string) string {
	s := strings.Join(items, "; ")
	if len(s) > 100 {
		s = s[:100] + "..."
	}
	return s
}

// isCancellationEmail returns true when the email body indicates an Amazon
// item cancellation (no charge was made to the customer).
func isCancellationEmail(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "your order was cancelled") ||
		strings.Contains(lower, "your items were cancelled") ||
		strings.Contains(lower, "item cancelled successfully")
}

// extractAriaLabelAmount parses the amount from Amazon's aria-label attribute
// used in cancellation emails: aria-label="{amount=9.99, currencyCode=...}".
func extractAriaLabelAmount(body string) decimal.Decimal {
	re := regexp.MustCompile(`aria-label="\{amount=([\d.]+),`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		if d, err := decimal.NewFromString(m[1]); err == nil {
			return d
		}
	}
	return decimal.Zero
}

func extractOrderDate(body string) string {
	if m := regexp.MustCompile(`Order Placed[:\s]+([A-Za-z]+ \d{1,2}, \d{4})`).FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	if date := regex.ExtractDate(body); date != "" {
		return date
	}
	return emailmime.HeaderDateString(body)
}

// extractItems parses bullet-formatted items from the plain-text MIME part.
func extractItems(body string) []string {
	var items []string
	skipPatterns := []string{
		"order", "total", "shipping", "tax", "subtotal",
		"delivery", "estimated", "thank you", "questions",
		"customer service", "return", "guarantee", "arriving",
		"price", "quantity", "item", "sold", "gift",
	}

	bulletPattern := regexp.MustCompile(`^[\s]*[\*\x{2022}-]\s+(.+)$`)
	lines := strings.Split(body, "\n")

	for i, rawLine := range lines {
		matches := bulletPattern.FindStringSubmatch(rawLine)
		if len(matches) < 2 {
			continue
		}
		itemText := strings.TrimSpace(matches[1])

		// Collect continuation lines.
		for j := i + 1; j < len(lines); j++ {
			nextLine := strings.TrimSpace(lines[j])
			if nextLine == "" {
				continue
			}
			if bulletPattern.MatchString(lines[j]) || isMarkerLine(nextLine) {
				break
			}
			itemText += " " + nextLine
		}

		itemText = regex.RemoveWhitespace(itemText)
		itemText = regex.CleanText(itemText)

		skip := false
		for _, s := range skipPatterns {
			if strings.Contains(strings.ToLower(itemText), s) {
				skip = true
				break
			}
		}
		if !skip && regexp.MustCompile(`\$\s*\d`).MatchString(itemText) {
			skip = true
		}
		if skip || len(itemText) < 6 {
			continue
		}

		dup := false
		for _, ex := range items {
			if ex == itemText {
				dup = true
				break
			}
		}
		if !dup && len(items) < 5 {
			items = append(items, itemText)
		}
	}
	return items
}

// extractItemsFromHTML extracts product names from Amazon HTML emails via
// image alt attributes — Amazon uses the full product title there consistently
// across order, shipping, and delivery confirmation formats.
func extractItemsFromHTML(body string) []string {
	var items []string
	seen := make(map[string]bool)

	altRe := regexp.MustCompile(`(?i)alt=(?:"([^"]{10,}?)"|'([^']{10,}?)')`)
	for _, m := range altRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "amazon") || strings.Contains(lower, "logo") ||
			strings.Contains(lower, "icon") || strings.Contains(lower, "image") ||
			len(name) < 10 {
			continue
		}
		seen[name] = true
		items = append(items, name)
		if len(items) >= 3 {
			break
		}
	}
	return items
}

func isMarkerLine(line string) bool {
	for _, marker := range []string{
		"quantity", "price", "item", "shipped", "expected delivery",
		"order total", "grand total", "subtotal", "shipping",
	} {
		if strings.Contains(strings.ToLower(line), marker) {
			return true
		}
	}
	return false
}

func init() { email.Register(NewParser()) }
