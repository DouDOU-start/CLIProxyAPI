import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import {
  IconAlertTriangle,
  IconCheckCircle2,
  IconChevronDown,
  IconChevronUp,
  IconDollarSign,
  IconPencil,
  IconPlus,
  IconRefreshCw,
  IconSearch,
  IconTrash2,
} from '@/components/ui/icons';
import { configApi, usageCostsApi } from '@/services/api';
import { useNotificationStore } from '@/stores';
import type {
  AccountUsageSummary,
  ModelPrice,
  ModelPriceInput,
  ModelPricesResponse,
  UsageCostSummary,
} from '@/types';
import { getErrorMessage } from '@/utils/helpers';
import { formatUsageCost, formatUsageNumber, formatUsageTokens } from '@/utils/usageCosts';
import styles from './UsageCostsPage.module.scss';

type UsageView = 'accounts' | 'prices';

interface PriceDraft {
  originalModel: string;
  model: string;
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
}

const emptyPriceDraft = (): PriceDraft => ({
  originalModel: '',
  model: '',
  input: '',
  output: '',
  cacheRead: '',
  cacheWrite: '',
});

const formatRate = (value: number) =>
  new Intl.NumberFormat(undefined, { maximumFractionDigits: 6 }).format(value);

const draftFromPrice = (price: ModelPrice): PriceDraft => ({
  originalModel: price.model,
  model: price.model,
  input: String(price.input_per_million_usd),
  output: String(price.output_per_million_usd),
  cacheRead: price.cache_read_configured ? String(price.cache_read_per_million_usd) : '',
  cacheWrite: price.cache_write_configured ? String(price.cache_write_per_million_usd) : '',
});

const parseOptionalPrice = (value: string): number | null => {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : Number.NaN;
};

