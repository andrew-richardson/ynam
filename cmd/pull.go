/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"ynam/pkg/config"
	"ynam/pkg/email/imap"

	// Register email parsers so ParseWithAll can use them.
	_ "ynam/pkg/email/amazon"
	_ "ynam/pkg/email/apple"
	_ "ynam/pkg/email/audible"
	_ "ynam/pkg/email/paypal"
	_ "ynam/pkg/email/venmo"

	"github.com/spf13/cobra"
)

var pullDays int

// pullCmd fetches emails from all configured accounts and prints the
// transactions parsed from them.
var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Fetch and parse transactions from configured email accounts",
	Long: `Connect to each configured email account over IMAP, fetch messages
within the lookback period, and parse any payment transactions found
(Amazon, PayPal, Venmo, Apple).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()

		daysToLookBack := cfg.DaysSince
		if pullDays > 0 {
			daysToLookBack = pullDays
		}

		results := imap.FetchAll(cmd.Context(), cfg, daysToLookBack, nil)

		total := 0
		for _, result := range results {
			fmt.Printf("\nAccount: %s\n", result.Account)
			if result.Err != nil {
				fmt.Printf("  error: %v\n", result.Err)
				continue
			}

			if len(result.Transactions) == 0 {
				fmt.Println("  no transactions found")
				continue
			}

			for _, txn := range result.Transactions {
				fmt.Printf("  [%s] %-20s %10s  %s\n",
					txn.Service,
					truncate(txn.Payee, 20),
					txn.Amount.StringFixed(2),
					txn.Memo,
				)
				total++
			}
		}

		fmt.Printf("\nTotal transactions parsed: %d\n", total)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullCmd)
	pullCmd.Flags().IntVar(&pullDays, "days", 0, "number of days to look back (0 = use config value)")
}
