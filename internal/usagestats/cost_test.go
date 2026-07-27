package usagestats

import "testing"

func TestCostMicrosSeparatesCachedInput(t *testing.T) {
	aggregate := Aggregate{
		Provider:        "openai",
		Model:           "gpt-cached",
		InputTokens:     1_000_000,
		OutputTokens:    500_000,
		CachedTokens:    250_000,
		CacheReadTokens: 250_000,
	}
	price := ModelPrice{
		InputMicrosPerMillion:     2_000_000,
		OutputMicrosPerMillion:    4_000_000,
		CacheReadMicrosPerMillion: 1_000_000,
		CacheReadConfigured:       true,
	}

	if got := costMicrosForAggregate(aggregate, price); got != 3_750_000 {
		t.Fatalf("cost = %d micro-dollars, want 3750000", got)
	}
}

func TestCostMicrosDoesNotDoubleBillMirroredCache(t *testing.T) {
	aggregate := Aggregate{
		Provider:        "openai",
		Model:           "gpt-cached",
		InputTokens:     1_000_000,
		CachedTokens:    400_000,
		CacheReadTokens: 400_000,
	}
	price := ModelPrice{
		InputMicrosPerMillion:     2_000_000,
		CacheReadMicrosPerMillion: 1_000_000,
		CacheReadConfigured:       true,
	}

	if got := costMicrosForAggregate(aggregate, price); got != 1_600_000 {
		t.Fatalf("cost = %d micro-dollars, want 1600000", got)
	}
}

func TestCostMicrosTreatsClaudeCacheAsSeparateInput(t *testing.T) {
	aggregate := Aggregate{
		Provider:         "claude",
		ExecutorType:     "ClaudeExecutor",
		Model:            "claude-cached",
		InputTokens:      600_000,
		OutputTokens:     250_000,
		CacheReadTokens:  2_000_000,
		CacheWriteTokens: 100_000,
	}
	price := ModelPrice{
		InputMicrosPerMillion:      2_000_000,
		OutputMicrosPerMillion:     4_000_000,
		CacheReadMicrosPerMillion:  1_000_000,
		CacheWriteMicrosPerMillion: 3_000_000,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	}

	if got := costMicrosForAggregate(aggregate, price); got != 4_500_000 {
		t.Fatalf("cost = %d micro-dollars, want 4500000", got)
	}
}

func TestCostMicrosUsesZeroForUnconfiguredCachePrices(t *testing.T) {
	aggregate := Aggregate{
		Provider:         "openai",
		Model:            "gpt-cached",
		InputTokens:      1_000_000,
		CacheReadTokens:  250_000,
		CacheWriteTokens: 100_000,
	}
	price := ModelPrice{
		InputMicrosPerMillion:      2_000_000,
		CacheReadMicrosPerMillion:  9_000_000,
		CacheWriteMicrosPerMillion: 9_000_000,
	}

	if got := costMicrosForAggregate(aggregate, price); got != 1_300_000 {
		t.Fatalf("cost = %d micro-dollars, want 1300000", got)
	}
}

func TestCostMicrosAppliesKnownServiceTierMultiplier(t *testing.T) {
	aggregate := Aggregate{
		Provider:    "openai",
		Model:       "openai/gpt-5.6-sol",
		ServiceTier: "priority",
		InputTokens: 1_000_000,
	}
	price := ModelPrice{InputMicrosPerMillion: 5_000_000}

	if got := costMicrosForAggregate(aggregate, price); got != 10_000_000 {
		t.Fatalf("cost = %d micro-dollars, want 10000000", got)
	}
}

func TestLongContextPremiumDoesNotApplyToMiniModel(t *testing.T) {
	aggregate := Aggregate{
		Provider:        "openai",
		Model:           "gpt-5.4-mini",
		InputTokens:     300_000,
		LongInputTokens: 300_000,
	}
	price := ModelPrice{InputMicrosPerMillion: 1_000_000}

	if got := costMicrosForAggregate(aggregate, price); got != 300_000 {
		t.Fatalf("cost = %d micro-dollars, want 300000", got)
	}
}

func TestLongContextPremiumKeepsFlexDiscount(t *testing.T) {
	aggregate := Aggregate{
		Provider:         "openai",
		Model:            "gpt-5.6-sol",
		ServiceTier:      "flex",
		InputTokens:      300_000,
		OutputTokens:     100_000,
		LongInputTokens:  300_000,
		LongOutputTokens: 100_000,
	}
	price := ModelPrice{
		InputMicrosPerMillion:  1_000_000,
		OutputMicrosPerMillion: 2_000_000,
	}

	if got := costMicrosForAggregate(aggregate, price); got != 450_000 {
		t.Fatalf("cost = %d micro-dollars, want 450000", got)
	}
}
