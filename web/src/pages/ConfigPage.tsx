import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { createPortal } from 'react-dom';
import { parse as parseConfigData, stringify as serializeConfigData } from 'yaml';
import { usePageTransitionLayer } from '@/components/common/PageTransitionLayer';
import { IconCheck, IconRefreshCw } from '@/components/ui/icons';
import { VisualConfigEditor } from '@/components/config/VisualConfigEditor';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useActionBarHeightVar } from '@/hooks/useActionBarHeightVar';
import { useUnsavedChangesGuard } from '@/hooks/useUnsavedChangesGuard';
import { useVisualConfig } from '@/hooks/useVisualConfig';
import { useNotificationStore, useAuthStore, useConfigStore } from '@/stores';
import { configApi } from '@/services/api/config';
import styles from './ConfigPage.module.scss';

function readCommercialMode(configData: Record<string, unknown>): boolean {
  return Boolean(configData['commercial-mode']);
}

function serializeConfig(configData: Record<string, unknown>): string {
  return serializeConfigData(configData, { indent: 2, lineWidth: 120, minContentWidth: 0 });
}

function parseConfig(content: string): Record<string, unknown> {
  const parsed: unknown = parseConfigData(content);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Configuration must be an object');
  }
  return parsed as Record<string, unknown>;
}

