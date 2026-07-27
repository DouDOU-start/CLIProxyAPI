import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import {
  IconAlertTriangle,
  IconDollarSign,
  IconPencil,
  IconPlus,
  IconSearch,
  IconTrash2,
} from '@/components/ui/icons';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { usageCostsApi } from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { ModelPrice, ModelPriceInput, ModelPricesResponse } from '@/types';
import { getErrorMessage } from '@/utils/helpers';
import styles from './ModelPricesPage.module.scss';

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

export function ModelPricesPage() {
  const { t } = useTranslation();
  const { showNotification, showConfirmation } = useNotificationStore();
  const [priceBook, setPriceBook] = useState<ModelPricesResponse>({
    prices: [],
    observed_models: [],
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [priceModalOpen, setPriceModalOpen] = useState(false);
  const [priceDraft, setPriceDraft] = useState<PriceDraft>(emptyPriceDraft);
  const [priceError, setPriceError] = useState('');
  const [savingPrice, setSavingPrice] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setPriceBook(await usageCostsApi.getPrices());
    } catch (err: unknown) {
      setError(getErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useHeaderRefresh(load);

  const filteredPrices = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return priceBook.prices;
    return priceBook.prices.filter((price) => price.model.toLowerCase().includes(query));
  }, [priceBook.prices, search]);

  const unconfiguredModels = useMemo(() => {
    const configured = new Set(priceBook.prices.map((price) => price.model.toLowerCase()));
    return priceBook.observed_models.filter((model) => !configured.has(model.toLowerCase()));
  }, [priceBook]);

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
      setPriceError(t('model_prices.price_required'));
      return;
    }
    if ([input, output, cacheRead, cacheWrite].some((value) => Number.isNaN(value))) {
      setPriceError(t('model_prices.price_invalid'));
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
      showNotification(t('model_prices.price_saved'), 'success');
      await load();
    } catch (err: unknown) {
      setPriceError(getErrorMessage(err));
    } finally {
      setSavingPrice(false);
    }
  };

  const confirmDeletePrice = (price: ModelPrice) => {
    showConfirmation({
      title: t('model_prices.delete_price_title'),
      message: t('model_prices.delete_price_confirm', { model: price.model }),
      confirmText: t('common.delete'),
      variant: 'danger',
      onConfirm: async () => {
        await usageCostsApi.deletePrice(price.model);
        showNotification(t('model_prices.price_deleted'), 'success');
        await load();
      },
    });
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('model_prices.title')}</h1>
          <p>{t('model_prices.description')}</p>
        </div>
      </header>

      {error && <div className="error-box">{error}</div>}

      <div className={styles.toolbar}>
        <div className={styles.searchField}>
          <IconSearch size={15} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('model_prices.search')}
            aria-label={t('model_prices.search')}
          />
        </div>
        <Button size="sm" onClick={() => openNewPrice()}>
          <IconPlus size={15} />
          {t('model_prices.add_price')}
        </Button>
      </div>

      <section className={styles.dataSection}>
        {unconfiguredModels.length > 0 && (
          <div className={styles.unconfiguredStrip}>
            <div>
              <IconAlertTriangle size={17} />
              <span>
                {t('model_prices.unconfigured_models_count', {
                  count: unconfiguredModels.length,
                })}
              </span>
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => openNewPrice(unconfiguredModels[0] ?? '')}
            >
              {t('model_prices.configure_first')}
            </Button>
          </div>
        )}

        {loading && priceBook.prices.length === 0 ? (
          <div className={styles.loadingState}>{t('common.loading')}</div>
        ) : filteredPrices.length === 0 ? (
          <EmptyState
            title={t('model_prices.no_prices_title')}
            description={t('model_prices.no_prices_description')}
            action={
              <Button size="sm" onClick={() => openNewPrice(unconfiguredModels[0] ?? '')}>
                <IconPlus size={15} />
                {t('model_prices.add_price')}
              </Button>
            }
          />
        ) : (
          <div className={styles.tableScroller}>
            <table className={styles.dataTable}>
              <thead>
                <tr>
                  <th>{t('model_prices.model')}</th>
                  <th>{t('model_prices.input_price')}</th>
                  <th>{t('model_prices.output_price')}</th>
                  <th>{t('model_prices.cache_read_price')}</th>
                  <th>{t('model_prices.cache_write_price')}</th>
                  <th>{t('model_prices.source')}</th>
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
                        : t('model_prices.auto_price')}
                    </td>
                    <td>
                      {price.cache_write_configured
                        ? formatRate(price.cache_write_per_million_usd)
                        : t('model_prices.auto_price')}
                    </td>
                    <td>
                      <span className={`${styles.sourceBadge} ${styles[`source_${price.source}`] ?? ''}`}>
                        {price.source === 'manual'
                          ? t('model_prices.source_manual')
                          : price.source === 'builtin'
                            ? t('model_prices.source_builtin')
                            : price.source}
                      </span>
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
                        {price.source === 'manual' && (
                          <button
                            type="button"
                            onClick={() => confirmDeletePrice(price)}
                            title={t('common.delete')}
                            aria-label={t('common.delete')}
                          >
                            <IconTrash2 size={15} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <Modal
        open={priceModalOpen}
        onClose={() => setPriceModalOpen(false)}
        title={priceDraft.originalModel ? t('model_prices.edit_price') : t('model_prices.add_price')}
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
            label={t('model_prices.model')}
            value={priceDraft.model}
            onChange={(event) =>
              setPriceDraft((draft) => ({ ...draft, model: event.target.value }))
            }
            list="observed-model-prices"
            placeholder="gpt-5.4"
            autoFocus
          />
          <datalist id="observed-model-prices">
            {priceBook.observed_models.map((model) => (
              <option key={model} value={model} />
            ))}
          </datalist>
          <div className={styles.priceGrid}>
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('model_prices.input_price')}
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
              label={t('model_prices.output_price')}
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
              label={t('model_prices.cache_read_price')}
              hint={t('model_prices.cache_read_hint')}
              value={priceDraft.cacheRead}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, cacheRead: event.target.value }))
              }
              placeholder={t('model_prices.auto_price')}
            />
            <Input
              type="number"
              min="0"
              step="0.000001"
              label={t('model_prices.cache_write_price')}
              hint={t('model_prices.cache_write_hint')}
              value={priceDraft.cacheWrite}
              onChange={(event) =>
                setPriceDraft((draft) => ({ ...draft, cacheWrite: event.target.value }))
              }
              placeholder={t('model_prices.auto_price')}
            />
          </div>
          <div className={styles.priceUnit}>
            <IconDollarSign size={16} />
            {t('model_prices.price_unit')}
          </div>
          {priceError && <div className="error-box">{priceError}</div>}
        </div>
      </Modal>
    </div>
  );
}
