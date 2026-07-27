import type {
  ModelPrice,
  ModelPriceInput,
  ModelPricesResponse,
  UsageCostSummary,
} from '@/types';
import { apiClient } from './client';

export const usageCostsApi = {
  getSummary: () => apiClient.get<UsageCostSummary>('/usage-costs'),
  getPrices: () => apiClient.get<ModelPricesResponse>('/model-prices'),
  savePrice: (price: ModelPriceInput) => apiClient.put<ModelPrice>('/model-prices', price),
  deletePrice: (model: string) =>
    apiClient.delete<{ ok: boolean }>('/model-prices', { params: { model } }),
};
