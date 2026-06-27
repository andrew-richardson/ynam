# Config Package - Quick Reference

## One-Liner: How to Use

```go
cfg := config.Get()  // That's it. Config is loaded at app startup.
```

## Common Patterns

### 1. Access Single Value

```go
cfg := config.Get()
token := cfg.YNABAPIToken
budgetID := cfg.YNABBudgetID
```

### 2. Check if Service is Enabled

```go
cfg := config.Get()
if cfg.IsSyncEnabled("amazon") {
    // Process Amazon
}
```

### 3. Get All Email Accounts

```go
cfg := config.Get()
for _, account := range cfg.GetEmailAccounts() {
    email := account.Email
    password := account.Password
    server := account.IMAPServer
}
```

### 4. Get Lookback Date

```go
cfg := config.Get()
sinceDate := cfg.SinceDateAsTime()
// Use sinceDate for API queries
```

### 5. Check for AI Feature

```go
cfg := config.Get()
if cfg.HasAnthropicKey() {
    // Use AI summarization
}
```

## Type Reference

```go
type Config struct {
    YNABAPIToken    string         // YNAB API token
    YNABBudgetID    string         // YNAB budget ID
    DaysSince       int            // Lookback days
    Sync            SyncConfig     // Service toggles
    AnthropicAPIKey string         // Optional AI key
    EmailAccounts   []EmailAccount // Email accounts
}

type SyncConfig struct {
    Venmo  bool  // Sync Venmo?
    PayPal bool  // Sync PayPal?
    Apple  bool  // Sync Apple?
    Amazon bool  // Sync Amazon?
}

type EmailAccount struct {
    Email      string // Email address
    Password   string // App password
    IMAPServer string // IMAP server URL
}
```

## Error Handling

Config errors happen at startup (in `cmd/root.go`), not during your code:

```go
// ✓ GOOD: Config is guaranteed to be valid
cfg := config.Get()
token := cfg.YNABAPIToken  // Never nil/empty

// ✗ BAD: Don't check like this
if cfg.YNABAPIToken == "" {  // This will never be true
    // ...
}
```

## Testing

```go
func TestMyFeature(t *testing.T) {
    // Create test config
    cfg := &config.Config{
        YNABAPIToken: "test",
        YNABBudgetID: "test",
        DaysSince: 7,
        EmailAccounts: []config.EmailAccount{
            {Email: "test@example.com", Password: "pass", IMAPServer: "imap"},
        },
    }
    
    // Validate config
    err := cfg.Validate()
    assert.NoError(t, err)
}
```

## Don't Forget

- Config is **global and thread-safe** (loaded once at startup)
- Call `config.Load()` in `cmd/root.go` (already done)
- All other packages just use `config.Get()`
- No need to pass config as function parameter
- Config is validated before use—no runtime surprises

## FAQ

**Q: What if I need config in my test?**
A: Create a `Config` struct directly, call `Validate()`, then use it.

**Q: Can I reload config at runtime?**
A: Current design loads once. For runtime reloading, modify `Load()` to support it (not needed yet).

**Q: What if config is missing?**
A: `config.Get()` panics if `Load()` wasn't called. This is intentional—force startup validation.

**Q: Can I override values with env vars?**
A: Use Viper's `BindEnv()` in `Load()` if needed. Currently not implemented.

**Q: Should I validate again in my package?**
A: No. Config is validated once at startup. Your code can assume it's valid.
