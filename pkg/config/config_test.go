package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary test config file
	testConfigContent := `
ynab_api_token: "test_token_12345"
ynab_budget_id: "test_budget_id"
days_since: 10
services:
  amazon:
    sync: true
    from:
      - "order-update@amazon.com"
    subject:
      - "your order"
  paypal:
    sync: false
    from:
      - "service@paypal.com"
    subject:
      - "receipt"
  venmo:
    sync: true
    from:
      - "transaction@venmo.com"
    subject:
      - "you paid"
  apple:
    sync: true
    from:
      - "itunes@apple.com"
    subject:
      - "your receipt from apple"
anthropic_api_key: "test_anthropic_key"
email_accounts:
  - email: "user1@gmail.com"
    password: "pass1"
    imap_server: "imap.gmail.com"
    mailboxes:
      - INBOX
  - email: "user2@icloud.com"
    password: "pass2"
    imap_server: "imap.mail.me.com"
    mailboxes:
      - INBOX
      - "Confirmations & Receipts"
`

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test_config_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testConfigContent)
	require.NoError(t, err)
	tmpFile.Close()

	// Load config
	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	// Verify loaded values
	assert.Equal(t, "test_token_12345", cfg.YNABAPIToken)
	assert.Equal(t, "test_budget_id", cfg.YNABBudgetID)
	assert.Equal(t, 10, cfg.DaysSince)
	assert.Equal(t, "test_anthropic_key", cfg.AnthropicAPIKey)

	// Verify service config
	assert.True(t, cfg.Services["amazon"].Sync)
	assert.True(t, cfg.Services["venmo"].Sync)
	assert.False(t, cfg.Services["paypal"].Sync)
	assert.True(t, cfg.Services["apple"].Sync)

	// Verify email accounts
	assert.Len(t, cfg.EmailAccounts, 2)
	assert.Equal(t, "user1@gmail.com", cfg.EmailAccounts[0].Email)
	assert.Equal(t, "imap.gmail.com", cfg.EmailAccounts[0].IMAPServer)
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		shouldErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				YNABAPIToken: "token",
				YNABBudgetID: "budget",
				DaysSince:    7,
				Services: map[string]ServiceConfig{
					"venmo": {Sync: true},
				},
				EmailAccounts: []EmailAccount{{Email: "test@gmail.com", Password: "pass", IMAPServer: "imap.gmail.com", Mailboxes: []string{"INBOX"}}},
			},
			shouldErr: false,
		},
		{
			name: "missing api token",
			config: &Config{
				YNABBudgetID: "budget",
				Services: map[string]ServiceConfig{
					"venmo": {Sync: true},
				},
				EmailAccounts: []EmailAccount{{Email: "test@gmail.com", Password: "pass", IMAPServer: "imap.gmail.com", Mailboxes: []string{"INBOX"}}},
			},
			shouldErr: true,
		},
		{
			name: "missing budget id",
			config: &Config{
				YNABAPIToken: "token",
				Services: map[string]ServiceConfig{
					"venmo": {Sync: true},
				},
				EmailAccounts: []EmailAccount{{Email: "test@gmail.com", Password: "pass", IMAPServer: "imap.gmail.com", Mailboxes: []string{"INBOX"}}},
			},
			shouldErr: true,
		},
		{
			name: "no email accounts",
			config: &Config{
				YNABAPIToken: "token",
				YNABBudgetID: "budget",
			},
			shouldErr: true,
		},
		{
			name: "negative days_since",
			config: &Config{
				YNABAPIToken: "token",
				YNABBudgetID: "budget",
				DaysSince:    -1,
				Services: map[string]ServiceConfig{
					"venmo": {Sync: true},
				},
				EmailAccounts: []EmailAccount{{Email: "test@gmail.com", Password: "pass", IMAPServer: "imap.gmail.com", Mailboxes: []string{"INBOX"}}},
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSinceDateAsTime(t *testing.T) {
	cfg := &Config{DaysSince: 7}

	// 1. Capture a single anchor time
	now := time.Now()

	// 2. Adjust your function if it allows passing a base time,
	// or use this single anchor to build your window:
	sinceTime := cfg.SinceDateAsTime(0)

	// 3. Use the single anchor variable for comparison
	expectedMin := now.AddDate(0, 0, -7).Add(-1 * time.Minute)
	expectedMax := now.AddDate(0, 0, -7).Add(1 * time.Minute)

	assert.True(t, sinceTime.After(expectedMin) && sinceTime.Before(expectedMax),
		"sinceTime should be approximately 7 days ago")
}

func TestIsSyncEnabled(t *testing.T) {
	cfg := &Config{
		Services: map[string]ServiceConfig{
			"amazon": {Sync: true},
			"venmo":  {Sync: false},
			"paypal": {Sync: true},
			"apple":  {Sync: false},
		},
	}

	assert.True(t, cfg.IsSyncEnabled("amazon"))
	assert.False(t, cfg.IsSyncEnabled("venmo"))
	assert.True(t, cfg.IsSyncEnabled("paypal"))
	assert.False(t, cfg.IsSyncEnabled("apple"))
	assert.False(t, cfg.IsSyncEnabled("invalid"))
}

func baseConfig() *Config {
	return &Config{
		YNABAPIToken: "token",
		YNABBudgetID: "budget",
		DaysSince:    7,
		Services: map[string]ServiceConfig{
			"amazon": {Sync: true, From: []string{"auto-confirm@amazon.com"}, Subject: []string{"Ordered"}},
			"paypal": {Sync: true, From: []string{"service@paypal.com"}, Subject: []string{}},
			"venmo":  {Sync: false, From: []string{"venmo@venmo.com"}, Subject: []string{}},
			"apple":  {Sync: true, From: []string{"no_reply@email.apple.com"}, Subject: []string{}},
		},
		EmailAccounts: []EmailAccount{
			{Email: "test@example.com", Password: "pass", IMAPServer: "imap.example.com", Mailboxes: []string{"INBOX"}},
		},
	}
}

func TestEnabledServices(t *testing.T) {
	cfg := baseConfig()
	got := cfg.EnabledServices()
	want := map[string]bool{"amazon": true, "paypal": true, "apple": true}
	if len(got) != 3 {
		t.Fatalf("expected 3 enabled services, got %v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected service %q in results", s)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("services not sorted: %v", got)
		}
	}
}

func TestFiltersForServices(t *testing.T) {
	cfg := baseConfig()

	t.Run("returns filters for enabled services", func(t *testing.T) {
		got := cfg.FiltersForServices([]string{"amazon", "paypal"})
		if _, ok := got["amazon"]; !ok {
			t.Error("expected amazon in filters")
		}
		if _, ok := got["paypal"]; !ok {
			t.Error("expected paypal in filters")
		}
	})

	t.Run("excludes disabled services", func(t *testing.T) {
		got := cfg.FiltersForServices([]string{"venmo"})
		if _, ok := got["venmo"]; ok {
			t.Error("venmo is disabled, should not appear in filters")
		}
	})

	t.Run("nil for empty input", func(t *testing.T) {
		got := cfg.FiltersForServices(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("excludes services with no filters", func(t *testing.T) {
		cfg2 := baseConfig()
		cfg2.Services["apple"] = ServiceConfig{Sync: true, From: []string{}, Subject: []string{}}
		got := cfg2.FiltersForServices([]string{"apple"})
		if _, ok := got["apple"]; ok {
			t.Error("apple has no filters, should be excluded")
		}
	})
}

func TestServicesForAccount(t *testing.T) {
	cfg := baseConfig()

	t.Run("no account services returns all enabled", func(t *testing.T) {
		acc := EmailAccount{Email: "a@b.com"}
		got := cfg.ServicesForAccount(acc)
		enabled := cfg.EnabledServices()
		if len(got) != len(enabled) {
			t.Errorf("got %v, want %v", got, enabled)
		}
	})

	t.Run("account services filtered to enabled only", func(t *testing.T) {
		acc := EmailAccount{Services: []string{"amazon", "venmo"}}
		got := cfg.ServicesForAccount(acc)
		if len(got) != 1 || got[0] != "amazon" {
			t.Errorf("expected [amazon], got %v", got)
		}
	})

	t.Run("empty service strings ignored", func(t *testing.T) {
		acc := EmailAccount{Services: []string{"amazon", ""}}
		got := cfg.ServicesForAccount(acc)
		if len(got) != 1 || got[0] != "amazon" {
			t.Errorf("expected [amazon], got %v", got)
		}
	})
}

func TestHasAnthropicKey(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		expected bool
	}{
		{"with key", "sk-ant-...", true},
		{"without key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AnthropicAPIKey: tt.apiKey}
			assert.Equal(t, tt.expected, cfg.HasAnthropicKey())
		})
	}
}
