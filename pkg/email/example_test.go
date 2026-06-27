package email_test

import (
	"testing"

	// Import parsers to register them
	em "ynam/pkg/email"
	_ "ynam/pkg/email/amazon"
	_ "ynam/pkg/email/apple"
	_ "ynam/pkg/email/paypal"
	_ "ynam/pkg/email/venmo"
)

func TestParserRegistry(t *testing.T) {
	// List all registered parsers
	parsers := em.ListParsers()
	if len(parsers) == 0 {
		t.Error("No parsers registered")
	}

	// Check specific parsers are registered
	expectedParsers := map[string]bool{
		"amazon": false,
		"paypal": false,
		"venmo":  false,
		"apple":  false,
	}

	for _, name := range parsers {
		expectedParsers[name] = true
	}

	for name, found := range expectedParsers {
		if !found {
			t.Errorf("Parser %s not registered", name)
		}
	}
}

func TestAmazonParserRetrieval(t *testing.T) {
	parser := em.GetParser("amazon")
	if parser == nil {
		t.Error("Could not retrieve Amazon parser")
	}
	if parser.Name() != "amazon" {
		t.Errorf("Parser name mismatch: got %s, want amazon", parser.Name())
	}
}

func TestParseWithAll(t *testing.T) {
	// Example Amazon email body
	emailBody := `
Order #123-4567890-1234567
Order Total: $49.99

* Awesome Product Name
Quantity: 1

Expected Delivery: Jan 15, 2024
`

	results := em.ParseWithAll(emailBody, "sender@example.com", "Your Amazon.com order")

	if len(results) == 0 {
		t.Error("No transactions parsed")
	}

	if amazonTxns, exists := results["amazon"]; exists {
		if len(amazonTxns) == 0 {
			t.Error("Amazon parser did not extract transactions")
		}

		txn := amazonTxns[0]
		if txn.Payee != "Amazon" {
			t.Errorf("Wrong payee: got %s, want Amazon", txn.Payee)
		}
		if txn.Amount.String() != "49.99" {
			t.Errorf("Wrong amount: got %s, want 49.99", txn.Amount.String())
		}
	} else {
		t.Error("Amazon transactions not found in results")
	}
}

func TestVenmoParserRetrieval(t *testing.T) {
	parser := em.GetParser("venmo")
	if parser == nil {
		t.Error("Could not retrieve Venmo parser")
	}
	if parser.Name() != "venmo" {
		t.Errorf("Parser name mismatch: got %s, want venmo", parser.Name())
	}
}

func TestPayPalParserRetrieval(t *testing.T) {
	parser := em.GetParser("paypal")
	if parser == nil {
		t.Error("Could not retrieve PayPal parser")
	}
	if parser.Name() != "paypal" {
		t.Errorf("Parser name mismatch: got %s, want paypal", parser.Name())
	}
}

func TestAppleParserRetrieval(t *testing.T) {
	parser := em.GetParser("apple")
	if parser == nil {
		t.Error("Could not retrieve Apple parser")
	}
	if parser.Name() != "apple" {
		t.Errorf("Parser name mismatch: got %s, want apple", parser.Name())
	}
}
