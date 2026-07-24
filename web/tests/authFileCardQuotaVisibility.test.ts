import { describe, expect, test } from 'bun:test';
import {
  canRequestAuthFileQuota,
  resolveAuthFileQuotaType,
  shouldAutoLoadAuthFileQuota,
} from '@/features/authFiles/quotaVisibility';

describe('auth-file card quota visibility', () => {
  test('shows quota-capable providers without requiring a provider filter', () => {
    expect(resolveAuthFileQuotaType({ name: 'codex.json', type: 'codex' })).toBe('codex');
    expect(resolveAuthFileQuotaType({ name: 'claude.json', provider: 'claude' })).toBe('claude');
    expect(resolveAuthFileQuotaType({ name: 'kimi.json', type: 'kimi' })).toBe('kimi');
  });

  test('keeps quota unsupported providers on the standard card layout', () => {
    expect(resolveAuthFileQuotaType({ name: 'gemini.json', type: 'gemini' })).toBeNull();
    expect(resolveAuthFileQuotaType({ name: 'unknown.json' })).toBeNull();
  });

  test('auto-loads quota only for active credentials without cached quota', () => {
    const file = { name: 'codex.json', type: 'codex' };

    expect(shouldAutoLoadAuthFileQuota(file, false, false)).toBe(true);
    expect(shouldAutoLoadAuthFileQuota(file, false, true)).toBe(false);
    expect(shouldAutoLoadAuthFileQuota(file, true, false)).toBe(false);
    expect(shouldAutoLoadAuthFileQuota({ ...file, disabled: true }, false, false)).toBe(false);
  });

  test('does not request quota for runtime-only credentials', () => {
    const runtimeFile = { name: 'codex-runtime', type: 'codex', runtime_only: true };

    expect(canRequestAuthFileQuota(runtimeFile, false)).toBe(false);
  });
});
