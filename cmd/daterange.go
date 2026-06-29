package cmd

import (
	"fmt"
	"time"

	"ynam/pkg/config"
)

// dateLayout is the accepted format for --from / --to flags.
const dateLayout = "2006-01-02"

// resolveRange computes the [since, until] window for a command from its flags.
//
//   - If --from is given, since is that date; otherwise since is `days` ago
//     (falling back to the config's days_since when days is 0).
//   - If --to is given, until is that date (inclusive); otherwise until is zero,
//     meaning "no upper bound" (up to now).
//
// It also returns the effective lookback-in-days for display/log purposes.
func resolveRange(cfg *config.Config, days int, fromStr, toStr string) (since, until time.Time, err error) {
	if fromStr != "" {
		since, err = time.Parse(dateLayout, fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from %q (want YYYY-MM-DD): %w", fromStr, err)
		}
	} else {
		since = cfg.SinceDateAsTime(days)
	}

	if toStr != "" {
		until, err = time.Parse(dateLayout, toStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --to %q (want YYYY-MM-DD): %w", toStr, err)
		}
	}

	if !until.IsZero() && until.Before(since) {
		return time.Time{}, time.Time{}, fmt.Errorf("--to %s is before --from %s", toStr, since.Format(dateLayout))
	}
	return since, until, nil
}

// rangeLabel renders the window for human-readable output.
func rangeLabel(since, until time.Time) string {
	if until.IsZero() {
		return fmt.Sprintf("since %s", since.Format(dateLayout))
	}
	return fmt.Sprintf("%s to %s", since.Format(dateLayout), until.Format(dateLayout))
}
