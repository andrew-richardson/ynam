package imap

import (
	"testing"

	"ynam/pkg/config"

	imapv2 "github.com/emersion/go-imap/v2"
)

func TestServicesForMessage(t *testing.T) {
	filters := map[string]config.ServiceConfig{
		"amazon": {
			From:    []string{"auto-confirm@amazon.com", "shipment-tracking@amazon.com"},
			Subject: []string{"Ordered", "Shipped"},
		},
		"paypal": {
			From:    []string{"service@paypal.com"},
			Subject: []string{"payment"},
		},
	}

	tests := []struct {
		name    string
		from    string
		subject string
		want    []string
	}{
		{
			name:    "amazon by from address",
			from:    "auto-confirm@amazon.com",
			subject: "Your order",
			want:    []string{"amazon"},
		},
		{
			name:    "amazon by subject keyword",
			from:    "unknown@example.com",
			subject: "Your order has Shipped",
			want:    []string{"amazon"},
		},
		{
			name:    "paypal by from",
			from:    "service@paypal.com",
			subject: "Receipt",
			want:    []string{"paypal"},
		},
		{
			name:    "no match",
			from:    "nobody@example.com",
			subject: "Hello",
			want:    nil,
		},
		{
			name:    "case-insensitive from match",
			from:    "Service@PayPal.Com",
			subject: "Receipt",
			want:    []string{"paypal"},
		},
		{
			name:    "empty filters",
			from:    "auto-confirm@amazon.com",
			subject: "Ordered",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filters
			if tt.name == "empty filters" {
				f = nil
			}
			got := servicesForMessage(tt.from, tt.subject, f)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if len(tt.want) > 0 && got[0] != tt.want[0] {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildOrSearchCriteria(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		got := buildOrSearchCriteria(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("single clause deduplicates into one pair", func(t *testing.T) {
		c := imapv2.SearchCriteria{Header: []imapv2.SearchCriteriaHeaderField{{Key: "FROM", Value: "a@b.com"}}}
		got := buildOrSearchCriteria([]imapv2.SearchCriteria{c})
		if len(got) != 1 {
			t.Fatalf("expected 1 pair, got %d", len(got))
		}
	})

	t.Run("two clauses returns one top-level pair", func(t *testing.T) {
		c1 := imapv2.SearchCriteria{Header: []imapv2.SearchCriteriaHeaderField{{Key: "FROM", Value: "a@a.com"}}}
		c2 := imapv2.SearchCriteria{Header: []imapv2.SearchCriteriaHeaderField{{Key: "FROM", Value: "b@b.com"}}}
		got := buildOrSearchCriteria([]imapv2.SearchCriteria{c1, c2})
		if len(got) != 1 {
			t.Fatalf("expected 1 pair at top level, got %d", len(got))
		}
	})

	t.Run("three clauses nested correctly", func(t *testing.T) {
		clauses := make([]imapv2.SearchCriteria, 3)
		for i := range clauses {
			clauses[i] = imapv2.SearchCriteria{Header: []imapv2.SearchCriteriaHeaderField{{Key: "FROM", Value: "x"}}}
		}
		got := buildOrSearchCriteria(clauses)
		if len(got) != 1 {
			t.Fatalf("expected 1 pair at top, got %d", len(got))
		}
		// The right side should itself be an OR criteria.
		right := got[0][1]
		if len(right.Or) == 0 {
			t.Errorf("expected nested OR on right side")
		}
	})
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"A", "a", "B", " b ", "", "C"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}
