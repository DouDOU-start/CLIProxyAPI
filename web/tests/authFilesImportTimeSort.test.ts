import { describe, expect, test } from 'bun:test';
import {
  compareAuthFilesByImportTime,
  readAuthFileImportedAtMs,
} from '@/features/authFiles/sorting';
import type { AuthFileItem } from '@/types';

const file = (name: string, fields: Partial<AuthFileItem>): AuthFileItem => ({
  name,
  ...fields,
});

describe('auth file import time sorting', () => {
  test('prefers created_at and supports second timestamps', () => {
    expect(
      readAuthFileImportedAtMs(
        file('auth.json', {
          created_at: 1_700_000_000,
          modtime: '2026-07-01T00:00:00Z',
        })
      )
    ).toBe(1_700_000_000_000);
  });

  test('falls back to modtime when created_at is unavailable', () => {
    expect(
      readAuthFileImportedAtMs(
        file('auth.json', {
          created_at: '',
          modtime: '2026-07-01T00:00:00Z',
        })
      )
    ).toBe(Date.parse('2026-07-01T00:00:00Z'));
  });

  test('sorts newest and oldest first while keeping missing timestamps last', () => {
    const files = [
      file('missing.json', {}),
      file('older.json', { created_at: '2026-06-01T00:00:00Z' }),
      file('newer.json', { created_at: '2026-07-01T00:00:00Z' }),
    ];

    expect([...files].sort((a, b) => compareAuthFilesByImportTime(a, b, 'desc'))).toEqual([
      files[2],
      files[1],
      files[0],
    ]);
    expect([...files].sort((a, b) => compareAuthFilesByImportTime(a, b, 'asc'))).toEqual([
      files[1],
      files[2],
      files[0],
    ]);
  });
});
