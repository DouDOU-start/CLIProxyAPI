# Integrated Management Console

This directory contains the source for the management console embedded into the CLIProxyAPI binary.

The initial source was imported from [`router-for-me/Cli-Proxy-API-Management-Center`](https://github.com/router-for-me/Cli-Proxy-API-Management-Center) at commit `3738c0b7ff21ce7e1423795a26769fff05fd81d6`. It was then adapted to use the same-origin email/password session API provided by this repository.

## Development

```bash
make dev-backend
make web-dev
```

The Vite server runs on port `5173` and proxies `/v0` to the backend on port `8317`.

## Production Build

```bash
make web-build
```

Vite emits a single `dist/index.html`. [`embed.go`](embed.go) embeds that file into the Go binary, so production deployments do not download or mount a separate management page.

## Authentication Contract

- The console only connects to the backend that served the page.
- Login creates an HttpOnly, SameSite=Strict session cookie.
- Passwords and session tokens are never stored in browser storage.
- The backend returns a per-session CSRF token, which the console sends with management API requests.
