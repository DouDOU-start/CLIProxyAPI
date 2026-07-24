import type { AuthFileItem } from '@/types';
import { resolveAuthProvider } from '@/utils/quota';
import {
  isRuntimeOnlyAuthFile,
  QUOTA_PROVIDER_TYPES,
  type QuotaProviderType,
} from '@/features/authFiles/constants';

export const resolveAuthFileQuotaType = (file: AuthFileItem): QuotaProviderType | null => {
  const provider = resolveAuthProvider(file);
  if (!QUOTA_PROVIDER_TYPES.has(provider as QuotaProviderType)) return null;
  return provider as QuotaProviderType;
};

export const canRequestAuthFileQuota = (file: AuthFileItem, disableControls: boolean): boolean =>
  !disableControls && !file.disabled && !isRuntimeOnlyAuthFile(file);

export const shouldAutoLoadAuthFileQuota = (
  file: AuthFileItem,
  disableControls: boolean,
  hasCachedQuota: boolean
): boolean => canRequestAuthFileQuota(file, disableControls) && !hasCachedQuota;
