package usagestats

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestStoreSeedsBuiltinCodexPrices(t *testing.T) {
	store, errStore := NewStore(filepath.Join(t.TempDir(), storageFileName), true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	defer store.Close()

	prices := make(map[string]ModelPrice)
	for _, price := range store.ListPrices() {
		prices[price.Model] = price
	}
	expected := map[string]ModelPrice{
		"gpt-5.3-codex-spark": {InputMicrosPerMillion: 1_750_000, OutputMicrosPerMillion: 14_000_000, CacheReadMicrosPerMillion: 175_000},
		"gpt-5.4":             {InputMicrosPerMillion: 2_500_000, OutputMicrosPerMillion: 15_000_000, CacheReadMicrosPerMillion: 250_000},
		"gpt-5.4-mini":        {InputMicrosPerMillion: 750_000, OutputMicrosPerMillion: 4_500_000, CacheReadMicrosPerMillion: 75_000},
		"gpt-5.5":             {InputMicrosPerMillion: 5_000_000, OutputMicrosPerMillion: 30_000_000, CacheReadMicrosPerMillion: 500_000},
		"codex-auto-review":   {InputMicrosPerMillion: 5_000_000, OutputMicrosPerMillion: 30_000_000, CacheReadMicrosPerMillion: 500_000},
		"gpt-5.6-sol":         {InputMicrosPerMillion: 5_000_000, OutputMicrosPerMillion: 30_000_000, CacheReadMicrosPerMillion: 500_000, CacheWriteMicrosPerMillion: 6_250_000},
		"gpt-5.6-terra":       {InputMicrosPerMillion: 2_500_000, OutputMicrosPerMillion: 15_000_000, CacheReadMicrosPerMillion: 250_000, CacheWriteMicrosPerMillion: 3_125_000},
		"gpt-5.6-luna":        {InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 6_000_000, CacheReadMicrosPerMillion: 100_000, CacheWriteMicrosPerMillion: 1_250_000},
	}
	for model, want := range expected {
		got, exists := prices[model]
		if !exists {
			t.Fatalf("built-in price %q was not seeded", model)
		}
		if got.InputMicrosPerMillion != want.InputMicrosPerMillion ||
			got.OutputMicrosPerMillion != want.OutputMicrosPerMillion ||
			got.CacheReadMicrosPerMillion != want.CacheReadMicrosPerMillion ||
			got.CacheWriteMicrosPerMillion != want.CacheWriteMicrosPerMillion {
			t.Fatalf("price for %q = %#v, want %#v", model, got, want)
		}
		if got.Source != builtinPriceSource || !got.CacheReadConfigured || !got.CacheWriteConfigured || got.UpdatedAtMS == 0 {
			t.Fatalf("built-in metadata for %q = %#v", model, got)
		}
	}
}

func TestStoreDoesNotOverwriteManualBuiltinPrice(t *testing.T) {
	path := filepath.Join(t.TempDir(), storageFileName)
	store, errStore := NewStore(path, true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	if errPrice := store.UpsertPrice(ModelPrice{
		Model:                  "gpt-5.6-sol",
		InputMicrosPerMillion:  123_000,
		OutputMicrosPerMillion: 456_000,
	}); errPrice != nil {
		t.Fatalf("UpsertPrice() error = %v", errPrice)
	}
	store.Close()

	reopened, errReopen := NewStore(path, false)
	if errReopen != nil {
		t.Fatalf("reopen store error = %v", errReopen)
	}
	defer reopened.Close()
	for _, price := range reopened.ListPrices() {
		if price.Model != "gpt-5.6-sol" {
			continue
		}
		if price.InputMicrosPerMillion != 123_000 || price.OutputMicrosPerMillion != 456_000 || price.Source != "manual" {
			t.Fatalf("manual price was overwritten: %#v", price)
		}
		return
	}
	t.Fatal("manual gpt-5.6-sol price was not found")
}

func TestStoreUsesBuiltinPriceForUsageCost(t *testing.T) {
	store, errStore := NewStore(filepath.Join(t.TempDir(), storageFileName), true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	defer store.Close()
	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:  "openai",
		Model:     "gpt-5.4-mini",
		AuthIndex: "auth-index-1",
		Detail: coreusage.Detail{
			InputTokens:  1_000_000,
			OutputTokens: 1_000_000,
			TotalTokens:  2_000_000,
		},
	})

	summary := store.Summary()
	if summary.CostMicros != 5_250_000 || summary.UnpricedCalls != 0 {
		t.Fatalf("summary with built-in price = %#v", summary)
	}
}

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
