package cmd

import (
	"testing"
	"time"

	"ynam/pkg/config"
	"ynam/pkg/email"
	"ynam/pkg/ynab"

	"github.com/shopspring/decimal"
)

func makeYNAB(amount, date, payee string) ynab.Transaction {
	d, _ := decimal.NewFromString(amount)
	t, _ := time.Parse("2006-01-02", date)
	return ynab.Transaction{Amount: d, Date: t, Payee: payee}
}

func makeEmail(amount, date string) email.Transaction {
	d, _ := decimal.NewFromString(amount)
	return email.Transaction{Amount: d, Date: date, Service: "amazon", Payee: "Amazon"}
}

func testServices() map[string]config.ServiceConfig {
	return map[string]config.ServiceConfig{
		"amazon": {Sync: true, PayeeKeywords: []string{"amazon"}},
		"apple":  {Sync: true, PayeeKeywords: []string{"apple"}},
		"venmo":  {Sync: true, PayeeKeywords: []string{"venmo"}},
		"paypal": {Sync: true, PayeeKeywords: []string{"paypal"}},
	}
}

func TestAmountsMatch(t *testing.T) {
	tests := []struct {
		ynabAmt  string
		emailAmt string
		want     bool
	}{
		{"10.00", "10.00", true},
		{"-10.00", "10.00", true}, // YNAB stores debits as negative
		{"10.00", "9.99", false},
		{"0.00", "0.00", true},
	}
	for _, tt := range tests {
		yt := makeYNAB(tt.ynabAmt, "2026-01-01", "Amazon")
		et := makeEmail(tt.emailAmt, "2026-01-01")
		got := amountsMatch(yt, et)
		if got != tt.want {
			t.Errorf("amountsMatch(%s, %s) = %v, want %v", tt.ynabAmt, tt.emailAmt, got, tt.want)
		}
	}
}

func TestDatesMatch(t *testing.T) {
	tests := []struct {
		ynabDate  string
		emailDate string
		want      bool
	}{
		{"2026-01-01", "2026-01-01", true},
		{"2026-01-05", "2026-01-01", true},  // 4 days within window
		{"2026-01-06", "2026-01-01", true},  // exactly 5 days
		{"2026-01-07", "2026-01-01", false}, // 6 days exceeds window
		{"2026-01-01", "", true},            // empty email date always matches
		{"2026-01-01", "not-a-date", true},  // unparseable date always matches
	}
	for _, tt := range tests {
		yt := makeYNAB("10.00", tt.ynabDate, "Amazon")
		et := makeEmail("10.00", tt.emailDate)
		got := datesMatch(yt, et)
		if got != tt.want {
			t.Errorf("datesMatch(ynab=%s, email=%q) = %v, want %v", tt.ynabDate, tt.emailDate, got, tt.want)
		}
	}
}

func TestPayeeMatches(t *testing.T) {
	svcs := testServices()
	tests := []struct {
		ynabPayee    string
		emailService string
		want         bool
	}{
		{"Amazon", "amazon", true},
		{"AMAZON MKTPLACE PMTS", "amazon", true},
		{"Audible", "amazon", false},   // the false-positive case we had
		{"Bill Miller Bar-B-Q", "apple", false},
		{"Apple", "apple", true},
		{"APPLE.COM/BILL", "apple", true},
		{"Venmo", "venmo", true},
		{"Dunkin' Donuts", "amazon", false},
	}
	for _, tt := range tests {
		yt := makeYNAB("-10.00", "2026-01-01", tt.ynabPayee)
		et := email.Transaction{Service: tt.emailService, Amount: yt.Amount.Neg()}
		got := payeeMatches(yt, et, svcs)
		if got != tt.want {
			t.Errorf("payeeMatches(payee=%q, service=%q) = %v, want %v",
				tt.ynabPayee, tt.emailService, got, tt.want)
		}
	}
}

func TestPayeeMatches_DefaultKeyword(t *testing.T) {
	// No PayeeKeywords configured — falls back to service name.
	svcs := map[string]config.ServiceConfig{
		"amazon": {Sync: true}, // no PayeeKeywords
	}
	yt := makeYNAB("-10.00", "2026-01-01", "Amazon")
	et := email.Transaction{Service: "amazon"}
	if !payeeMatches(yt, et, svcs) {
		t.Error("expected match using service name as default keyword")
	}

	ytBad := makeYNAB("-10.00", "2026-01-01", "Audible")
	if payeeMatches(ytBad, et, svcs) {
		t.Error("Audible should not match amazon service")
	}
}

func TestBuildMemo(t *testing.T) {
	tests := []struct {
		name string
		et   email.Transaction
		want string
	}{
		{"memo set", email.Transaction{Memo: "Order #123", Payee: "Amazon", Service: "amazon"}, "Order #123"},
		{"memo empty uses payee", email.Transaction{Memo: "", Payee: "Amazon", Service: "amazon"}, "Amazon"},
		{"memo and payee empty uses service", email.Transaction{Memo: "", Payee: "", Service: "amazon"}, "amazon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMemo(tt.et)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindMatch(t *testing.T) {
	svcs := testServices()
	emailTxns := []email.Transaction{
		makeEmail("10.00", "2026-01-01"),
		makeEmail("25.50", "2026-01-05"),
		makeEmail("5.99", "2026-01-10"),
	}

	t.Run("finds exact match", func(t *testing.T) {
		yt := makeYNAB("-25.50", "2026-01-06", "Amazon")
		used := make([]bool, len(emailTxns))
		idx := findMatch(yt, emailTxns, used, svcs)
		if idx != 1 {
			t.Errorf("expected index 1, got %d", idx)
		}
	})

	t.Run("skips used transactions", func(t *testing.T) {
		yt := makeYNAB("-25.50", "2026-01-06", "Amazon")
		used := []bool{false, true, false}
		idx := findMatch(yt, emailTxns, used, svcs)
		if idx != -1 {
			t.Errorf("expected -1 (already used), got %d", idx)
		}
	})

	t.Run("no match returns -1", func(t *testing.T) {
		yt := makeYNAB("-99.99", "2026-01-01", "Amazon")
		used := make([]bool, len(emailTxns))
		idx := findMatch(yt, emailTxns, used, svcs)
		if idx != -1 {
			t.Errorf("expected -1, got %d", idx)
		}
	})

	t.Run("date out of window excludes match", func(t *testing.T) {
		yt := makeYNAB("-10.00", "2026-01-10", "Amazon") // 9 days from email date 2026-01-01
		used := make([]bool, len(emailTxns))
		idx := findMatch(yt, emailTxns, used, svcs)
		if idx != -1 {
			t.Errorf("expected -1 (date too far), got %d", idx)
		}
	})

	t.Run("wrong payee excludes match", func(t *testing.T) {
		yt := makeYNAB("-10.00", "2026-01-01", "Audible") // amount matches but payee is wrong
		used := make([]bool, len(emailTxns))
		idx := findMatch(yt, emailTxns, used, svcs)
		if idx != -1 {
			t.Errorf("expected -1 (payee mismatch), got %d", idx)
		}
	})
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"a; b; c", []string{"a", "b", "c"}},
		{"a, b, c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"", nil},
		{"a;  ; b", []string{"a", "b"}}, // drops empty segment (semicolon path)
		{"10.98; 12.97; 32.00", []string{"10.98", "12.97", "32.00"}},
	}
	for _, tt := range tests {
		got := splitList(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitList(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitList(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}
