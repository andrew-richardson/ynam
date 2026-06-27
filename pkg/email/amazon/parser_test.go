package amazon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ynam/pkg/email"
)

// parseFixture reads a testdata .eml file and runs it through the parser.
func parseFixture(t *testing.T, name string) []email.Transaction {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	txns, err := NewParser().Parse(string(data))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return txns
}

// TestParseSingleOrder covers every email format that yields exactly one
// transaction: order confirmations (HTML alt items), the "Grand Total:\n$X"
// separate-line layout, shipment notices ("Order Total: $X"), and delivery
// confirmations. They share the same assertions, so they are table-driven.
func TestParseSingleOrder(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		order    string
		amount   string   // "" to skip the amount assertion
		date     string   // "" to skip the date assertion
		memoAny  []string // memo must contain at least one of these
	}{
		{
			name:    "shipment Order Total inline",
			file:    "amazon_shipped.eml",
			order:   "114-4113830-0044259",
			amount:  "29.97",
			memoAny: []string{"Qunol", "CoQ10"},
		},
		{
			name:    "delivery confirmation",
			file:    "amazon1.eml",
			order:   "114-0359112-6662639",
			memoAny: []string{"Polymaker", "PETG Teal 3D Printer"},
		},
		{
			name:    "order confirmation HTML alt items",
			file:    "amazon_dawn_1.eml",
			order:   "111-0594087-0941813",
			amount:  "10.57",
			memoAny: []string{"Belkin"},
		},
		{
			name:    "order confirmation with date",
			file:    "amazon_dawn_nomemo_15_14.eml",
			order:   "111-8168084-7688257",
			amount:  "15.14",
			date:    "2026-06-03",
			memoAny: []string{"Choker", "Necklace"},
		},
		{
			name:    "Grand Total on separate line",
			file:    "amazon_dawn_thorne.eml",
			order:   "111-5933974-8384218",
			amount:  "44",
			memoAny: []string{"THORNE", "Creatine"},
		},
		{
			// Regression: item name lives only in the HTML alt / Quantity-anchored
			// text; previously fell back to the generic "Amazon Order" memo.
			name:    "item name from Quantity anchor (no plain-text items)",
			file:    "amazon_order_no_items.eml",
			order:   "111-8008885-2712214",
			amount:  "17.31",
			memoAny: []string{"Sanwuta", "Seersucker"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txns := parseFixture(t, tc.file)
			if len(txns) != 1 {
				t.Fatalf("expected 1 transaction, got %d", len(txns))
			}
			txn := txns[0]
			if txn.Service != "amazon" {
				t.Errorf("service: got %q, want %q", txn.Service, "amazon")
			}
			if txn.Payee != "Amazon" {
				t.Errorf("payee: got %q, want %q", txn.Payee, "Amazon")
			}
			if txn.Details["order_number"] != tc.order {
				t.Errorf("order_number: got %q, want %q", txn.Details["order_number"], tc.order)
			}
			if tc.amount != "" && txn.Amount.String() != tc.amount {
				t.Errorf("amount: got %q, want %q", txn.Amount.String(), tc.amount)
			}
			if tc.date != "" && txn.Date != tc.date {
				t.Errorf("date: got %q, want %q", txn.Date, tc.date)
			}
			if !containsAny(txn.Memo, tc.memoAny) {
				t.Errorf("memo %q should contain one of %v", txn.Memo, tc.memoAny)
			}
		})
	}
}

// TestParseMultiOrder verifies that an email bundling two distinct orders (each
// with its own Grand Total) becomes two transactions with per-order items.
func TestParseMultiOrder(t *testing.T) {
	txns := parseFixture(t, "amazon_dawn_tarte.eml")
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	byAmount := map[string]email.Transaction{}
	for _, txn := range txns {
		byAmount[txn.Amount.String()] = txn
	}
	if byAmount["11.88"].Details["order_number"] != "111-3652293-6349020" {
		t.Errorf("expected $11.88 -> 111-3652293-6349020, got %q", byAmount["11.88"].Details["order_number"])
	}
	if byAmount["96.04"].Details["order_number"] != "111-6356409-3384206" {
		t.Errorf("expected $96.04 -> 111-6356409-3384206, got %q", byAmount["96.04"].Details["order_number"])
	}
	// Each order should carry its own item names, not a shared/blank memo.
	if !strings.Contains(byAmount["11.88"].Memo, "L'Oreal") {
		t.Errorf("$11.88 memo should name its item, got %q", byAmount["11.88"].Memo)
	}
	if !strings.Contains(byAmount["96.04"].Memo, "Maybelline") {
		t.Errorf("$96.04 memo should name its items, got %q", byAmount["96.04"].Memo)
	}
}

// TestParseCancellation verifies that "Item cancelled successfully" emails are
// parsed with the voided amount from the aria-label and a cancelled marker.
func TestParseCancellation(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		order  string
		amount string
	}{
		{"Lilac St", "amazon_dawn_update_4.eml", "111-1550756-7911411", "11"},
		{"Tupmi", "amazon_dawn_update_5.eml", "111-8764322-3282620", "9.99"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txns := parseFixture(t, tc.file)
			if len(txns) != 1 {
				t.Fatalf("expected 1 transaction, got %d", len(txns))
			}
			txn := txns[0]
			if txn.Service != "amazon" {
				t.Errorf("service: got %q, want %q", txn.Service, "amazon")
			}
			if txn.Details["order_number"] != tc.order {
				t.Errorf("order_number: got %q, want %q", txn.Details["order_number"], tc.order)
			}
			if txn.Amount.String() != tc.amount {
				t.Errorf("amount: got %q, want %q", txn.Amount.String(), tc.amount)
			}
			if txn.Details["cancelled"] != "true" {
				t.Errorf("expected cancelled=true, got %q", txn.Details["cancelled"])
			}
			if !strings.Contains(txn.Memo, "Cancelled") {
				t.Errorf("memo should indicate cancellation, got %q", txn.Memo)
			}
		})
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
