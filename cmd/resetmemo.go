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
	resetMemoFrom   string
	resetMemoTo     string
	resetMemoText   string
	resetMemoPayee  string
	resetMemoAll    bool
	resetMemoDryRun bool
)

// resetMemoCmd clears memos on YNAB transactions so a later sync can re-populate
// them. By default it targets the generic "Amazon Order" fallback memo; with
// --all it clears every non-empty memo, optionally scoped to a --payee.
var resetMemoCmd = &cobra.Command{
	Use:   "reset-memo",
	Short: `Clear YNAB memos (exact match, or --all for any) so they can re-sync`,
	Long: `Clear the memo on matching YNAB transactions so a subsequent sync can
re-populate them with a fresh description.

Matching:
  - By default, clears memos that CONTAIN --memo (default "Amazon Order"),
    case-insensitive.
  - With --all, clears every transaction that has a non-empty memo.
  - --payee restricts to transactions whose payee contains the given text.

Examples:
  ynam reset-memo --days 120                       # clear "Amazon Order" memos
  ynam reset-memo --all --payee amazon --days 120  # clear ALL Amazon memos
  ynam reset-memo --all --payee amazon --days 120 --dry-run  # preview first

Use --dry-run to preview without modifying YNAB.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()

		since, until, err := resolveRange(cfg, resetMemoDays, resetMemoFrom, resetMemoTo)
		if err != nil {
			return err
		}

		target := strings.TrimSpace(resetMemoText)
		if !resetMemoAll && target == "" {
			return fmt.Errorf("--memo cannot be empty (or pass --all to clear any memo)")
		}

		sp := spinner.New([]string{"YNAB"})
		sp.Start()

		client := ynab.NewClient(cfg.YNABAPIToken, cfg.YNABBudgetID)
		transactions, err := client.ListRange(since, until)
		if err != nil {
			sp.Finish(0, "", err)
			sp.Stop()
			return fmt.Errorf("failed to fetch transactions: %w", err)
		}
		sp.Finish(0, fmt.Sprintf("%d transactions", len(transactions)), nil)
		sp.Stop()

		payeeFilter := strings.ToLower(strings.TrimSpace(resetMemoPayee))

		// Collect matching transactions: must have a non-empty memo, match the
		// payee filter (if any), and either equal --memo or, with --all, be any memo.
		var matches []ynab.Transaction
		for _, txn := range transactions {
			if txn.Memo == nil || strings.TrimSpace(*txn.Memo) == "" {
				continue
			}
			if payeeFilter != "" && !strings.Contains(strings.ToLower(txn.Payee), payeeFilter) {
				continue
			}
			if !resetMemoAll && !strings.Contains(
				strings.ToLower(strings.TrimSpace(*txn.Memo)),
				strings.ToLower(target),
			) {
				continue
			}
			matches = append(matches, txn)
		}
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].Date.After(matches[j].Date)
		})

		// Describe the selection for the header / empty message.
		scope := fmt.Sprintf("memo containing %q", target)
		if resetMemoAll {
			scope = "any non-empty memo"
		}
		if payeeFilter != "" {
			scope += fmt.Sprintf(" for payee containing %q", resetMemoPayee)
		}

		if len(matches) == 0 {
			fmt.Printf("No transactions found with %s (%s).\n", scope, rangeLabel(since, until))
			return nil
		}

		fmt.Printf("Transactions with %s (%s):\n\n", scope, rangeLabel(since, until))
		clears := make(map[string]string, len(matches))
		for _, txn := range matches {
			memo := ""
			if txn.Memo != nil {
				memo = *txn.Memo
			}
			prefix := "Would clear:"
			if !resetMemoDryRun {
				prefix = "Clearing:   "
				clears[txn.ID] = "" // empty memo clears it
			}
			fmt.Printf("%s %-24s %-12s %10s  %s\n",
				prefix,
				truncate(txn.Payee, 24),
				txn.Date.Format("2006-01-02"),
				txn.Amount.StringFixed(2),
				truncate(memo, 40),
			)
		}

		fmt.Println()
		if resetMemoDryRun {
			fmt.Printf("Dry run: %d transaction(s) would be cleared. No changes written.\n", len(matches))
			return nil
		}

		// One bulk request per 100 transactions, instead of one per transaction,
		// to stay under YNAB's ~200 requests/hour limit.
		cleared, err := client.BulkUpdate(clears)
		if err != nil {
			return fmt.Errorf("cleared %d of %d before failing: %w", cleared, len(matches), err)
		}
		fmt.Printf("Cleared %d memo(s) in YNAB.\n", cleared)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resetMemoCmd)
	resetMemoCmd.Flags().IntVar(&resetMemoDays, "days", 0, "number of days to look back (0 = use config value; ignored if --from is set)")
	resetMemoCmd.Flags().StringVar(&resetMemoFrom, "from", "", "start date YYYY-MM-DD (overrides --days)")
	resetMemoCmd.Flags().StringVar(&resetMemoTo, "to", "", "end date YYYY-MM-DD inclusive (default: now)")
	resetMemoCmd.Flags().StringVar(&resetMemoText, "memo", "Amazon Order", "clear memos containing this text, case-insensitive (ignored with --all)")
	resetMemoCmd.Flags().StringVar(&resetMemoPayee, "payee", "", "restrict to payees containing this text (case-insensitive)")
	resetMemoCmd.Flags().BoolVar(&resetMemoAll, "all", false, "clear every non-empty memo (not just exact --memo matches)")
	resetMemoCmd.Flags().BoolVar(&resetMemoDryRun, "dry-run", false, "preview matches without modifying YNAB")
}
