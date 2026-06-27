package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	YNABAPIToken    string                   `mapstructure:"ynab_api_token"`
	YNABBudgetID    string                   `mapstructure:"ynab_budget_id"`
	DaysSince       int                      `mapstructure:"days_since"`
	Services        map[string]ServiceConfig `mapstructure:"services"`
	AnthropicAPIKey string                   `mapstructure:"anthropic_api_key"`
	EmailAccounts   []EmailAccount           `mapstructure:"email_accounts"`
}

// ServiceConfig defines a single payment service: whether it is enabled and the
// IMAP search filters used to find its emails.
type ServiceConfig struct {
	Sync            bool     `mapstructure:"sync"`
	From            []string `mapstructure:"from"`
	Subject         []string `mapstructure:"subject"`
	PayeeKeywords   []string `mapstructure:"payee_keywords"`
	DateMatchWindow int      `mapstructure:"date_match_window"`
}

// EmailAccount represents an email account configuration
type EmailAccount struct {
	Email      string   `mapstructure:"email"`
	Password   string   `mapstructure:"password"`
	IMAPServer string   `mapstructure:"imap_server"`
	Services   []string `mapstructure:"services"`
	Mailboxes  []string `mapstructure:"mailboxes"`
}

var globalConfig *Config

// Load loads configuration from a file
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("days_since", 7)

	// Load from file
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal into struct
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	globalConfig = cfg
	return cfg, nil
}

// Get returns the global config instance (populated by Load)
func Get() *Config {
	if globalConfig == nil {
		panic("config not loaded: call config.Load() first")
	}
	return globalConfig
}

// Validate checks required fields and constraints
func (c *Config) Validate() error {
	if c.YNABAPIToken == "" {
		return fmt.Errorf("ynab_api_token is required")
	}
	if c.YNABBudgetID == "" {
		return fmt.Errorf("ynab_budget_id is required")
	}
	if c.DaysSince < 0 {
		return fmt.Errorf("days_since must be non-negative")
	}
	if len(c.EmailAccounts) == 0 {
		return fmt.Errorf("at least one email account is required")
	}

	// Validate each email account
	for i, acc := range c.EmailAccounts {
		if acc.Email == "" {
			return fmt.Errorf("email_accounts[%d].email is required", i)
		}
		if acc.Password == "" {
			return fmt.Errorf("email_accounts[%d].password is required", i)
		}
		if acc.IMAPServer == "" {
			return fmt.Errorf("email_accounts[%d].imap_server is required", i)
		}
		if len(acc.Mailboxes) == 0 {
			return fmt.Errorf("email_accounts[%d] (%s).mailboxes is required (e.g. [INBOX])", i, acc.Email)
		}
		for j, mb := range acc.Mailboxes {
			if strings.TrimSpace(mb) == "" {
				return fmt.Errorf("email_accounts[%d].mailboxes[%d] cannot be empty", i, j)
			}
		}
		for _, service := range acc.Services {
			if !c.isValidService(service) {
				return fmt.Errorf("email_accounts[%d].services contains invalid service %q", i, service)
			}
		}
	}

	for name, svc := range c.Services {
		if !c.isValidService(name) {
			return fmt.Errorf("services contains invalid service %q", name)
		}
		for i, from := range svc.From {
			if strings.TrimSpace(from) == "" {
				return fmt.Errorf("services[%s].from[%d] cannot be empty", name, i)
			}
		}
		for i, subject := range svc.Subject {
			if strings.TrimSpace(subject) == "" {
				return fmt.Errorf("services[%s].subject[%d] cannot be empty", name, i)
			}
		}
	}

	return nil
}

// SinceDateAsTime returns a time.Time that is daysToLookBack days in the past.
// When daysToLookBack is 0, it falls back to the configured days_since value.
func (c *Config) SinceDateAsTime(daysToLookBack int) time.Time {
	days := daysToLookBack
	if days <= 0 {
		days = c.DaysSince
	}
	return time.Now().AddDate(0, 0, -days)
}

// GetEmailAccounts returns all email accounts for a specific service
// Useful for iterating over accounts to fetch transactions
func (c *Config) GetEmailAccounts() []EmailAccount {
	return c.EmailAccounts
}

// IsSyncEnabled reports whether a service is defined and has sync enabled.
func (c *Config) IsSyncEnabled(service string) bool {
	svc, ok := c.Services[strings.ToLower(strings.TrimSpace(service))]
	return ok && svc.Sync
}

// EnabledServices returns the names of all services with sync enabled.
func (c *Config) EnabledServices() []string {
	services := make([]string, 0, len(c.Services))
	for name, svc := range c.Services {
		if svc.Sync {
			services = append(services, name)
		}
	}
	sort.Strings(services)
	return services
}

// FiltersForServices returns the IMAP search filters for the given service
// names, limited to services that are enabled and have at least one filter.
func (c *Config) FiltersForServices(services []string) map[string]ServiceConfig {
	if len(services) == 0 {
		return nil
	}

	filters := make(map[string]ServiceConfig, len(services))
	for _, name := range services {
		name = strings.ToLower(strings.TrimSpace(name))
		svc, ok := c.Services[name]
		if !ok || !svc.Sync {
			continue
		}
		if len(svc.From) == 0 && len(svc.Subject) == 0 {
			continue
		}
		filters[name] = svc
	}
	return filters
}

// ServicesForAccount returns which enabled services should be parsed for the
// given email account. If the account defines no services, it is eligible for
// all globally enabled services.
func (c *Config) ServicesForAccount(account EmailAccount) []string {
	if len(account.Services) == 0 {
		return c.EnabledServices()
	}

	enabled := c.EnabledServices()
	active := make([]string, 0, len(account.Services))
	for _, service := range account.Services {
		service = strings.ToLower(strings.TrimSpace(service))
		if service == "" {
			continue
		}
		if !c.IsSyncEnabled(service) {
			continue
		}
		for _, candidate := range enabled {
			if candidate == service {
				active = append(active, service)
				break
			}
		}
	}
	return active
}

func (c *Config) isValidService(service string) bool {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "amazon", "venmo", "paypal", "apple", "audible":
		return true
	default:
		return false
	}
}

// HasAnthropicKey returns true if anthropic API key is configured
func (c *Config) HasAnthropicKey() bool {
	return c.AnthropicAPIKey != ""
}
