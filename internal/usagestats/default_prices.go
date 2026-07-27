package usagestats

const builtinPriceSource = "builtin"

var builtinCodexModelPrices = []ModelPrice{
	{
		Model:                      "gpt-5.3-codex-spark",
		InputMicrosPerMillion:      1_750_000,
		OutputMicrosPerMillion:     14_000_000,
		CacheReadMicrosPerMillion:  175_000,
		CacheWriteMicrosPerMillion: 0,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.4",
		InputMicrosPerMillion:      2_500_000,
		OutputMicrosPerMillion:     15_000_000,
		CacheReadMicrosPerMillion:  250_000,
		CacheWriteMicrosPerMillion: 0,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.4-mini",
		InputMicrosPerMillion:      750_000,
		OutputMicrosPerMillion:     4_500_000,
		CacheReadMicrosPerMillion:  75_000,
		CacheWriteMicrosPerMillion: 0,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.5",
		InputMicrosPerMillion:      5_000_000,
		OutputMicrosPerMillion:     30_000_000,
		CacheReadMicrosPerMillion:  500_000,
		CacheWriteMicrosPerMillion: 0,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "codex-auto-review",
		InputMicrosPerMillion:      5_000_000,
		OutputMicrosPerMillion:     30_000_000,
		CacheReadMicrosPerMillion:  500_000,
		CacheWriteMicrosPerMillion: 0,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.6-sol",
		InputMicrosPerMillion:      5_000_000,
		OutputMicrosPerMillion:     30_000_000,
		CacheReadMicrosPerMillion:  500_000,
		CacheWriteMicrosPerMillion: 6_250_000,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.6-terra",
		InputMicrosPerMillion:      2_500_000,
		OutputMicrosPerMillion:     15_000_000,
		CacheReadMicrosPerMillion:  250_000,
		CacheWriteMicrosPerMillion: 3_125_000,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
	{
		Model:                      "gpt-5.6-luna",
		InputMicrosPerMillion:      1_000_000,
		OutputMicrosPerMillion:     6_000_000,
		CacheReadMicrosPerMillion:  100_000,
		CacheWriteMicrosPerMillion: 1_250_000,
		CacheReadConfigured:        true,
		CacheWriteConfigured:       true,
	},
}

func (s *Store) insertMissingBuiltinPrices(updatedAtMS int64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	inserted := false
	for _, builtinPrice := range builtinCodexModelPrices {
		key := normalizeModelKey(builtinPrice.Model)
		if _, exists := s.state.Prices[key]; exists {
			continue
		}
		builtinPrice.Source = builtinPriceSource
		builtinPrice.UpdatedAtMS = updatedAtMS
		s.state.Prices[key] = builtinPrice
		inserted = true
	}
	return inserted
}
