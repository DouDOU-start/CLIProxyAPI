import { describe, expect, test } from 'bun:test';
import { apiClient } from '../src/services/api/client';
import { modelsApi } from '../src/services/api/models';

describe('management models API', () => {
  test('uses the management session endpoint without proxy credentials', async () => {
    const client = apiClient as unknown as {
      get: (url: string, config?: unknown) => Promise<unknown>;
    };
    const originalGet = client.get;
    let requestUrl = '';
    let requestConfig: unknown;

    client.get = async (url, config) => {
      requestUrl = url;
      requestConfig = config;
      return { object: 'list', data: [{ id: 'test-model' }] };
    };

    try {
      const models = await modelsApi.fetchModels();
      expect(requestUrl).toBe('/models');
      expect(requestConfig).toBeUndefined();
      expect(models).toEqual([{ name: 'test-model' }]);
    } finally {
      client.get = originalGet;
    }
  });
});
