import { describe, expect, test } from 'bun:test';
import { apiClient } from '../src/services/api/client';
import { usageCostsApi } from '../src/services/api/usageCosts';

describe('usage costs API', () => {
  test('uses management endpoints and preserves optional cache prices', async () => {
    const client = apiClient as unknown as {
      get: (url: string) => Promise<unknown>;
      put: (url: string, body: unknown) => Promise<unknown>;
      delete: (url: string, config?: unknown) => Promise<unknown>;
    };
    const originalGet = client.get;
    const originalPut = client.put;
    const originalDelete = client.delete;
    const calls: Array<{ method: string; url: string; value?: unknown }> = [];

    client.get = async (url) => {
      calls.push({ method: 'GET', url });
      return url === '/usage-costs'
        ? { enabled: true, accounts: [], models: [], unpriced_models: [] }
        : { prices: [], observed_models: [] };
    };
    client.put = async (url, body) => {
      calls.push({ method: 'PUT', url, value: body });
      return body;
    };
    client.delete = async (url, config) => {
      calls.push({ method: 'DELETE', url, value: config });
      return { ok: true };
    };

    try {
      await usageCostsApi.getSummary();
      await usageCostsApi.getPrices();
      await usageCostsApi.savePrice({
        model: 'gpt-test',
        input_per_million_usd: 2,
        output_per_million_usd: 4,
        cache_read_per_million_usd: null,
        cache_write_per_million_usd: null,
      });
      await usageCostsApi.deletePrice('gpt-test');
      await usageCostsApi.clearSummary();

      expect(calls).toEqual([
        { method: 'GET', url: '/usage-costs' },
        { method: 'GET', url: '/model-prices' },
        {
          method: 'PUT',
          url: '/model-prices',
          value: {
            model: 'gpt-test',
            input_per_million_usd: 2,
            output_per_million_usd: 4,
            cache_read_per_million_usd: null,
            cache_write_per_million_usd: null,
          },
        },
        { method: 'DELETE', url: '/model-prices', value: { params: { model: 'gpt-test' } } },
        { method: 'DELETE', url: '/usage-costs', value: undefined },
      ]);
    } finally {
      client.get = originalGet;
      client.put = originalPut;
      client.delete = originalDelete;
    }
  });
});