export function ConfigPage() {
  const { t } = useTranslation();
  const pageTransitionLayer = usePageTransitionLayer();
  const isCurrentLayer = pageTransitionLayer ? pageTransitionLayer.isCurrentLayer : true;
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const isMobile = useMediaQuery('(max-width: 768px)');

  const {
    visualValues,
    visualDirty,
    visualParseError,
    visualValidationErrors,
    visualHasPayloadValidationErrors,
    loadVisualValuesFromYaml,
    applyVisualChangesToYaml,
    setVisualValues,
  } = useVisualConfig();

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const floatingActionsRef = useRef<HTMLDivElement>(null);

  const disableControls = connectionStatus !== 'connected';
  const isDirty = visualDirty;
  const shouldRenderFloatingActions = isCurrentLayer;
  const hasVisualModeError = !!visualParseError;
  const hasVisualValidationErrors =
    Object.values(visualValidationErrors).some(Boolean) || visualHasPayloadValidationErrors;
  const unsavedChangesDialog = useMemo(
    () => ({
      title: t('common.unsaved_changes_title'),
      message: t('common.unsaved_changes_message'),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    }),
    [t]
  );

  useUnsavedChangesGuard({
    enabled: isCurrentLayer,
    shouldBlock: isDirty,
    dialog: unsavedChangesDialog,
  });

  const loadConfig = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const data = await configApi.getConfigData();
      loadVisualValuesFromYaml(serializeConfig(data));
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('notification.refresh_failed');
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [loadVisualValuesFromYaml, t]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  const persistConfig = async () => {
    setSaving(true);
    try {
      const latestConfig = await configApi.getConfigData();
      const nextConfig = parseConfig(applyVisualChangesToYaml(serializeConfig(latestConfig)));
      const previousCommercialMode = readCommercialMode(latestConfig);
      const nextCommercialMode = readCommercialMode(nextConfig);
      const commercialModeChanged = previousCommercialMode !== nextCommercialMode;

      await configApi.saveConfigData(nextConfig);
      const latestContent = await configApi.getConfigData();
      loadVisualValuesFromYaml(serializeConfig(latestContent));

      // Keep the global config store in sync so sidebar and other pages update immediately.
      try {
        useConfigStore.getState().clearCache();
        await useConfigStore.getState().fetchConfig(true);
      } catch (refreshError: unknown) {
        const message =
          refreshError instanceof Error
            ? refreshError.message
            : typeof refreshError === 'string'
              ? refreshError
              : '';
        showNotification(
          `${t('notification.refresh_failed')}${message ? `: ${message}` : ''}`,
          'error'
        );
      }

      showNotification(t('config_management.save_success'), 'success');
      if (commercialModeChanged) {
        showNotification(t('notification.commercial_mode_restart_required'), 'warning');
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      showNotification(`${t('notification.save_failed')}: ${message}`, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleSave = () => {
    if (visualParseError) {
      showNotification(t('config_management.visual_mode_save_blocked'), 'error');
      return;
    }
    showConfirmation({
      title: t('config_management.save_confirm_title'),
      message: t('config_management.save_confirm_message'),
      confirmText: t('config_management.save'),
      cancelText: t('common.cancel'),
      onConfirm: persistConfig,
    });
  };

  // Keep bottom floating actions from covering page content by syncing its height to a CSS variable.
  useActionBarHeightVar(
    floatingActionsRef,
    '--config-action-bar-height',
    shouldRenderFloatingActions
  );

  // Status text
  const getStatusText = () => {
    if (disableControls) return t('config_management.status_disconnected');
    if (loading) return t('config_management.status_loading');
    if (error) return t('config_management.status_load_failed');
    if (hasVisualModeError) return t('config_management.visual_mode_unavailable');
    if (hasVisualValidationErrors)
      return t('config_management.visual.validation.validation_blocked');
    if (saving) return t('config_management.status_saving');
    if (isDirty) return t('config_management.status_dirty');
    return t('config_management.status_loaded');
  };

  const getStatusClass = () => {
    if (error || hasVisualModeError || hasVisualValidationErrors) return styles.error;
    if (isDirty) return styles.modified;
    if (!loading && !saving) return styles.saved;
    return '';
  };

  const getFloatingStatusText = () => {
    if (!isMobile) return getStatusText();
    if (disableControls)
      return t('config_management.status_disconnected_short', { defaultValue: 'Disconnected' });
    if (loading) return t('config_management.status_loading_short', { defaultValue: 'Loading' });
    if (error) return t('config_management.status_load_failed_short', { defaultValue: 'Failed' });
    if (hasVisualModeError)
      return t('config_management.visual_mode_unavailable_short', { defaultValue: 'Config issue' });
    if (hasVisualValidationErrors)
      return t('config_management.visual.validation_blocked_short', { defaultValue: 'Fix errors' });
    if (saving) return t('config_management.status_saving_short', { defaultValue: 'Saving' });
    if (isDirty) return t('config_management.status_dirty_short', { defaultValue: 'Unsaved' });
    return t('config_management.status_loaded_short', { defaultValue: 'Loaded' });
  };

  const handleReload = useCallback(() => {
    if (!isDirty) {
      void loadConfig();
      return;
    }

    showConfirmation({
      title: t('common.unsaved_changes_title'),
      message: t('config_management.reload_confirm_message'),
      confirmText: t('config_management.reload'),
      cancelText: t('common.cancel'),
      variant: 'danger',
      onConfirm: async () => {
        await loadConfig();
      },
    });
  }, [isDirty, loadConfig, showConfirmation, t]);

  const floatingActions = (
    <div className={styles.floatingActionContainer} ref={floatingActionsRef}>
      <div className={styles.floatingActionList}>
        <div
          className={`${styles.floatingStatus} ${
            isMobile ? styles.floatingStatusCompact : ''
          } ${getStatusClass()}`}
        >
          {getFloatingStatusText()}
        </div>
        <button
          type="button"
          className={styles.floatingActionButton}
          onClick={handleReload}
          disabled={loading || saving}
          title={t('config_management.reload')}
          aria-label={t('config_management.reload')}
        >
          <IconRefreshCw size={16} />
        </button>
        <button
          type="button"
          className={styles.floatingActionButton}
          onClick={handleSave}
          disabled={
            disableControls ||
            loading ||
            saving ||
            !isDirty ||
            hasVisualModeError ||
            hasVisualValidationErrors
          }
          title={t('config_management.save')}
          aria-label={t('config_management.save')}
        >
          <IconCheck size={16} />
          {isDirty && <span className={styles.dirtyDot} aria-hidden="true" />}
        </button>
      </div>
    </div>
  );

  return (
    <div className={styles.container}>
      <div className={styles.pageHeader}>
        <h1 className={styles.pageTitle}>{t('config_management.title')}</h1>
      </div>

      <div className={styles.workspaceShell}>
        <div className={styles.content}>
          {error && <div className="error-box">{error}</div>}
          {!error && visualParseError && (
            <div className="error-box">
              {t('config_management.visual_mode_unavailable_detail', { message: visualParseError })}
            </div>
          )}

          <VisualConfigEditor
            values={visualValues}
            validationErrors={visualValidationErrors}
            hasPayloadValidationErrors={visualHasPayloadValidationErrors}
            disabled={disableControls || loading}
            onChange={setVisualValues}
          />
        </div>
      </div>

      {shouldRenderFloatingActions && typeof document !== 'undefined'
        ? createPortal(floatingActions, document.body)
        : null}
    </div>
  );
}
