package cmd

import (
	"fmt"
	"sort"
	"strings"

	"ynam/pkg/config"
	"ynam/pkg/spinner"
	"ynam/pkg/ynab"

	"github.com/spf13/cobra"
)

var (
	resetMemoDays   int
	resetMemoText   string
	resetMemoDryRun bool
)

// resetMemoCmd clears the memo on YNAB transactions whose memo exactly matches
// a given value (default "Amazon Order"), so a later sync can re-populate them
// with a proper item description.
var resetMemoCmd = &cobra.Command{
	Use:   "reset-memo",
	Short: `Clear memos that exactly match a value (default "Amazon Order")`,
	Long: `Find YNAB transactions whose memo exactly matches the given text and
clear them, so a subsequent sync can re-populate them with a real description.

By default it targets the generic "Amazon Order" fallback memo. Use --dry-run
to preview which transactions would be cleared without modifying YNAB.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()

		daysToLookBack := cfg.DaysSince
		if resetMemoDays > 0 {
			daysToLookBack = resetMemoDays
		}

		target := strings.TrimSpace(resetMemoText)
		if target == "" {
			return fmt.Errorf("--memo cannot be empty")
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

		// Collect transactions whose memo exactly matches the target.
		var matches []ynab.Transaction
		for _, txn := range transactions {
			if txn.Memo != nil && strings.TrimSpace(*txn.Memo) == target {
				matches = append(matches, txn)
			}
		}
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].Date.After(matches[j].Date)
		})

		if len(matches) == 0 {
			fmt.Printf("No transactions found with memo %q in the last %d days.\n", target, daysToLookBack)
			return nil
		}

		fmt.Printf("Transactions with memo %q (last %d days):\n\n", target, daysToLookBack)
		cleared := 0
		for _, txn := range matches {
			line := fmt.Sprintf("%-24s %-12s %10s",
				truncate(txn.Payee, 24),
				txn.Date.Format("2006-01-02"),
				txn.Amount.StringFixed(2),
			)
			if resetMemoDryRun {
				fmt.Printf("Would clear: %s\n", line)
				continue
			}
			if err := client.ClearMemo(txn.ID); err != nil {
				fmt.Printf("Failed:      %s  (%v)\n", line, err)
				continue
			}
			fmt.Printf("Cleared:     %s\n", line)
			cleared++
		}

		fmt.Println()
		if resetMemoDryRun {
			fmt.Printf("Dry run: %d transaction(s) would be cleared. No changes written.\n", len(matches))
		} else {
			fmt.Printf("Cleared %d of %d matching memo(s).\n", cleared, len(matches))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resetMemoCmd)
	resetMemoCmd.Flags().IntVar(&resetMemoDays, "days", 0, "number of days to look back (0 = use config value)")
	resetMemoCmd.Flags().StringVar(&resetMemoText, "memo", "Amazon Order", "exact memo text to match and clear")
	resetMemoCmd.Flags().BoolVar(&resetMemoDryRun, "dry-run", false, "preview matches without modifying YNAB")
}
