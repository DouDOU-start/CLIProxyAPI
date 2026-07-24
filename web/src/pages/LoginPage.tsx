import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import { IconEye, IconEyeOff } from '@/components/ui/icons';
import { useAuthStore, useLanguageStore, useNotificationStore } from '@/stores';
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
  const [showPassword, setShowPassword] = useState(false);
  const [rememberSession, setRememberSession] = useState(storedRememberSession);
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
  const redirectPath =
    (location.state as RedirectState | null)?.from?.pathname || '/';

  const handleLanguageChange = useCallback(
    (selectedLanguage: string) => {
      if (isSupportedLanguage(selectedLanguage)) setLanguage(selectedLanguage);
    },
    [setLanguage]
  );

  useEffect(() => {
    let active = true;
    void restoreSession()
      .then((restored) => {
        if (active && restored) navigate(redirectPath, { replace: true });
      })
      .finally(() => {
        if (active) setRestoring(false);
      });
    return () => {
      active = false;
    };
  }, [navigate, redirectPath, restoreSession]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!email.trim() || !password) {
      setError(t('login.error_required'));
      return;
    }

    setLoading(true);
    setError('');
    try {
      await login({ email, password, rememberSession });
      showNotification(t('common.connected_status'), 'success');
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
          <span className={styles.brandEyebrow}>CONTROL PLANE</span>
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

            <form className={styles.loginCard} onSubmit={handleSubmit}>
              <div className={styles.loginHeader}>
                <div className={styles.titleRow}>
                  <div>
                    <h2 className={styles.title}>{t('login.welcome')}</h2>
                    <p className={styles.subtitle}>{t('login.subtitle')}</p>
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
                placeholder={t('login.password_placeholder')}
                type={showPassword ? 'text' : 'password'}
                name="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                rightElement={
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => setShowPassword((visible) => !visible)}
                    aria-label={
                      showPassword ? t('login.hide_password') : t('login.show_password')
                    }
                    title={showPassword ? t('login.hide_password') : t('login.show_password')}
                  >
                    {showPassword ? <IconEyeOff size={16} /> : <IconEye size={16} />}
                  </button>
                }
              />

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
                {loading ? t('login.submitting') : t('login.submit_button')}
              </Button>
            </form>
          </div>
        )}
      </section>
    </main>
  );
}
