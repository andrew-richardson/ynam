# Configuration Integration Example

This document shows how the config package integrates across the application.

## Flow

```
cmd/root.go
    ↓
config.Load() → validates → config.Get() available globally
    ↓
cmd/list.go
    ↓
config.Get() → creates ynab.NewClient()
    ↓
pkg/ynab/client.go uses config
```

## Step 1: Application Startup (cmd/root.go)

When the CLI starts, `initConfig()` runs first:

```go
func initConfig() {
    configPath := cfgFile
    if configPath == "" {
        home, _ := os.UserHomeDir()
        configPath = home + "/.ynam.yaml"
    }

    cfg, err := config.Load(configPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
        os.Exit(1)
    }

    fmt.Fprintf(os.Stderr, "Using config file: %s\n", configPath)
}
```

**Result:** Config is loaded once, validated, and available globally.

## Step 2: Command Execution (cmd/list.go)

Any command can access the config:

```go
var listCmd = &cobra.Command{
    Use: "list",
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg := config.Get()  // Get the config loaded in Step 1
        
        // Create client using config values
        client, err := ynab.NewClient()
        
        // Fetch transactions
        txns, err := client.List()
        
        // Use config to determine display behavior
        if cfg.HasAnthropicKey() {
            // Use AI summarization
        }
        
        return nil
    },
}
```

**Key point:** No need to pass config around—it's globally accessible.

## Step 3: Package Usage (pkg/ynab/client.go)

Any package can use the config when initialized:

```go
// pkg/ynab/client.go
package ynab

import "ynam/pkg/config"

func NewClient() (*Client, error) {
    cfg := config.Get()  // Access global config
    
    return &Client{
        apiToken: cfg.YNABAPIToken,
        budgetID: cfg.YNABBudgetID,
        baseURL:  "https://api.youneedabudget.com/v1",
    }, nil
}

func (c *Client) List() ([]Transaction, error) {
    cfg := config.Get()
    sinceDate := cfg.SinceDateAsTime()  // Helper method
    
    // Fetch transactions since that date
    // ...
}
```

## Configuration File (.ynam.yaml)

```yaml
ynab_api_token: "NI8Qgsow8ncJKb0posudi0ccTRcpFtvHyt2N-Kn9lDA"
ynab_budget_id: "078d6a9f-ef41-4d1b-b32b-2be856fcc2f8"
days_since: 10
sync:
  venmo: true
  paypal: true
  apple: true
  amazon: true
anthropic_api_key: "sk-ant-api03-a0L3LPl-..."
email_accounts:
  - email: "arichardson2189@gmail.com"
    password: "beninfugihqvvnbp"
    imap_server: "imap.gmail.com"
  - email: "dawnwolff@me.com"
    password: "msxmxgzohgnqdoro"
    imap_server: "imap.mail.me.com"
```

## Usage Examples

### Example 1: List Command

```bash
$ ynam list
```

**What happens:**
1. `initConfig()` loads `.ynam.yaml` from home directory
2. `listCmd` runs, calls `config.Get()`
3. Creates `ynab.NewClient()` which uses config values
4. Fetches unapproved transactions since `days_since` ago
5. Displays results in a table

### Example 2: Custom Config Path

```bash
$ ynam --config /tmp/custom.yaml list
```

**What happens:**
1. `cfgFile` flag is set to `/tmp/custom.yaml`
2. `initConfig()` loads from that path
3. Rest is the same as Example 1

### Example 3: Using Service Sync Settings

```go
// In any package
cfg := config.Get()

if cfg.IsSyncEnabled("amazon") {
    // Process Amazon emails
}

if cfg.IsSyncEnabled("venmo") {
    // Process Venmo emails
}

for _, account := range cfg.GetEmailAccounts() {
    // Connect to each email account
}
```

## Adding a New Package

To add a new feature that uses config:

1. **Create package** (e.g., `pkg/email/parser.go`)
2. **Import config**:
   ```go
   import "ynam/pkg/config"
   ```
3. **Access config in functions**:
   ```go
   func ParseEmails() error {
       cfg := config.Get()
       for _, account := range cfg.GetEmailAccounts() {
           // Connect to account
       }
   }
   ```
4. **Call from command** (e.g., `cmd/sync.go`)
5. **Done!** Config is already loaded and available.

## Error Handling

Config validation happens once at startup, preventing invalid state:

```
Missing ynab_api_token
↓
config.Load() returns error
↓
cmd/root.go prints error and exits
↓
Application never starts with bad config
```

This is better than having runtime errors deep in your code.

## Testing

Test with different configs:

```go
func TestWithConfig(t *testing.T) {
    // Create temp config file
    tmpFile, _ := os.CreateTemp("", "test_*.yaml")
    tmpFile.WriteString(`
ynab_api_token: "test_token"
ynab_budget_id: "test_budget"
days_since: 7
email_accounts:
  - email: "test@example.com"
    password: "pass"
    imap_server: "imap.example.com"
`)
    tmpFile.Close()

    // Load config
    cfg, _ := config.Load(tmpFile.Name())

    // Test with config
    client := ynab.NewClient()
    // ...
}
```
