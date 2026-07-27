package usagestats

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestStorePersistsAccountUsageWithoutRawAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), storageFileName)
	store, errStore := NewStore(path, true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-test",
		AuthIndex:   "auth-index-1",
		AuthType:    "apikey",
		Source:      "secret-upstream-api-key",
		RequestedAt: time.Unix(1_700_000_000, 0),
		Detail: coreusage.Detail{
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			TotalTokens:  1_500_000,
		},
	})
	if errPrice := store.UpsertPrice(ModelPrice{
		Model:                  "gpt-test",
		InputMicrosPerMillion:  2_000_000,
		OutputMicrosPerMillion: 4_000_000,
	}); errPrice != nil {
		t.Fatalf("UpsertPrice() error = %v", errPrice)
	}

	summary := store.Summary()
	if len(summary.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(summary.Accounts))
	}
	if strings.Contains(summary.Accounts[0].AccountLabel, "secret-upstream-api-key") {
		t.Fatalf("account label leaked raw API key: %q", summary.Accounts[0].AccountLabel)
	}
	if summary.CostMicros != 4_000_000 {
		t.Fatalf("cost = %d micro-dollars, want 4000000", summary.CostMicros)
	}
	store.Close()

	reopened, errReopen := NewStore(path, false)
	if errReopen != nil {
		t.Fatalf("reopen store error = %v", errReopen)
	}
	defer reopened.Close()
	reopenedSummary := reopened.Summary()
	if len(reopenedSummary.Accounts) != 1 || reopenedSummary.TotalTokens != 1_500_000 {
		t.Fatalf("reopened summary = %#v", reopenedSummary)
	}
}

func TestStoreMergesServiceTiersInAccountModelSummary(t *testing.T) {
	store, errStore := NewStore(filepath.Join(t.TempDir(), storageFileName), true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	defer store.Close()
	for _, tier := range []string{"default", "priority"} {
		store.HandleUsage(context.Background(), coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.6-sol",
			AuthIndex:   "auth-index-1",
			ServiceTier: tier,
			Detail:      coreusage.Detail{InputTokens: 100, TotalTokens: 100},
		})
	}
	if errPrice := store.UpsertPrice(ModelPrice{Model: "gpt-5.6-sol", InputMicrosPerMillion: 1_000_000}); errPrice != nil {
		t.Fatalf("UpsertPrice() error = %v", errPrice)
	}

	summary := store.Summary()
	if len(summary.Accounts) != 1 || len(summary.Accounts[0].Models) != 1 {
		t.Fatalf("account models = %#v", summary.Accounts)
	}
	if summary.Accounts[0].Models[0].Calls != 2 {
		t.Fatalf("model calls = %d, want 2", summary.Accounts[0].Models[0].Calls)
	}
}
