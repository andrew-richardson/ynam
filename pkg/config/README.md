# Config Package

Configuration utility for loading and accessing application settings from `.ynam.yaml`.

## Usage

### Loading Configuration

Call `config.Load()` once during application startup (handled by `cmd/root.go`):

```go
cfg, err := config.Load("/path/to/.ynam.yaml")
if err != nil {
    log.Fatal(err)
}
```

### Accessing Configuration

Other packages access the global config instance using `config.Get()`:

```go
package ynab

import "ynam/pkg/config"

func NewClient() *Client {
    cfg := config.Get()
    return &Client{
        APIToken: cfg.YNABAPIToken,
        BudgetID: cfg.YNABBudgetID,
    }
}
```

## Configuration Structure

```yaml
ynab_api_token: "your_api_token"
ynab_budget_id: "your_budget_id"
days_since: 10
sync:
  venmo: true
  paypal: true
  apple: true
  amazon: true
anthropic_api_key: "optional_api_key"
email_accounts:
  - email: "user@gmail.com"
    password: "app_password"
    imap_server: "imap.gmail.com"
```

## API Reference

### Config Struct

- `YNABAPIToken` - YNAB API authentication token (required)
- `YNABBudgetID` - YNAB budget ID (required)
- `DaysSince` - Number of days to look back (default: 7)
- `Sync` - Service sync configuration
- `AnthropicAPIKey` - Optional API key for AI features
- `EmailAccounts` - List of email accounts to sync

### Helper Methods

#### Get()
```go
cfg := config.Get()  // Returns global config instance
```

#### IsSyncEnabled(service string)
```go
if cfg.IsSyncEnabled("amazon") {
    // Process Amazon transactions
}
```

Supported services: `"amazon"`, `"venmo"`, `"paypal"`, `"apple"`

#### HasAnthropicKey()
```go
if cfg.HasAnthropicKey() {
    // Use AI summarization
}
```

#### SinceDateAsTime()
```go
sinceDate := cfg.SinceDateAsTime()  // Returns time.Time for lookback period
```

#### GetEmailAccounts()
```go
accounts := cfg.GetEmailAccounts()
for _, acc := range accounts {
    // Process email account
}
```

## Validation

Configuration is automatically validated on load. Required fields:
- `ynab_api_token`
- `ynab_budget_id`
- At least one email account with `email`, `password`, and `imap_server`

All validation happens before the config is returned, preventing invalid configurations from being used.

## Error Handling

Configuration errors are surfaced immediately:
- Missing required fields
- Negative `days_since` value
- Missing email account details

Example error handling:

```go
cfg, err := config.Load(".ynam.yaml")
if err != nil {
    fmt.Fprintf(os.Stderr, "Invalid config: %v\n", err)
    os.Exit(1)
}
```
