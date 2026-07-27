import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type { AuthState, ConnectionStatus, LoginCredentials, ServerRuntimeKind } from '@/types';
import { apiClient } from '@/services/api/client';
import { obfuscatedStorage } from '@/services/storage/secureStorage';
import { STORAGE_KEY_AUTH } from '@/utils/constants';
import { detectApiBaseFromLocation, normalizeApiBase } from '@/utils/connection';
import { runtimeApi } from '@/services/api/runtime';
import { useConfigStore } from './useConfigStore';
import { useModelsStore } from './useModelsStore';
import { useQuotaStore } from './useQuotaStore';

interface AuthStoreState extends AuthState {
  connectionStatus: ConnectionStatus;
  login: (credentials: LoginCredentials) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<boolean>;
  restoreSession: () => Promise<boolean>;
  expireSession: () => void;
  updateServerRuntimeKind: (runtimeKind: ServerRuntimeKind) => void;
  updateServerPluginSupport: (supportsPlugin: boolean) => void;
}

let restoreSessionPromise: Promise<boolean> | null = null;

const currentApiBase = () => normalizeApiBase(detectApiBaseFromLocation());

const detectRuntimeKind = async (): Promise<ServerRuntimeKind> => {
  try {
    return await runtimeApi.detectRuntimeKind();
  } catch (error) {
    console.warn('Runtime kind detection failed:', error);
    return 'unknown';
  }
};

const clearRuntimeCaches = () => {
  useConfigStore.getState().clearCache();
  useModelsStore.getState().clearCache();
  useQuotaStore.getState().clearQuotaCache();
};

export const useAuthStore = create<AuthStoreState>()(
  persist(
    (set, get) => ({
      isAuthenticated: false,
      apiBase: '',
      email: '',
      csrfToken: '',
      rememberSession: false,
      serverRuntimeKind: 'unknown',
      supportsPlugin: false,
      connectionStatus: 'disconnected',

      expireSession: () => {
        restoreSessionPromise = null;
        clearRuntimeCaches();
        apiClient.setConfig({ apiBase: currentApiBase(), csrfToken: '' });
        set({
          isAuthenticated: false,
          apiBase: currentApiBase(),
          csrfToken: '',
          serverRuntimeKind: 'unknown',
          supportsPlugin: false,
          connectionStatus: 'disconnected',
        });
      },

      restoreSession: () => {
        if (restoreSessionPromise) return restoreSessionPromise;

        restoreSessionPromise = (async () => {
          const apiBase = currentApiBase();
          apiClient.setConfig({ apiBase, csrfToken: '' });
          set({ apiBase, connectionStatus: 'connecting' });

          try {
            const session = await apiClient.getSession();
            apiClient.setConfig({ apiBase, csrfToken: session.csrf_token });
            set({
              isAuthenticated: true,
              apiBase,
              email: session.email,
              csrfToken: session.csrf_token,
              connectionStatus: 'connected',
            });
            return true;
          } catch {
            set({
              isAuthenticated: false,
              apiBase,
              csrfToken: '',
              connectionStatus: 'disconnected',
              supportsPlugin: false,
            });
            return false;
          }
        })();

        return restoreSessionPromise;
      },

      login: async (credentials) => {
        const apiBase = currentApiBase();
        const email = credentials.email.trim().toLowerCase();
        const rememberSession = credentials.rememberSession ?? get().rememberSession;

        set({
          connectionStatus: 'connecting',
          serverRuntimeKind: 'unknown',
          supportsPlugin: false,
        });
        clearRuntimeCaches();
        apiClient.setConfig({ apiBase, csrfToken: '' });

        try {
          const session = await apiClient.login(email, credentials.password, rememberSession);
          apiClient.setConfig({ apiBase, csrfToken: session.csrf_token });
          await useConfigStore.getState().fetchConfig(true);
          const runtimeKind = await detectRuntimeKind();

          restoreSessionPromise = Promise.resolve(true);
          set({
            isAuthenticated: true,
            apiBase,
            email: session.email,
            csrfToken: session.csrf_token,
            rememberSession,
            connectionStatus: 'connected',
            ...(runtimeKind !== 'unknown' ? { serverRuntimeKind: runtimeKind } : {}),
          });
        } catch (error: unknown) {
          apiClient.setConfig({ apiBase, csrfToken: '' });
          set({
            isAuthenticated: false,
            csrfToken: '',
            connectionStatus: 'error',
          });
          throw error;
        }
      },

      logout: async () => {
        try {
          if (get().isAuthenticated) {
            await apiClient.logout();
          }
        } catch {
          // The local session must still be cleared when the server session already expired.
        } finally {
          get().expireSession();
        }
      },

      checkAuth: async () => {
        const apiBase = currentApiBase();
        try {
          apiClient.setConfig({ apiBase, csrfToken: '' });
          const session = await apiClient.getSession();
          apiClient.setConfig({ apiBase, csrfToken: session.csrf_token });
          const runtimeKind = await detectRuntimeKind();
          set({
            isAuthenticated: true,
            apiBase,
            email: session.email,
            csrfToken: session.csrf_token,
            connectionStatus: 'connected',
            ...(runtimeKind !== 'unknown' ? { serverRuntimeKind: runtimeKind } : {}),
          });
          return true;
        } catch {
          get().expireSession();
          return false;
        }
      },

      updateServerRuntimeKind: (runtimeKind) => {
        set({ serverRuntimeKind: runtimeKind });
      },

      updateServerPluginSupport: (supportsPlugin) => {
        set({ supportsPlugin });
      },
    }),
    {
      name: STORAGE_KEY_AUTH,
      storage: createJSONStorage(() => ({
        getItem: (name) => {
          const data = obfuscatedStorage.getItem<AuthStoreState>(name);
          return data ? JSON.stringify(data) : null;
        },
        setItem: (name, value) => {
          obfuscatedStorage.setItem(name, JSON.parse(value));
        },
        removeItem: (name) => {
          obfuscatedStorage.removeItem(name);
        },
      })),
      partialize: (state) => ({
        email: state.email,
        rememberSession: state.rememberSession,
        serverRuntimeKind: state.serverRuntimeKind,
      }),
    }
  )
);

if (typeof window !== 'undefined') {
  window.addEventListener('unauthorized', () => {
    useAuthStore.getState().expireSession();
  });

  window.addEventListener('server-plugin-support-update', ((event: CustomEvent) => {
    useAuthStore.getState().updateServerPluginSupport(event.detail?.supportsPlugin === true);
  }) as EventListener);
}
