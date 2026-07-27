import { useCallback, useRef, useState } from 'react';
import { usageCostsApi } from '@/services/api';
import type { AccountUsageSummary } from '@/types';
import {
  buildAuthFileUsageIndex,
  type AuthFilesUsageCostsStatus,
} from '@/features/authFiles/usageCosts';

export function useAuthFilesUsageCosts() {
  const [usageByAuthIndex, setUsageByAuthIndex] = useState<Map<string, AccountUsageSummary>>(
    () => new Map()
  );
  const [status, setStatus] = useState<AuthFilesUsageCostsStatus>('loading');
  const requestIdRef = useRef(0);

  const loadUsageCosts = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    setStatus((current) => (current === 'ready' ? current : 'loading'));

    try {
      const summary = await usageCostsApi.getSummary();
      if (requestId !== requestIdRef.current) return;

      setUsageByAuthIndex(buildAuthFileUsageIndex(summary.accounts));
      setStatus('ready');
    } catch {
      if (requestId !== requestIdRef.current) return;

      setUsageByAuthIndex(new Map());
      setStatus('unavailable');
    }
  }, []);

  return { usageByAuthIndex, status, loadUsageCosts };
}