export function UsageCostsPage() {
  const { t } = useTranslation();
  const { showNotification, showConfirmation } = useNotificationStore();
  const [view, setView] = useState<UsageView>('accounts');
  const [summary, setSummary] = useState<UsageCostSummary | null>(null);
  const [priceBook, setPriceBook] = useState<ModelPricesResponse>({
    prices: [],
    observed_models: [],
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [expandedAccount, setExpandedAccount] = useState<string | null>(null);
  const [priceModalOpen, setPriceModalOpen] = useState(false);
  const [priceDraft, setPriceDraft] = useState<PriceDraft>(emptyPriceDraft);
  const [priceError, setPriceError] = useState('');
  const [savingPrice, setSavingPrice] = useState(false);
  const [enabling, setEnabling] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [nextSummary, nextPriceBook] = await Promise.all([
        usageCostsApi.getSummary(),
        usageCostsApi.getPrices(),
      ]);
      setSummary(nextSummary);
      setPriceBook(nextPriceBook);
    } catch (err: unknown) {
      setError(getErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const filteredAccounts = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return summary?.accounts ?? [];
    return (summary?.accounts ?? []).filter((account) =>
      [
        account.account_label,
        account.provider,
        account.auth_index,
        ...account.models.map((item) => item.model),
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query))
    );
  }, [search, summary]);

  const filteredPrices = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return priceBook.prices;
    return priceBook.prices.filter((price) => price.model.toLowerCase().includes(query));
  }, [priceBook.prices, search]);

  const openNewPrice = useCallback((model = '') => {
    setPriceDraft({ ...emptyPriceDraft(), model });
    setPriceError('');
    setPriceModalOpen(true);
  }, []);

  const openEditPrice = useCallback((price: ModelPrice) => {
    setPriceDraft(draftFromPrice(price));
    setPriceError('');
    setPriceModalOpen(true);
  }, []);

  const savePrice = async () => {
    const model = priceDraft.model.trim();
    const input = parseOptionalPrice(priceDraft.input);
    const output = parseOptionalPrice(priceDraft.output);
    const cacheRead = parseOptionalPrice(priceDraft.cacheRead);
    const cacheWrite = parseOptionalPrice(priceDraft.cacheWrite);
    if (!model || input === null || output === null) {
      setPriceError(t('usage_costs.price_required'));
      return;
    }
    if ([input, output, cacheRead, cacheWrite].some((value) => Number.isNaN(value))) {
      setPriceError(t('usage_costs.price_invalid'));
      return;
    }

    const payload: ModelPriceInput = {
      model,
      input_per_million_usd: input,
      output_per_million_usd: output,
      cache_read_per_million_usd: cacheRead,
      cache_write_per_million_usd: cacheWrite,
    };
    setSavingPrice(true);
    setPriceError('');
    try {
      await usageCostsApi.savePrice(payload);
      if (priceDraft.originalModel && priceDraft.originalModel !== model) {
        await usageCostsApi.deletePrice(priceDraft.originalModel);
      }
      setPriceModalOpen(false);
      showNotification(t('usage_costs.price_saved'), 'success');
      await load();
    } catch (err: unknown) {
      setPriceError(getErrorMessage(err));
    } finally {
      setSavingPrice(false);
    }
  };

  const confirmDeletePrice = (price: ModelPrice) => {
    showConfirmation({
      title: t('usage_costs.delete_price_title'),
      message: t('usage_costs.delete_price_confirm', { model: price.model }),
      confirmText: t('common.delete'),
      variant: 'danger',
      onConfirm: async () => {
        await usageCostsApi.deletePrice(price.model);
        showNotification(t('usage_costs.price_deleted'), 'success');
        await load();
      },
    });
  };

  const enableCollection = async () => {
    setEnabling(true);
    try {
      await configApi.updateUsageStatistics(true);
      showNotification(t('usage_costs.enabled'), 'success');
      await load();
    } catch (err: unknown) {
      showNotification(getErrorMessage(err), 'error');
    } finally {
      setEnabling(false);
    }
  };

  const toggleAccount = (account: AccountUsageSummary) => {
    setExpandedAccount((current) => (current === account.account_key ? null : account.account_key));
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('usage_costs.title')}</h1>
          <p>{t('usage_costs.description')}</p>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void load()}
          loading={loading}
          title={t('common.refresh')}
        >
          {!loading && <IconRefreshCw size={15} />}
          {t('common.refresh')}
        </Button>
      </header>

      {error && <div className="error-box">{error}</div>}

      {summary && !summary.enabled && (
        <section className={styles.disabledNotice}>
          <IconAlertTriangle size={19} />
          <div>
            <strong>{t('usage_costs.disabled_title')}</strong>
            <span>{t('usage_costs.disabled_description')}</span>
          </div>
          <Button size="sm" onClick={() => void enableCollection()} loading={enabling}>
            {t('usage_costs.enable_action')}
          </Button>
        </section>
      )}

      <section className={styles.metrics} aria-label={t('usage_costs.summary_label')}>
        <div className={styles.primaryMetric}>
          <span>{t('usage_costs.estimated_cost')}</span>
          <strong>{formatUsageCost(summary?.cost_micros ?? 0)}</strong>
          <small>{t('usage_costs.current_prices')}</small>
        </div>
        <div>
          <span>{t('usage_costs.total_tokens')}</span>
          <strong title={formatUsageNumber(summary?.total_tokens ?? 0)}>
            {formatUsageTokens(summary?.total_tokens ?? 0)}
          </strong>
        </div>
        <div>
          <span>{t('usage_costs.requests')}</span>
          <strong>{formatUsageNumber(summary?.calls ?? 0)}</strong>
        </div>
        <div className={(summary?.unpriced_calls ?? 0) > 0 ? styles.metricWarning : undefined}>
          <span>{t('usage_costs.unpriced_requests')}</span>
          <strong>{formatUsageNumber(summary?.unpriced_calls ?? 0)}</strong>
        </div>
      </section>

      <div className={styles.toolbar}>
        <div className={styles.segmented} role="tablist" aria-label={t('usage_costs.views_label')}>
          <button
            type="button"
            role="tab"
            aria-selected={view === 'accounts'}
            className={view === 'accounts' ? styles.segmentActive : ''}
            onClick={() => setView('accounts')}
          >
            {t('usage_costs.accounts_tab')}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === 'prices'}
            className={view === 'prices' ? styles.segmentActive : ''}
            onClick={() => setView('prices')}
          >
            {t('usage_costs.prices_tab')}
          </button>
        </div>
        <div className={styles.toolbarActions}>
          <div className={styles.searchField}>
            <IconSearch size={15} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={
                view === 'accounts'
                  ? t('usage_costs.search_accounts')
                  : t('usage_costs.search_prices')
              }
              aria-label={t('usage_costs.search')}
            />
          </div>
          {view === 'prices' && (
            <Button size="sm" onClick={() => openNewPrice()}>
              <IconPlus size={15} />
              {t('usage_costs.add_price')}
            </Button>
          )}
        </div>
      </div>

      {view === 'accounts' ? (
        <section className={styles.dataSection} role="tabpanel">
          {loading && !summary ? (
            <div className={styles.loadingState}>{t('common.loading')}</div>
          ) : filteredAccounts.length === 0 ? (
            <EmptyState
              title={t('usage_costs.no_usage_title')}
              description={t('usage_costs.no_usage_description')}
            />
          ) : (
            <div className={styles.tableScroller}>
              <table className={styles.dataTable}>
                <thead>
                  <tr>
                    <th>{t('usage_costs.account')}</th>
                    <th>{t('usage_costs.provider')}</th>
                    <th>{t('usage_costs.models')}</th>
                    <th>{t('usage_costs.tokens')}</th>
                    <th>{t('usage_costs.requests')}</th>
                    <th>{t('usage_costs.failures')}</th>
                    <th>{t('usage_costs.cost')}</th>
                    <th aria-label={t('usage_costs.details')} />
                  </tr>
                </thead>
                <tbody>
                  {filteredAccounts.map((account) => {
                    const expanded = expandedAccount === account.account_key;
                    return (
                      <AccountRows
                        key={account.account_key}
                        account={account}
                        expanded={expanded}
                        onToggle={() => toggleAccount(account)}
                        onAddPrice={openNewPrice}
                        t={t}
                      />
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>
      ) : (
        <section className={styles.dataSection} role="tabpanel">
          {(summary?.unpriced_models.length ?? 0) > 0 && (
            <div className={styles.unpricedStrip}>
              <div>
                <IconAlertTriangle size={17} />
                <span>
                  {t('usage_costs.unpriced_models_count', {
                    count: summary?.unpriced_models.length ?? 0,
                  })}
                </span>
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => openNewPrice(summary?.unpriced_models[0] ?? '')}
              >
                {t('usage_costs.configure_first')}
              </Button>
            </div>
          )}
          {filteredPrices.length === 0 ? (
            <EmptyState
              title={t('usage_costs.no_prices_title')}
              description={t('usage_costs.no_prices_description')}
              action={
                <Button size="sm" onClick={() => openNewPrice(summary?.unpriced_models[0] ?? '')}>
                  <IconPlus size={15} />
                  {t('usage_costs.add_price')}
                </Button>
              }
            />
          ) : (
            <div className={styles.tableScroller}>
              <table className={styles.dataTable}>
                <thead>
                  <tr>
                    <th>{t('usage_costs.model')}</th>
                    <th>{t('usage_costs.input_price')}</th>
                    <th>{t('usage_costs.output_price')}</th>
                    <th>{t('usage_costs.cache_read_price')}</th>
                    <th>{t('usage_costs.cache_write_price')}</th>
                    <th>{t('usage_costs.source')}</th>
                    <th aria-label={t('common.action')} />
                  </tr>
                </thead>
                <tbody>
                  {filteredPrices.map((price) => (
                    <tr key={price.model}>
                      <td className={styles.modelName}>{price.model}</td>
                      <td>{formatRate(price.input_per_million_usd)}</td>
                      <td>{formatRate(price.output_per_million_usd)}</td>
                      <td>
                        {price.cache_read_configured
                          ? formatRate(price.cache_read_per_million_usd)
                          : t('usage_costs.auto_price')}
                      </td>
                      <td>
                        {price.cache_write_configured
                          ? formatRate(price.cache_write_per_million_usd)
                          : t('usage_costs.auto_price')}
                      </td>
                      <td>
                        {price.source === 'manual'
                          ? t('usage_costs.source_manual')
                          : price.source === 'builtin'
                            ? t('usage_costs.source_builtin')
                            : price.source}
                      </td>
                      <td>
                        <div className={styles.rowActions}>
                          <button
                            type="button"
                            onClick={() => openEditPrice(price)}
                            title={t('common.edit')}
                            aria-label={t('common.edit')}
                          >
                            <IconPencil size={15} />
                          </button>
                          <button
                            type="button"
                            onClick={() => confirmDeletePrice(price)}
                            title={t('common.delete')}
                            aria-label={t('common.delete')}
                          >
                            <IconTrash2 size={15} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      <Modal
        open={priceModalOpen}
        onClose={() => setPriceModalOpen(false)}
        title={priceDraft.originalModel ? t('usage_costs.edit_price') : t('usage_costs.add_price')}
        width={620}
        closeDisabled={savingPrice}
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => setPriceModalOpen(false)}
              disabled={savingPrice}
            >
              {t('common.cancel')}
            </Button>
            <Button onClick={() => void savePrice()} loading={savingPrice}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className={styles.priceForm}>
          <Input
            label={t('usage_costs.model')}
            value={priceDraft.model}
            onChange={(event) =>
              setPriceDraft((draft) => ({ ...draft, model: event.target.value }))
            }
            list="observed-usage-models"
            placeholder="gpt-5.4"
            autoFocus
          />
          <datalist id="observed-usage-models">
            {priceBook.observed_models.map((model) => (
              <option key={model} value={model} />
            ))}
          </datalist>
          <div className={styles.priceGrid}>
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('usage_costs.input_price')}
              value={priceDraft.input}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, input: event.target.value }))
              }
              placeholder="0.00"
            />
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('usage_costs.output_price')}
              value={priceDraft.output}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, output: event.target.value }))
              }
              placeholder="0.00"
            />
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('usage_costs.cache_read_price')}
              hint={t('usage_costs.cache_read_hint')}
              value={priceDraft.cacheRead}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, cacheRead: event.target.value }))
              }
              placeholder={t('usage_costs.auto_price')}
            />
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('usage_costs.cache_write_price')}
              hint={t('usage_costs.cache_write_hint')}
              value={priceDraft.cacheWrite}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, cacheWrite: event.target.value }))
              }
              placeholder={t('usage_costs.auto_price')}
            />
          </div>
          <div className={styles.priceUnit}>
            <IconDollarSign size={16} />
            {t('usage_costs.price_unit')}
          </div>
          {priceError && <div className="error-box">{priceError}</div>}
        </div>
      </Modal>
    </div>
  );
}

