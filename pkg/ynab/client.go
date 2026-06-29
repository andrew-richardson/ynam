package ynab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"ynam/pkg/config"

	"github.com/shopspring/decimal"
)

// Transaction represents a YNAB transaction
type Transaction struct {
	ID      string
	Payee   string
	Date    time.Time
	Amount  decimal.Decimal
	Memo    *string
	Cleared bool
}

// Client manages YNAB API interactions
type Client struct {
	apiToken   string
	budgetID   string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new YNAB API client
func NewClient(apiToken, budgetID string) *Client {
	return &Client{
		apiToken:   apiToken,
		budgetID:   budgetID,
		baseURL:    "https://api.youneedabudget.com/v1",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// List returns transactions on or after `daysToLookBack` days ago.
func (c *Client) List(daysToLookBack int) ([]Transaction, error) {
	return c.listSince(config.Get().SinceDateAsTime(daysToLookBack))
}

// ListRange returns transactions with a date in [since, until]. A zero `until`
// means no upper bound. The YNAB API only supports a lower bound (since_date),
// so the upper bound is applied client-side (inclusive of the until day).
func (c *Client) ListRange(since, until time.Time) ([]Transaction, error) {
	txns, err := c.listSince(since)
	if err != nil {
		return nil, err
	}
	if until.IsZero() {
		return txns, nil
	}
	var out []Transaction
	for _, t := range txns {
		if !t.Date.After(until) {
			out = append(out, t)
		}
	}
	return out, nil
}

// listSince fetches all transactions on or after the given date from YNAB.
func (c *Client) listSince(sinceDate time.Time) ([]Transaction, error) {
	// Construct YNAB API URL
	url := fmt.Sprintf("%s/budgets/%s/transactions?since_date=%s",
		c.baseURL, c.budgetID, sinceDate.Format("2006-01-02"))

	// Create request with bearer token
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiToken))

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("YNAB rate limit hit (429): ~200 requests/hour exceeded — wait up to an hour and retry")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YNAB API returned status %d", resp.StatusCode)
	}

	// Parse response
	var apiResp struct {
		Data struct {
			Transactions []struct {
				ID      string  `json:"id"`
				Payee   string  `json:"payee_name"`
				Date    string  `json:"date"`
				Amount  int64   `json:"amount"` // in milliunits
				Memo    *string `json:"memo"`
				Cleared string  `json:"cleared"`
			} `json:"transactions"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// filter and convert transactions
	var transactions []Transaction
	for _, t := range apiResp.Data.Transactions {
		// Parse date
		date, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			continue
		}

		// Convert amount from milliunits to decimal
		amount := decimal.NewFromInt(t.Amount).Div(decimal.NewFromInt(1000))

		transactions = append(transactions, Transaction{
			ID:      t.ID,
			Payee:   t.Payee,
			Date:    date,
			Amount:  amount,
			Memo:    t.Memo,
			Cleared: t.Cleared == "cleared",
		})
	}

	return transactions, nil
}

// ClearMemo sets a transaction's memo to empty string.
func (c *Client) ClearMemo(transactionID string) error {
	return c.patch(transactionID, "")
}

// Update updates a transaction's memo
func (c *Client) Update(transactionID, memo string) error {
	if transactionID == "" {
		return fmt.Errorf("transaction ID cannot be empty")
	}

	if memo == "" {
		return fmt.Errorf("memo cannot be empty")
	}

	return c.patch(transactionID, memo)
}

// BulkUpdate updates the memo on many transactions using YNAB's bulk endpoint,
// which accepts many transactions in a single request. This collapses what would
// be one API call per transaction into a handful, staying well under YNAB's
// ~200-requests/hour limit (the cause of HTTP 429 errors). The map is keyed by
// transaction ID; an empty memo value clears the memo. Requests are chunked.
func (c *Client) BulkUpdate(updates map[string]string) (int, error) {
	type txnPatch struct {
		ID   string `json:"id"`
		Memo string `json:"memo"`
	}
	all := make([]txnPatch, 0, len(updates))
	for id, memo := range updates {
		if id == "" {
			continue
		}
		all = append(all, txnPatch{ID: id, Memo: memo})
	}

	const chunkSize = 100
	updated := 0
	for i := 0; i < len(all); i += chunkSize {
		end := i + chunkSize
		if end > len(all) {
			end = len(all)
		}
		if err := c.bulkPatch(all[i:end]); err != nil {
			return updated, err
		}
		updated += end - i
	}
	return updated, nil
}

func (c *Client) bulkPatch(txns interface{}) error {
	url := fmt.Sprintf("%s/budgets/%s/transactions", c.baseURL, c.budgetID)
	payload := map[string]interface{}{"transactions": txns}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("YNAB rate limit hit (429): ~200 requests/hour exceeded — wait up to an hour and retry")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("YNAB API returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) patch(transactionID, memo string) error {
	url := fmt.Sprintf("%s/budgets/%s/transactions/%s",
		c.baseURL, c.budgetID, transactionID)

	payload := struct {
		Transaction struct {
			Memo string `json:"memo"`
		} `json:"transaction"`
	}{}
	payload.Transaction.Memo = memo

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("YNAB API returned status %d", resp.StatusCode)
	}

	return nil
}
