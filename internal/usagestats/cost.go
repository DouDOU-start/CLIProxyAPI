package usagestats

import (
	"math/big"
	"strings"
)

const longContextInputThreshold = int64(272_000)

func costMicrosForAggregate(aggregate Aggregate, price ModelPrice) int64 {
	inputTokens := maxInt64(aggregate.InputTokens, 0)
	outputTokens := maxInt64(aggregate.OutputTokens, 0)
	cacheReadTokens := maxInt64(aggregate.CacheReadTokens, aggregate.CachedTokens)
	cacheWriteTokens := maxInt64(aggregate.CacheWriteTokens, 0)

	uncachedInputTokens := inputTokens
	if cacheIncludedInInput(aggregate) {
		uncachedInputTokens = maxInt64(inputTokens-cacheReadTokens-cacheWriteTokens, 0)
	}

	cacheReadRate := int64(0)
	if price.CacheReadConfigured {
		cacheReadRate = price.CacheReadMicrosPerMillion
	}
	cacheWriteRate := int64(0)
	if price.CacheWriteConfigured {
		cacheWriteRate = price.CacheWriteMicrosPerMillion
	}

	longInput := clampInt64(aggregate.LongInputTokens, uncachedInputTokens)
	longOutput := clampInt64(aggregate.LongOutputTokens, outputTokens)
	longCacheRead := clampInt64(maxInt64(aggregate.LongCacheReadTokens, aggregate.LongCachedTokens), cacheReadTokens)
	longCacheWrite := clampInt64(aggregate.LongCacheWriteTokens, cacheWriteTokens)

	shortCost := costMicrosForSegment(
		uncachedInputTokens-longInput,
		outputTokens-longOutput,
		cacheReadTokens-longCacheRead,
		cacheWriteTokens-longCacheWrite,
		price.InputMicrosPerMillion,
		price.OutputMicrosPerMillion,
		cacheReadRate,
		cacheWriteRate,
		1,
		1,
		1,
		1,
	)
	longCost := int64(0)
	if supportsLongContextPremium(aggregate.Model) {
		longCost = costMicrosForSegment(
			longInput,
			longOutput,
			longCacheRead,
			longCacheWrite,
			price.InputMicrosPerMillion,
			price.OutputMicrosPerMillion,
			cacheReadRate,
			cacheWriteRate,
			2,
			1,
			3,
			2,
		)
	} else {
		shortCost += costMicrosForSegment(
			longInput,
			longOutput,
			longCacheRead,
			longCacheWrite,
			price.InputMicrosPerMillion,
			price.OutputMicrosPerMillion,
			cacheReadRate,
			cacheWriteRate,
			1,
			1,
			1,
			1,
		)
	}

	total := shortCost + longCost
	numerator, denominator := serviceTierMultiplier(aggregate.Model, aggregate.ServiceTier)
	if longInput > 0 && isPriorityServiceTier(aggregate.ServiceTier) {
		numerator, denominator = 1, 1
	}
	return multiplyRatio(total, numerator, denominator)
}

func costMicrosForSegment(
	inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64,
	inputRate, outputRate, cacheReadRate, cacheWriteRate int64,
	inputMultiplierNumerator, inputMultiplierDenominator int64,
	outputMultiplierNumerator, outputMultiplierDenominator int64,
) int64 {
	return tokenCostMicros(inputTokens, inputRate, inputMultiplierNumerator, inputMultiplierDenominator) +
		tokenCostMicros(cacheReadTokens, cacheReadRate, inputMultiplierNumerator, inputMultiplierDenominator) +
		tokenCostMicros(cacheWriteTokens, cacheWriteRate, inputMultiplierNumerator, inputMultiplierDenominator) +
		tokenCostMicros(outputTokens, outputRate, outputMultiplierNumerator, outputMultiplierDenominator)
}

func tokenCostMicros(tokens, rate, multiplierNumerator, multiplierDenominator int64) int64 {
	if tokens <= 0 || rate <= 0 || multiplierNumerator <= 0 || multiplierDenominator <= 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(rate))
	numerator.Mul(numerator, big.NewInt(multiplierNumerator))
	denominator := big.NewInt(1_000_000 * multiplierDenominator)
	numerator.Add(numerator, new(big.Int).Div(new(big.Int).Set(denominator), big.NewInt(2)))
	numerator.Div(numerator, denominator)
	if !numerator.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return numerator.Int64()
}

func multiplyRatio(value, numerator, denominator int64) int64 {
	if value <= 0 || numerator <= 0 || denominator <= 0 {
		return 0
	}
	result := new(big.Int).Mul(big.NewInt(value), big.NewInt(numerator))
	result.Add(result, big.NewInt(denominator/2))
	result.Div(result, big.NewInt(denominator))
	if !result.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return result.Int64()
}

func cacheIncludedInInput(aggregate Aggregate) bool {
	identity := strings.ToLower(strings.Join([]string{aggregate.Provider, aggregate.ExecutorType, aggregate.AuthType}, " "))
	return !strings.Contains(identity, "claude") && !strings.Contains(identity, "anthropic")
}

func serviceTierMultiplier(model, tier string) (int64, int64) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "flex", "batch":
		return 1, 2
	case "priority", "fast":
		switch {
		case isModelFamily(model, "gpt-5.5"):
			return 5, 2
		case isModelFamily(model, "gpt-5.6"),
			isModelFamily(model, "gpt-5.4"),
			isModelFamily(model, "gpt-5.4-mini"),
			isModelFamily(model, "gpt-5.3-codex"):
			return 2, 1
		}
	}
	return 1, 1
}

func isPriorityServiceTier(tier string) bool {
	tier = strings.ToLower(strings.TrimSpace(tier))
	return tier == "priority" || tier == "fast"
}

func supportsLongContextPremium(model string) bool {
	model = normalizedModelTail(model)
	if isModelFamily(model, "gpt-5.6") {
		return true
	}
	if model == "gpt-5.5" || strings.HasPrefix(model, "gpt-5.5-20") {
		return true
	}
	return model == "gpt-5.4" || strings.HasPrefix(model, "gpt-5.4-20") ||
		model == "gpt-5.4-pro" || strings.HasPrefix(model, "gpt-5.4-pro-20")
}

func isModelFamily(model, family string) bool {
	model = normalizedModelTail(model)
	family = normalizedModelTail(family)
	return model == family || strings.HasPrefix(model, family+"-")
}

func normalizedModelTail(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(model, "/"); index >= 0 {
		model = model[index+1:]
	}
	return model
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func clampInt64(value, maximum int64) int64 {
	if value <= 0 || maximum <= 0 {
		return 0
	}
	if value > maximum {
		return maximum
	}
	return value
}
