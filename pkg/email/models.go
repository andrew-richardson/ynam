package email

import (
	"github.com/shopspring/decimal"
)

// Transaction represents a parsed email transaction
type Transaction struct {
	Service string            // "amazon", "venmo", "paypal", "apple"
	Payee   string            // Who payment is to/from
	Amount  decimal.Decimal   // Amount in dollars
	Date    string            // ISO format: 2006-01-02
	Memo    string            // Transaction description
	Raw     string            // Original matched text for debugging
	Details map[string]string // Service-specific details (e.g., order number, item list)
}

// EmailResult wraps parsed transactions with source info
type EmailResult struct {
	From         string
	Subject      string
	Date         string
	Transactions []Transaction
	Error        error // Set if parsing failed, continue with next email
}

// ParseResult groups results by service
type ParseResult struct {
	By  string        // Service name
	TXN []Transaction // Parsed transactions
	Err error         // Parse error if any
}
