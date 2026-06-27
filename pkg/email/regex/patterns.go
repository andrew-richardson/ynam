package regex

import "regexp"

// Common currency and amount patterns
var (
	// CurrencyPattern matches $123.45, $1,234.56, etc
	CurrencyPattern = regexp.MustCompile(`(?:\$\s*)?(\d{1,3}(?:,\d{3})*(?:\.\d{2})?)`)

	// DatePattern matches various date formats
	DatePatternISO = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	DatePatternUS  = regexp.MustCompile(`(\d{1,2}/\d{1,2}/\d{4})`)

	// NamePattern matches words and special characters (for payee names)
	NamePattern = regexp.MustCompile(`[\w\s\-&'\.]+`)

	// PhonePattern matches phone numbers
	PhonePattern = regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`)

	// MoneyPattern matches complete amount with currency
	MoneyPattern = regexp.MustCompile(`(?:\$\s*)?(\d+(?:,\d{3})*(?:\.\d{2})?)`)
)

// Helper functions for common extraction patterns

// ExtractAmount finds the first currency amount in text
func ExtractAmount(text string) string {
	matches := MoneyPattern.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// ExtractAllAmounts finds all currency amounts in text
func ExtractAllAmounts(text string) []string {
	matches := MoneyPattern.FindAllStringSubmatch(text, -1)
	var amounts []string
	for _, match := range matches {
		if len(match) > 1 {
			amounts = append(amounts, match[1])
		}
	}
	return amounts
}

// ExtractDate finds the first ISO date in text
func ExtractDate(text string) string {
	matches := DatePatternISO.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// RemoveWhitespace normalizes whitespace
func RemoveWhitespace(text string) string {
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(text, " ")
}

// CleanText removes special formatting characters
func CleanText(text string) string {
	// Remove common formatting chars
	re := regexp.MustCompile(`[\*\-\x{2022}\[]|[\]\)]$`)
	return re.ReplaceAllString(text, "")
}
