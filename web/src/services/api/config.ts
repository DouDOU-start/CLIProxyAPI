/**
 * 配置相关 API
 */

import { apiClient } from './client';
import type { Config } from '@/types';
import { isRecord } from '@/utils/helpers';
import { normalizeConfigResponse } from './transformers';

export const configApi = {
  async getConfigData(): Promise<Record<string, unknown>> {
    const raw = await apiClient.get<unknown>('/config');
    return isRecord(raw) ? raw : {};
  },

  async saveConfigData(data: Record<string, unknown>): Promise<void> {
    await apiClient.put('/config', data);
  },

  /**
   * 获取配置（会进行字段规范化）
   */
  async getConfig(): Promise<Config> {
    const raw = await this.getConfigData();
    return normalizeConfigResponse(raw);
  },

  /**
   * 请求日志开关
   */
  updateRequestLog: (enabled: boolean) => apiClient.put('/request-log', { value: enabled }),
};
