package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ynam/pkg/config"
	"ynam/pkg/email"
	"ynam/pkg/email/imap"
	"ynam/pkg/spinner"

	_ "ynam/pkg/email/amazon"
	_ "ynam/pkg/email/apple"
	_ "ynam/pkg/email/audible"
	_ "ynam/pkg/email/paypal"
	_ "ynam/pkg/email/venmo"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
)

var (
	searchDays      int
	searchUIDsOnly  bool
	searchAmount    string
	searchSaveDir   string
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search configured email accounts for payment emails",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()

		daysToLookBack := cfg.DaysSince
		if searchDays > 0 {
			daysToLookBack = searchDays
		}
		since := cfg.SinceDateAsTime(daysToLookBack)

		fmt.Printf("Searching since %s (%d day lookback)\n\n",
			since.Format("2006-01-02"), daysToLookBack)

		// Parse optional amount filter.
		var filterAmount *decimal.Decimal
		if searchAmount != "" {
			d, err := decimal.NewFromString(searchAmount)
			if err != nil {
				return fmt.Errorf("invalid --amount %q: %w", searchAmount, err)
			}
			filterAmount = &d
		}

		accounts := cfg.GetEmailAccounts()
		labels := make([]string, len(accounts))
		for i, a := range accounts {
			labels[i] = a.Email
		}

		sp := spinner.New(labels)
		sp.Start()

		// Use FetchAllWithBodies when we need raw email content for saving.
		needBodies := filterAmount != nil || searchSaveDir != ""

		if !needBodies {
			// Fast path: normal parsed-only fetch.
			results := imap.FetchAll(cmd.Context(), cfg, daysToLookBack, func(idx int, r imap.FetchResult) {
				if r.Err != nil {
					sp.Finish(idx, "", r.Err)
					return
				}
				detail := fmt.Sprintf("%d matched, %d parsed", r.MatchedCount, len(r.Transactions))
				sp.Finish(idx, detail, nil)
			})

			sp.Stop()

			grandTotal := 0
			for _, result := range results {
				fmt.Printf("%s\n", result.Account)
				fmt.Println("----------------------------------------")

				if result.Err != nil {
					fmt.Printf("  error: %v\n\n", result.Err)
					continue
				}

				fmt.Printf("  matched %d message(s)\n", result.MatchedCount)

				if len(result.Transactions) == 0 {
					fmt.Print("  no transactions parsed from matched messages\n\n")
					continue
				}

				txns := result.Transactions
				sortByDateDesc(txns)
				fmt.Printf("  parsed %d transaction(s):\n", len(txns))
				for _, txn := range txns {
					fmt.Printf("    %-10s %-8s %-24s %12s  %s\n",
						formatTxnDate(txn.Date),
						txn.Service,
						truncate(txn.Payee, 24),
						txn.Amount.StringFixed(2),
						txn.Memo,
					)
				}
				grandTotal += len(txns)
				fmt.Println()
			}

			fmt.Printf("Done. %d transaction(s) parsed across all accounts.\n", grandTotal)
			return nil
		}

		// Slow path: fetch with bodies for amount filtering / saving.
		results := imap.FetchAllWithBodies(cmd.Context(), cfg, daysToLookBack, func(idx int, r imap.RawFetchResult) {
			if r.Err != nil {
				sp.Finish(idx, "", r.Err)
				return
			}
			txnCount := 0
			for _, m := range r.Messages {
				txnCount += len(m.Transactions)
			}
			detail := fmt.Sprintf("%d matched, %d parsed", r.MatchedCount, txnCount)
			sp.Finish(idx, detail, nil)
		})

		sp.Stop()

		saved := 0
		grandTotal := 0
		for _, result := range results {
			fmt.Printf("%s\n", result.Account)
			fmt.Println("----------------------------------------")

			if result.Err != nil {
				fmt.Printf("  error: %v\n\n", result.Err)
				continue
			}

			fmt.Printf("  matched %d message(s)\n", result.MatchedCount)

			if len(result.Messages) == 0 {
				fmt.Print("  no transactions parsed from matched messages\n\n")
				continue
			}

			var allTxns []email.Transaction
			for _, m := range result.Messages {
				allTxns = append(allTxns, m.Transactions...)
			}
			sortByDateDesc(allTxns)

			for _, m := range result.Messages {
				for _, txn := range m.Transactions {
					if filterAmount != nil && !txn.Amount.Abs().Equal(filterAmount.Abs()) {
						continue
					}

					fmt.Printf("    %-10s %-8s %-24s %12s  %s\n",
						formatTxnDate(txn.Date),
						txn.Service,
						truncate(txn.Payee, 24),
						txn.Amount.StringFixed(2),
						txn.Memo,
					)
					grandTotal++

					if searchSaveDir != "" {
						filename := fmt.Sprintf("%s_%s_%s.eml",
							strings.ToLower(txn.Service),
							txn.Amount.StringFixed(2),
							formatTxnDate(txn.Date),
						)
						outPath := filepath.Join(searchSaveDir, filename)
						if err := os.WriteFile(outPath, []byte(m.RawBody), 0644); err != nil {
							fmt.Printf("      warning: could not save %s: %v\n", outPath, err)
						} else {
							fmt.Printf("      saved -> %s\n", outPath)
							saved++
						}
					}
				}
			}
			fmt.Println()
		}

		fmt.Printf("Done. %d transaction(s) matched", grandTotal)
		if saved > 0 {
			fmt.Printf(", %d email(s) saved to %s", saved, searchSaveDir)
		}
		fmt.Println(".")
		return nil
	},
}

// parseTxnDate parses the date formats that parsers produce.
func parseTxnDate(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02",
		"January 2, 2006",
		"Jan 2, 2006",
		"2 January 2006",
		"2 Jan 2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func formatTxnDate(s string) string {
	if t := parseTxnDate(s); !t.IsZero() {
		return t.Format("2006-01-02")
	}
	if s != "" {
		return s
	}
	return "unknown"
}

func sortByDateDesc(txns []email.Transaction) {
	sort.SliceStable(txns, func(i, j int) bool {
		return parseTxnDate(txns[i].Date).After(parseTxnDate(txns[j].Date))
	})
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntVar(&searchDays, "days", 0, "number of days to look back (0 = use config value)")
	searchCmd.Flags().BoolVar(&searchUIDsOnly, "uids-only", false, "only run the search; do not fetch/parse bodies")
	searchCmd.Flags().StringVar(&searchAmount, "amount", "", "filter to transactions matching this amount (e.g. 83.26)")
	searchCmd.Flags().StringVar(&searchSaveDir, "save-testdata", "", "directory to save matching raw emails into (e.g. testdata/)")
}
