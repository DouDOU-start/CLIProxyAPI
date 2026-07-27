import type { AuthFileItem } from '@/types';
import { parseTimestampMs } from '@/utils/timestamp';

export type AuthFileImportSortDirection = 'asc' | 'desc';

const parseAuthFileTimestampMs = (value: unknown): number | null => {
  if (typeof value === 'number' && Number.isFinite(value)) {
    const timestamp = value < 1e12 ? value * 1000 : value;
    return timestamp > 0 ? timestamp : null;
  }

  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) return null;

    const numeric = Number(trimmed);
    if (Number.isFinite(numeric)) {
      const timestamp = numeric < 1e12 ? numeric * 1000 : numeric;
      return timestamp > 0 ? timestamp : null;
    }
  }

  const timestamp = parseTimestampMs(value);
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null;
};

export const readAuthFileImportedAtMs = (file: AuthFileItem): number | null => {
  const candidates = [file['created_at'], file.createdAt, file['modtime'], file.modified];

  for (const candidate of candidates) {
    const timestamp = parseAuthFileTimestampMs(candidate);
    if (timestamp !== null) return timestamp;
  }

  return null;
};

export const compareAuthFilesByImportTime = (
  left: AuthFileItem,
  right: AuthFileItem,
  direction: AuthFileImportSortDirection
): number => {
  const leftTimestamp = readAuthFileImportedAtMs(left);
  const rightTimestamp = readAuthFileImportedAtMs(right);

  if (leftTimestamp === null && rightTimestamp !== null) return 1;
  if (leftTimestamp !== null && rightTimestamp === null) return -1;

  if (leftTimestamp !== null && rightTimestamp !== null && leftTimestamp !== rightTimestamp) {
    return direction === 'asc'
      ? leftTimestamp - rightTimestamp
      : rightTimestamp - leftTimestamp;
  }

  return left.name.localeCompare(right.name);
};
