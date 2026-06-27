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

// List returns unapproved transactions without a memo
func (c *Client) List(daysToLookBack int) ([]Transaction, error) {
	cfg := config.Get()
	sinceDate := cfg.SinceDateAsTime(daysToLookBack)

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
