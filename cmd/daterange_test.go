package cmd

import (
	"testing"
	"time"

	"ynam/pkg/config"
)

func TestResolveRange(t *testing.T) {
	cfg := &config.Config{DaysSince: 7}

	t.Run("from and to explicit", func(t *testing.T) {
		since, until, err := resolveRange(cfg, 0, "2026-03-01", "2026-03-10")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if since.Format(dateLayout) != "2026-03-01" {
			t.Errorf("since: got %s", since.Format(dateLayout))
		}
		if until.Format(dateLayout) != "2026-03-10" {
			t.Errorf("until: got %s", until.Format(dateLayout))
		}
	})

	t.Run("from overrides days", func(t *testing.T) {
		since, until, err := resolveRange(cfg, 120, "2026-03-01", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if since.Format(dateLayout) != "2026-03-01" {
			t.Errorf("since should come from --from, got %s", since.Format(dateLayout))
		}
		if !until.IsZero() {
			t.Errorf("until should be zero (open) when --to omitted, got %s", until)
		}
	})

	t.Run("days fallback when no from", func(t *testing.T) {
		since, until, err := resolveRange(cfg, 30, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantSince := time.Now().AddDate(0, 0, -30)
		if since.After(wantSince.Add(time.Minute)) || since.Before(wantSince.Add(-time.Minute)) {
			t.Errorf("since ~30 days ago expected, got %s", since)
		}
		if !until.IsZero() {
			t.Errorf("until should be zero, got %s", until)
		}
	})

	t.Run("invalid from", func(t *testing.T) {
		if _, _, err := resolveRange(cfg, 0, "03/01/2026", ""); err == nil {
			t.Error("expected error for bad --from format")
		}
	})

	t.Run("to before from", func(t *testing.T) {
		if _, _, err := resolveRange(cfg, 0, "2026-03-10", "2026-03-01"); err == nil {
			t.Error("expected error when --to precedes --from")
		}
	})
}
