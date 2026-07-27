import { describe, expect, test } from 'bun:test';
import type { AccountUsageSummary } from '@/types';
import {
  buildAuthFileUsageIndex,
  resolveAuthFileUsageMetrics,
} from '@/features/authFiles/usageCosts';

const makeAccountUsage = (overrides: Partial<AccountUsageSummary> = {}): AccountUsageSummary => ({
  account_key: 'account-1',
  auth_index: '1',
  account_label: 'account@example.com',
  provider: 'codex',
  first_seen_ms: 1,
  last_seen_ms: 2,
  calls: 3,
  success_calls: 3,
  failure_calls: 0,
  input_tokens: 100,
  output_tokens: 50,
  reasoning_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  total_tokens: 150,
  cost_micros: 25_000,
  unpriced_calls: 0,
  models: [],
  ...overrides,
});

describe('auth file usage costs', () => {
  test('indexes account summaries by normalized auth index', () => {
    const first = makeAccountUsage({ auth_index: ' 17 ' });
    const second = makeAccountUsage({ account_key: 'account-2', auth_index: '23' });
    const missing = makeAccountUsage({ account_key: 'account-3', auth_index: undefined });

    const index = buildAuthFileUsageIndex([first, second, missing]);

    expect(index.size).toBe(2);
    expect(index.get('17')).toBe(first);
    expect(index.get('23')).toBe(second);
  });

  test('returns zero metrics when collection is ready without account history', () => {
    expect(resolveAuthFileUsageMetrics('ready')).toEqual({
      costMicros: 0,
      totalTokens: 0,
      calls: 0,
      unpricedCalls: 0,
    });
  });

  test('does not expose metrics while loading or after an API failure', () => {
    const usage = makeAccountUsage();

    expect(resolveAuthFileUsageMetrics('loading', usage)).toBeNull();
    expect(resolveAuthFileUsageMetrics('unavailable', usage)).toBeNull();
  });

  test('preserves cost, token, request, and unpriced totals', () => {
    const usage = makeAccountUsage({
      cost_micros: 1_250_000,
      total_tokens: 98_765,
      calls: 42,
      unpriced_calls: 2,
    });

    expect(resolveAuthFileUsageMetrics('ready', usage)).toEqual({
      costMicros: 1_250_000,
      totalTokens: 98_765,
      calls: 42,
      unpricedCalls: 2,
    });
  });
});
