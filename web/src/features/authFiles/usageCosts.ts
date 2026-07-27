import type { AccountUsageSummary } from '@/types';
import { normalizeRecentRequestAuthIndex } from '@/utils/recentRequests';

export type AuthFilesUsageCostsStatus = 'loading' | 'ready' | 'unavailable';

export interface AuthFileUsageMetrics {
  costMicros: number;
  totalTokens: number;
  calls: number;
  unpricedCalls: number;
}

export function buildAuthFileUsageIndex(accounts: AccountUsageSummary[]) {
  const usageByAuthIndex = new Map<string, AccountUsageSummary>();

  accounts.forEach((account) => {
    const authIndex = normalizeRecentRequestAuthIndex(account.auth_index);
    if (authIndex) {
      usageByAuthIndex.set(authIndex, account);
    }
  });

  return usageByAuthIndex;
}

export function resolveAuthFileUsageMetrics(
  status: AuthFilesUsageCostsStatus,
  usage?: AccountUsageSummary
): AuthFileUsageMetrics | null {
  if (status !== 'ready') return null;

  return {
    costMicros: usage?.cost_micros ?? 0,
    totalTokens: usage?.total_tokens ?? 0,
    calls: usage?.calls ?? 0,
    unpricedCalls: usage?.unpriced_calls ?? 0,
  };
}
