package audible

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAudibleMembership(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "audible_dawn_2.eml"))
	if err != nil {
		t.Fatalf("read sample email: %v", err)
	}
	txns, err := NewParser().Parse(string(data))
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}
	if len(txns) == 0 {
		t.Fatal("expected at least one transaction, got none")
	}
	txn := txns[0]
	if txn.Service != "audible" {
		t.Errorf("service: got %q, want %q", txn.Service, "audible")
	}
	if txn.Payee != "Audible" {
		t.Errorf("payee: got %q, want %q", txn.Payee, "Audible")
	}
	if got := txn.Amount.String(); got != "16.18" {
		t.Errorf("amount: got %q, want %q", got, "16.18")
	}
	if txn.Details["order_number"] != "D01-0196618-3945841" {
		t.Errorf("order_number: got %q, want %q", txn.Details["order_number"], "D01-0196618-3945841")
	}
	if txn.Memo != "Audible Premium Plus Monthly" {
		t.Errorf("memo: got %q, want %q", txn.Memo, "Audible Premium Plus Monthly")
	}
}

func TestParseAudiblePreOrder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "audible_dawn_4.eml"))
	if err != nil {
		t.Fatalf("read sample email: %v", err)
	}
	txns, err := NewParser().Parse(string(data))
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}
	if len(txns) == 0 {
		t.Fatal("expected at least one transaction, got none")
	}
	txn := txns[0]
	if txn.Service != "audible" {
		t.Errorf("service: got %q, want %q", txn.Service, "audible")
	}
	if got := txn.Amount.String(); got != "7.47" {
		t.Errorf("amount: got %q, want %q", got, "7.47")
	}
	if txn.Details["order_number"] != "D01-7634508-9273830" {
		t.Errorf("order_number: got %q, want %q", txn.Details["order_number"], "D01-7634508-9273830")
	}
	if txn.Details["type"] != "preorder" {
		t.Errorf("type: got %q, want %q", txn.Details["type"], "preorder")
	}
}

func TestParseAudibleCreditOrder(t *testing.T) {
	// Credit-only orders ($0.00) should produce no transactions.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "audible_dawn_1.eml"))
	if err != nil {
		t.Fatalf("read sample email: %v", err)
	}
	txns, err := NewParser().Parse(string(data))
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}
	if len(txns) != 0 {
		t.Errorf("expected 0 transactions for credit-only order, got %d", len(txns))
	}
}
