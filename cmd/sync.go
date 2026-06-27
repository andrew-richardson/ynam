package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ynam/pkg/claude"
	"ynam/pkg/config"
	"ynam/pkg/email"
	"ynam/pkg/email/imap"
	"ynam/pkg/spinner"
	"ynam/pkg/ynab"

	_ "ynam/pkg/email/amazon"
	_ "ynam/pkg/email/apple"
	_ "ynam/pkg/email/audible"
	_ "ynam/pkg/email/paypal"
	_ "ynam/pkg/email/venmo"

	"github.com/spf13/cobra"
)

var (
	syncDays          int
	syncDryRun        bool
	syncFailOnWarning bool
	syncLogPath       string
)

// dateMatchWindow is how many days apart an email and a YNAB transaction may be
// while still being considered the same purchase.
const dateMatchWindow = 5

// ynabTransactionDaysBuffer is how many extra days of YNAB transactions to fetch
// beyond the email fetch window, to account for possible delays in YNAB syncing.
const ynabTransactionDaysBuffer = 10

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync parsed email transactions into YNAB memos",
	Long: `Fetch transactions from configured email accounts, match them against
unapproved YNAB transactions by amount and date, and update each matched
YNAB transaction's memo with the description from the email.

Use --dry-run to preview matches without modifying YNAB.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		ctx := cmd.Context()

		lg, err := newSyncLogger(syncLogPath)
		if err != nil {
			return err
		}
		defer lg.Close()

		daysToLookBack := cfg.DaysSince
		if syncDays > 0 {
			daysToLookBack = syncDays
		}

		// YNAB and email fetching are independent — run them concurrently.
		// The spinner has two lines: one for YNAB, one per email account.
		accounts := cfg.GetEmailAccounts()
		labels := make([]string, 0, 1+len(accounts))
		labels = append(labels, "YNAB")
		for _, a := range accounts {
			labels = append(labels, a.Email)
		}

		sp := spinner.New(labels)
		sp.Start()

		var (
			mu        sync.Mutex
			ynabErr   error
			allTxns   []ynab.Transaction
			emailTxns []email.Transaction
			emailErrs []string
		)

		var wg sync.WaitGroup

		// Goroutine 1: fetch YNAB transactions.
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := ynab.NewClient(cfg.YNABAPIToken, cfg.YNABBudgetID)
			txns, err := client.List(daysToLookBack + ynabTransactionDaysBuffer)
			mu.Lock()
			allTxns = txns
			ynabErr = err
			mu.Unlock()
			if err != nil {
				sp.Finish(0, "", err)
			} else {
				sp.Finish(0, fmt.Sprintf("%d transactions", len(txns)), nil)
			}
		}()

		// Goroutine 2+: fetch email accounts concurrently via FetchAll.
		wg.Add(1)
		go func() {
			defer wg.Done()
			results := imap.FetchAll(ctx, cfg, daysToLookBack, func(idx int, r imap.FetchResult) {
				if r.Err != nil {
					sp.Finish(idx+1, "", r.Err)
					return
				}
				detail := fmt.Sprintf("%d matched, %d parsed", r.MatchedCount, len(r.Transactions))
				sp.Finish(idx+1, detail, nil)
			})
			mu.Lock()
			for _, r := range results {
				if r.Err != nil {
					emailErrs = append(emailErrs, fmt.Sprintf("%s: %v", r.Account, r.Err))
					continue
				}
				emailTxns = append(emailTxns, r.Transactions...)
			}
			mu.Unlock()
		}()

		wg.Wait()
		sp.Stop()

		if ynabErr != nil {
			return fmt.Errorf("YNAB: %w", ynabErr)
		}
		for _, e := range emailErrs {
			lg.Printf("Warning: %s\n", e)
		}

		// When --fail-on-warning is set, any email-account failure should make
		// the command exit non-zero (so a scheduler/notifier can detect it),
		// but only after we finish processing whatever did succeed.
		var warnErr error
		if syncFailOnWarning && len(emailErrs) > 0 {
			warnErr = fmt.Errorf("%d email account(s) failed: %s",
				len(emailErrs), strings.Join(emailErrs, "; "))
		}

		if len(allTxns) == 0 {
			lg.Println("No YNAB transactions found in range.")
			return warnErr
		}
		if len(emailTxns) == 0 {
			lg.Println("No email transactions found to match against.")
			return warnErr
		}

		ynabClient := ynab.NewClient(cfg.YNABAPIToken, cfg.YNABBudgetID)

		var summarizer *claude.Summarizer
		if cfg.HasAnthropicKey() {
			summarizer = claude.New(cfg.AnthropicAPIKey)
		}

		matched := 0
		skipped := 0
		updated := 0
		usedEmail := make([]bool, len(emailTxns))

		// Match against ALL YNAB transactions regardless of memo so we can
		// distinguish "already memoized" (Skipped) from "no counterpart" (No match).
		for _, yt := range allTxns {
			idx := findMatch(yt, emailTxns, usedEmail, cfg.Services)
			if idx < 0 {
				continue
			}
			usedEmail[idx] = true

			et := emailTxns[idx]

			// Already has a memo — show as skipped, don't overwrite.
			if yt.Memo != nil && *yt.Memo != "" {
				skipped++
				lg.Printf("Skipped: %-24s %10s  ->  [%s] already has memo: %s\n",
					truncate(yt.Payee, 24),
					yt.Amount.StringFixed(2),
					et.Service,
					*yt.Memo,
				)
				continue
			}

			matched++
			memo := buildMemo(et)
			if memo == "" {
				continue
			}

			if summarizer != nil && et.Service != "venmo" {
				if short, err := summarizeItems(ctx, summarizer, et); err == nil && short != "" {
					memo = short
				} else if err != nil {
					lg.Printf("  warning: claude summarize: %v\n", err)
				}
			}

			lg.Printf("Match: %-24s %10s  ->  [%s] %s\n",
				truncate(yt.Payee, 24),
				yt.Amount.StringFixed(2),
				et.Service,
				memo,
			)

			if syncDryRun {
				continue
			}

			if err := ynabClient.Update(yt.ID, memo); err != nil {
				lg.Printf("  failed to update %s: %v\n", yt.ID, err)
				continue
			}
			updated++
		}

		lg.Printf("\nMatched %d, skipped %d (already memoized).\n", matched, skipped)
		if syncDryRun {
			unmatched := 0
			for i, et := range emailTxns {
				if !usedEmail[i] {
					if unmatched == 0 {
						lg.Println("\nUnmatched email transactions (no YNAB counterpart in range):")
					}
					lg.Printf("  No match: [%s] %-12s %10s  %s\n",
						et.Service, et.Payee, et.Amount.StringFixed(2), et.Date)
					unmatched++
				}
			}
			lg.Println("\nDry run: no changes were written to YNAB.")
		} else {
			lg.Printf("Updated %d memo(s) in YNAB.\n", updated)
		}
		return warnErr
	},
}

func findMatch(yt ynab.Transaction, emailTxns []email.Transaction, used []bool, services map[string]config.ServiceConfig) int {
	for i, et := range emailTxns {
		if used[i] {
			continue
		}
		if !amountsMatch(yt, et) {
			continue
		}
		window := dateMatchWindow
		if svc, ok := services[strings.ToLower(et.Service)]; ok && svc.DateMatchWindow > 0 {
			window = svc.DateMatchWindow
		}
		if !datesMatchWithWindow(yt, et, window) {
			continue
		}
		if !payeeMatches(yt, et, services) {
			continue
		}
		return i
	}
	return -1
}

// payeeMatches returns true when the YNAB payee name contains at least one of
// the configured payee_keywords for the email's service. If no keywords are
// configured the service name itself is used as the default keyword, so
// existing configs work without any changes.
func payeeMatches(yt ynab.Transaction, et email.Transaction, services map[string]config.ServiceConfig) bool {
	svc, ok := services[strings.ToLower(et.Service)]
	keywords := svc.PayeeKeywords
	if !ok || len(keywords) == 0 {
		// Default: the service name must appear somewhere in the YNAB payee.
		keywords = []string{et.Service}
	}

	payeeLower := strings.ToLower(yt.Payee)
	for _, kw := range keywords {
		if strings.Contains(payeeLower, strings.ToLower(strings.TrimSpace(kw))) {
			return true
		}
	}
	return false
}

func amountsMatch(yt ynab.Transaction, et email.Transaction) bool {
	return yt.Amount.Abs().Equal(et.Amount.Abs())
}

func datesMatch(yt ynab.Transaction, et email.Transaction) bool {
	return datesMatchWithWindow(yt, et, dateMatchWindow)
}

func datesMatchWithWindow(yt ynab.Transaction, et email.Transaction, windowDays int) bool {
	if et.Date == "" {
		return true
	}
	emailDate, err := time.Parse("2006-01-02", et.Date)
	if err != nil {
		return true
	}
	diff := yt.Date.Sub(emailDate)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(windowDays)*24*time.Hour
}

// summarizeItems calls Claude to shorten each item in the transaction to 3–4
// words and returns them joined by ", ". Only operates on transactions that
// carry a structured item list (e.g. Amazon orders); freeform memos from
// Venmo or PayPal are returned unchanged to avoid misinterpretation.
func summarizeItems(ctx context.Context, s *claude.Summarizer, et email.Transaction) (string, error) {
	raw, ok := et.Details["items"]
	if !ok || raw == "" {
		return "", nil // no structured items — caller keeps original memo
	}

	var items []string
	for _, item := range strings.Split(raw, "; ") {
		if t := strings.TrimSpace(item); t != "" {
			items = append(items, t)
		}
	}
	if len(items) == 0 {
		return "", nil
	}

	return s.SummarizeMemo(ctx, items)
}

func buildMemo(et email.Transaction) string {
	if et.Memo != "" {
		return et.Memo
	}
	if et.Payee != "" {
		return et.Payee
	}
	return et.Service
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().IntVar(&syncDays, "days", 0, "number of days to look back (0 = use config value)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "preview matches without updating YNAB")
	syncCmd.Flags().BoolVar(&syncFailOnWarning, "fail-on-warning", false, "exit non-zero if any email account fails (for schedulers/notifiers)")
	syncCmd.Flags().StringVar(&syncLogPath, "log", "", "also write output to this file (overwritten every 7 runs)")
}
