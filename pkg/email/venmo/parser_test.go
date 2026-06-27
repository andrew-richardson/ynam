package venmo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVenmoPaymentEmail(t *testing.T) {
	emlPath := filepath.Join("..", "..", "..", "testdata", "venmo1.eml")
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
	if txn.Service != "venmo" {
		t.Errorf("service: got %q, want %q", txn.Service, "venmo")
	}
	if txn.Payee != "Justin Hay" {
		t.Errorf("payee: got %q, want %q", txn.Payee, "Justin Hay")
	}
	if got := txn.Amount.String(); got != "443.85" {
		t.Errorf("amount: got %q, want %q", got, "443.85")
	}
	// The memo in venmo1.eml is a ✈ emoji (QP-encoded as =E2=9C=88).
	if txn.Memo == "" {
		t.Errorf("memo should not be empty")
	}
	if txn.Memo != "✈" {
		t.Errorf("memo: got %q, want %q", txn.Memo, "✈")
	}
}
