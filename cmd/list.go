/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"ynam/pkg/config"
	"ynam/pkg/spinner"
	"ynam/pkg/ynab"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
)

var (
	days        int
	listPayee   string
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List unapproved transactions without memos",
	Long: `Fetch all unapproved transactions from YNAB that don't have a memo yet.
Shows payee, date, and amount for each transaction.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get the loaded config
		cfg := config.Get()

		daysToLookBack := cfg.DaysSince
		if days > 0 {
			daysToLookBack = days
		}

		sp := spinner.New([]string{"YNAB"})
		sp.Start()

		client := ynab.NewClient(cfg.YNABAPIToken, cfg.YNABBudgetID)
		transactions, err := client.List(daysToLookBack)
		if err != nil {
			sp.Finish(0, "", err)
			sp.Stop()
			return fmt.Errorf("failed to fetch transactions: %w", err)
		}
		sp.Finish(0, fmt.Sprintf("%d transactions", len(transactions)), nil)
		sp.Stop()

		sort.Slice(transactions, func(i, j int) bool {
			return transactions[i].Date.After(transactions[j].Date)
		})

		// Apply optional payee filter.
		if listPayee != "" {
			filter := strings.ToLower(listPayee)
			var filtered []ynab.Transaction
			for _, txn := range transactions {
				if strings.Contains(strings.ToLower(txn.Payee), filter) {
					filtered = append(filtered, txn)
				}
			}
			transactions = filtered
		}

		// Display results
		if len(transactions) == 0 {
			fmt.Println("No transactions found.")
			return nil
		}

		fmt.Printf("Found %d transactions\n\n", len(transactions))
		fmt.Printf("%-40s %-12s %-12s  %s\n", "Payee", "Date", "Amount", "Memo")
		fmt.Println("--------------------------------------------------------------------------------")

		totalAmount := decimal.NewFromInt(0)
		for _, txn := range transactions {
			memo := ""
			if txn.Memo != nil {
				memo = *txn.Memo
			}
			fmt.Printf("%-40s %-12s %-12s  %s\n",
				truncate(txn.Payee, 40),
				txn.Date.Format("2006-01-02"),
				txn.Amount.StringFixed(2),
				memo,
			)
			totalAmount = totalAmount.Add(txn.Amount)
		}

		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Printf("%-40s %-12s %-12s\n", "TOTAL", "", totalAmount.StringFixed(2))

		// Show config info
		fmt.Printf("\nLookback period: %d days (since %s)\n",
			daysToLookBack,
			cfg.SinceDateAsTime(daysToLookBack).Format("2006-01-02"),
		)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().IntVar(&days, "days", 0, "number of days to look back (0 = use config value)")
	listCmd.Flags().StringVar(&listPayee, "payee", "", "filter by payee name (case-insensitive substring match)")
}

// truncate limits a string to max length with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
