export interface UsageTotals {
  calls: number;
  success_calls: number;
  failure_calls: number;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  total_tokens: number;
  cost_micros: number;
  unpriced_calls: number;
}

export interface ModelUsageSummary extends UsageTotals {
  model: string;
  priced: boolean;
}

export interface AccountUsageSummary extends UsageTotals {
  account_key: string;
  auth_index?: string;
  account_label: string;
  provider: string;
  auth_type?: string;
  first_seen_ms: number;
  last_seen_ms: number;
  models: ModelUsageSummary[];
}

export interface UsageCostSummary extends UsageTotals {
  enabled: boolean;
  updated_at_ms: number;
  accounts: AccountUsageSummary[];
  models: ModelUsageSummary[];
  unpriced_models: string[];
}

export interface ModelPrice {
  model: string;
  input_per_million_usd: number;
  output_per_million_usd: number;
  cache_read_per_million_usd: number;
  cache_write_per_million_usd: number;
  cache_read_configured: boolean;
  cache_write_configured: boolean;
  source: string;
  updated_at_ms: number;
}

export interface ModelPricesResponse {
  prices: ModelPrice[];
  observed_models: string[];
}

export interface ModelPriceInput {
  model: string;
  input_per_million_usd: number;
  output_per_million_usd: number;
  cache_read_per_million_usd?: number | null;
  cache_write_per_million_usd?: number | null;
}
