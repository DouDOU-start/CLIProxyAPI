import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import { IconEye, IconEyeOff, IconShield } from '@/components/ui/icons';
import { useAuthStore, useLanguageStore, useNotificationStore } from '@/stores';
import { apiClient, type ManagementSetupStatus } from '@/services/api/client';
import { LANGUAGE_LABEL_KEYS, LANGUAGE_ORDER } from '@/utils/constants';
import { isSupportedLanguage } from '@/utils/language';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import type { ApiError } from '@/types';
import styles from './LoginPage.module.scss';

type RedirectState = { from?: { pathname?: string } };

function getLocalizedErrorMessage(error: unknown, t: (key: string) => string): string {
  const apiError = error as Partial<ApiError>;
  const status = typeof apiError.status === 'number' ? apiError.status : undefined;
  const code = typeof apiError.code === 'string' ? apiError.code : undefined;
  const message =
    error instanceof Error
      ? error.message
      : typeof apiError.message === 'string'
        ? apiError.message
        : typeof error === 'string'
          ? error
          : '';

  const withHttpStatus = (summary: string, includeBackendDetail = true) => {
    if (!status) return summary;
    const genericAxiosMessage = `Request failed with status code ${status}`;
    const detail = message.trim();
    const backendDetail =
      includeBackendDetail && detail && detail !== genericAxiosMessage
        ? ` (${t('login.error_backend_detail')}: ${detail})`
        : '';
    return `HTTP ${status}: ${summary}${backendDetail}`;
  };

  if (status === 401) return withHttpStatus(t('login.error_unauthorized'), false);
  if (status === 403) return withHttpStatus(t('login.error_forbidden'), false);
  if (status === 404) return withHttpStatus(t('login.error_not_found'), false);
  if (status === 409) return withHttpStatus(t('login.error_setup_conflict'), false);
  if (status === 429) return withHttpStatus(t('login.error_rate_limited'), false);
  if (status === 503) return withHttpStatus(t('login.error_not_configured'), false);
  if (status && status >= 500) return withHttpStatus(t('login.error_server'));
  if (code === 'ECONNABORTED' || message.toLowerCase().includes('timeout')) {
    return t('login.error_timeout');
  }
  if (code === 'ERR_NETWORK' || message.toLowerCase().includes('network error')) {
    return t('login.error_network');
  }
  if (code === 'ERR_CERT_AUTHORITY_INVALID' || message.toLowerCase().includes('certificate')) {
    return t('login.error_ssl');
  }
  return withHttpStatus(t('login.error_invalid'));
}

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const login = useAuthStore((state) => state.login);
  const restoreSession = useAuthStore((state) => state.restoreSession);
  const storedEmail = useAuthStore((state) => state.email);
  const storedRememberSession = useAuthStore((state) => state.rememberSession);

  const [email, setEmail] = useState(storedEmail);
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [rememberSession, setRememberSession] = useState(storedRememberSession);
  const [setupStatus, setSetupStatus] = useState<ManagementSetupStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState(true);
  const [error, setError] = useState('');

  const languageOptions = useMemo(
    () =>
      LANGUAGE_ORDER.map((lang) => ({
        value: lang,
        label: t(LANGUAGE_LABEL_KEYS[lang]),
      })),
    [t]
  );
  const redirectPath = (location.state as RedirectState | null)?.from?.pathname || '/';
  const setupRequired = setupStatus?.required === true;

  const handleLanguageChange = useCallback(
    (selectedLanguage: string) => {
      if (isSupportedLanguage(selectedLanguage)) setLanguage(selectedLanguage);
    },
    [setLanguage]
  );

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const restored = await restoreSession();
        if (!active) return;
        if (restored) {
          navigate(redirectPath, { replace: true });
          return;
        }
        const status = await apiClient.getManagementSetupStatus();
        if (!active) return;
        setSetupStatus(status);
      } catch (setupError: unknown) {
        if (active) setError(getLocalizedErrorMessage(setupError, t));
      } finally {
        if (active) setRestoring(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [navigate, redirectPath, restoreSession, t]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!email.trim() || !password) {
      setError(t('login.error_required'));
      return;
    }
    if (setupRequired && password.length < 8) {
      setError(t('login.error_password_length'));
      return;
    }
    if (setupRequired && new TextEncoder().encode(password).length > 72) {
      setError(t('login.error_password_too_long'));
      return;
    }
    if (setupRequired && password !== confirmPassword) {
      setError(t('login.error_password_mismatch'));
      return;
    }

    setLoading(true);
    setError('');
    try {
      if (setupRequired) {
        await apiClient.setupManagementAdmin({
          email: email.trim().toLowerCase(),
          password,
          confirm_password: confirmPassword,
        });
      }
      await login({ email, password, rememberSession });
      showNotification(
        setupRequired ? t('login.setup_success') : t('common.connected_status'),
        'success'
      );
      navigate(redirectPath, { replace: true });
    } catch (loginError: unknown) {
      const localizedMessage = getLocalizedErrorMessage(loginError, t);
      setError(localizedMessage);
    } finally {
      setLoading(false);
    }
  };

  if (isAuthenticated && !restoring) {
    return <Navigate to={redirectPath} replace />;
  }

  return (
    <main className={styles.container}>
      <section className={styles.brandPanel} aria-label="CLI Proxy API">
        <img src={INLINE_LOGO_JPEG} alt="CLI Proxy API" className={styles.brandMark} />
        <div className={styles.brandContent}>
          <span className={styles.brandEyebrow}>
            {setupRequired ? 'INITIAL SETUP' : 'CONTROL PLANE'}
          </span>
          <h1>CLI Proxy API</h1>
          <div className={styles.brandRule} />
          <p>{t('splash.subtitle')}</p>
        </div>
      </section>

      <section className={styles.formPanel}>
        {restoring ? (
          <div className={styles.splashContent}>
            <img src={INLINE_LOGO_JPEG} alt="CLI Proxy API" className={styles.splashLogo} />
            <h2 className={styles.splashTitle}>{t('splash.title')}</h2>
            <p className={styles.splashSubtitle}>{t('login.restoring_session')}</p>
            <div className={styles.splashLoader} aria-hidden="true">
              <div className={styles.splashLoaderBar} />
            </div>
          </div>
        ) : (
          <div className={styles.formContent}>
            <header className={styles.mobileBrand}>
              <img src={INLINE_LOGO_JPEG} alt="CLI Proxy API" className={styles.logo} />
              <span>CLI Proxy API</span>
            </header>

            <form
              className={`${styles.loginCard} ${setupRequired ? styles.setupCard : ''}`}
              onSubmit={handleSubmit}
            >
              <div className={styles.loginHeader}>
                <div className={styles.titleRow}>
                  <div>
                    {setupRequired && (
                      <div className={styles.setupBadge}>
                        <IconShield size={14} />
                        <span>{t('login.setup_badge')}</span>
                      </div>
                    )}
                    <h2 className={styles.title}>
                      {setupRequired ? t('login.setup_title') : t('login.welcome')}
                    </h2>
                    <p className={styles.subtitle}>
                      {setupRequired ? t('login.setup_subtitle') : t('login.subtitle')}
                    </p>
                  </div>
                  <Select
                    className={styles.languageSelect}
                    value={language}
                    options={languageOptions}
                    onChange={handleLanguageChange}
                    fullWidth={false}
                    ariaLabel={t('language.switch')}
                  />
                </div>
              </div>

              <Input
                autoFocus
                required
                label={t('login.email_label')}
                placeholder={t('login.email_placeholder')}
                type="email"
                name="email"
                autoComplete="username"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />

              <Input
                required
                label={t('login.password_label')}
                placeholder={
                  setupRequired
                    ? t('login.setup_password_placeholder')
                    : t('login.password_placeholder')
                }
                type={showPassword ? 'text' : 'password'}
                name="password"
                autoComplete={setupRequired ? 'new-password' : 'current-password'}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                rightElement={
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => setShowPassword((visible) => !visible)}
                    aria-label={showPassword ? t('login.hide_password') : t('login.show_password')}
                    title={showPassword ? t('login.hide_password') : t('login.show_password')}
                  >
                    {showPassword ? <IconEyeOff size={16} /> : <IconEye size={16} />}
                  </button>
                }
              />

              {setupRequired && (
                <Input
                  required
                  label={t('login.confirm_password_label')}
                  placeholder={t('login.confirm_password_placeholder')}
                  type={showPassword ? 'text' : 'password'}
                  name="confirm-password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                />
              )}

              {setupRequired && (
                <p className={styles.setupSecurityNote}>{t('login.setup_security_note')}</p>
              )}

              <SelectionCheckbox
                checked={rememberSession}
                onChange={setRememberSession}
                ariaLabel={t('login.remember_session_label')}
                label={t('login.remember_session_label')}
                labelClassName={styles.toggleLabel}
              />

              {error && (
                <div className={styles.errorBox} role="alert">
                  {error}
                </div>
              )}

              <Button type="submit" fullWidth loading={loading}>
                {loading
                  ? setupRequired
                    ? t('login.setup_submitting')
                    : t('login.submitting')
                  : setupRequired
                    ? t('login.setup_submit_button')
                    : t('login.submit_button')}
              </Button>
            </form>
          </div>
        )}
      </section>
    </main>
  );
}