function AccountRows({
  account,
  expanded,
  onToggle,
  onAddPrice,
  t,
}: {
  account: AccountUsageSummary;
  expanded: boolean;
  onToggle: () => void;
  onAddPrice: (model?: string) => void;
  t: ReturnType<typeof useTranslation>['t'];
}) {
  const failureRate = account.calls > 0 ? (account.failure_calls / account.calls) * 100 : 0;
  return (
    <>
      <tr className={expanded ? styles.expandedRow : undefined}>
        <td>
          <div className={styles.accountIdentity}>
            <strong>{account.account_label}</strong>
            {account.auth_index && <code>{account.auth_index.slice(0, 12)}</code>}
          </div>
        </td>
        <td>{account.provider}</td>
        <td>{account.models.length}</td>
        <td title={formatUsageNumber(account.total_tokens)}>
          {formatUsageTokens(account.total_tokens)}
        </td>
        <td>{formatUsageNumber(account.calls)}</td>
        <td>
          <span className={failureRate > 5 ? styles.failureHigh : styles.failureNormal}>
            {failureRate.toFixed(failureRate > 0 && failureRate < 1 ? 1 : 0)}%
          </span>
        </td>
        <td className={styles.costCell}>
          {formatUsageCost(account.cost_micros)}
          {account.unpriced_calls > 0 && (
            <span title={t('usage_costs.unpriced_account_hint')}>
              <IconAlertTriangle size={13} />
            </span>
          )}
        </td>
        <td>
          <button
            type="button"
            className={styles.expandButton}
            onClick={onToggle}
            aria-expanded={expanded}
            aria-label={expanded ? t('common.collapse') : t('common.expand')}
          >
            {expanded ? <IconChevronUp size={16} /> : <IconChevronDown size={16} />}
          </button>
        </td>
      </tr>
      {expanded && (
        <tr className={styles.detailRow}>
          <td colSpan={8}>
            <div className={styles.modelBreakdown}>
              <div className={styles.breakdownHeader}>
                <span>{t('usage_costs.model_breakdown')}</span>
                <span>{t('usage_costs.input_output_tokens')}</span>
                <span>{t('usage_costs.cost')}</span>
              </div>
              {account.models.map((model) => (
                <div key={model.model} className={styles.breakdownRow}>
                  <div>
                    {model.priced ? (
                      <IconCheckCircle2 size={14} className={styles.pricedIcon} />
                    ) : (
                      <IconAlertTriangle size={14} className={styles.unpricedIcon} />
                    )}
                    <strong>{model.model}</strong>
                  </div>
                  <span>
                    {formatUsageTokens(model.input_tokens)} /{' '}
                    {formatUsageTokens(model.output_tokens)}
                  </span>
                  <span>
                    {model.priced
                      ? formatUsageCost(model.cost_micros)
                      : t('usage_costs.not_priced')}
                  </span>
                  {!model.priced && (
                    <Button variant="ghost" size="sm" onClick={() => onAddPrice(model.model)}>
                      {t('usage_costs.configure_price')}
                    </Button>
                  )}
                </div>
              ))}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
