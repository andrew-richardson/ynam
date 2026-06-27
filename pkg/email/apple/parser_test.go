package apple

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAppleSubscriptionEmail(t *testing.T) {
	emlPath := filepath.Join("..", "..", "..", "testdata", "apple1.eml")
	data, err := os.ReadFile(emlPath)
	if err != nil {
		t.Fatalf("read sample email: %v", err)
	}

	parser := NewParser()
	txns, err := parser.Parse(string(data))
	if err != nil {
		t.Fatalf("parser returned error: %v", err)
	}
	if len(txns) == 0 {
		t.Fatalf("expected at least one transaction, got none")
	}

	txn := txns[0]
	if txn.Service != "apple" {
		t.Errorf("service: got %q, want %q", txn.Service, "apple")
	}
	if txn.Payee != "Apple Inc." {
		t.Errorf("payee: got %q, want %q", txn.Payee, "Apple Inc.")
	}
	if got := txn.Amount.String(); got != "21.65" {
		t.Errorf("amount: got %q, want %q", got, "21.65")
	}
	if txn.Details["order_id"] != "MM63T5MM3S" {
		t.Errorf("order_id: got %q, want %q", txn.Details["order_id"], "MM63T5MM3S")
	}
	if txn.Details["item"] == "" {
		t.Errorf("item detail should not be empty")
	}
}
