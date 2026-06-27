package paypal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAutoPaymentEmail(t *testing.T) {
	emlPath := filepath.Join("..", "..", "..", "testdata", "paypal1.eml")
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
	if txn.Service != "paypal" {
		t.Errorf("service: got %q, want %q", txn.Service, "paypal")
	}
	if txn.Payee != "TickTick Limited" {
		t.Errorf("payee: got %q, want %q", txn.Payee, "TickTick Limited")
	}
	if got := txn.Amount.String(); got != "3.99" {
		t.Errorf("amount: got %q, want %q", got, "3.99")
	}
	if txn.Date == "" {
		t.Errorf("date should not be empty")
	}
	if txn.Details["transaction_id"] != "17S59139SP901063W" {
		t.Errorf("transaction_id: got %q, want %q", txn.Details["transaction_id"], "17S59139SP901063W")
	}
	// Memo should contain the subscription description
	if !strings.Contains(txn.Memo, "TickTick Premium") {
		t.Errorf("memo should contain subscription description, got %q", txn.Memo)
	}
}

func TestParseAuthorizationEmail(t *testing.T) {
	emlPath := filepath.Join("..", "..", "..", "testdata", "paypal2.eml")
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
	if txn.Service != "paypal" {
		t.Errorf("service: got %q, want %q", txn.Service, "paypal")
	}
	if txn.Payee != "Cloudflare Inc" {
		t.Errorf("payee: got %q, want %q", txn.Payee, "Cloudflare Inc")
	}
	if got := txn.Amount.String(); got != "10.46" {
		t.Errorf("amount: got %q, want %q", got, "10.46")
	}
	if txn.Date == "" {
		t.Errorf("date should not be empty")
	}
}
