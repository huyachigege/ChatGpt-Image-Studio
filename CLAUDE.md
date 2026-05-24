# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ChatGpt Image Studio is delivered as one Go backend binary plus a built Vite/React frontend. The backend serves the frontend from `backend/static`, creates runtime config under `backend/data`, and exposes both admin/workspace APIs and OpenAI-compatible image APIs.

Primary components:

- `backend/`: Go service (`module chatgpt2api`) for HTTP routing, image workflows, account pool management, config, sync integrations, request logs, and static asset serving.
- `web/`: Vite + React 19 frontend; production build outputs to `web/dist` and is copied/synced into `backend/static`.
- `scripts/`: Cross-platform dev, check, build, and Docker update scripts. Prefer these scripts for full-project workflows.

Runtime requirements from README: Go `1.25+`, Node.js `24+`, npm `10+`.

## Common Commands

Run from repository root unless noted.

### Full-project workflows

Windows PowerShell:

```powershell
./scripts/dev.ps1
./scripts/check.ps1
./scripts/build.ps1
```

macOS / Linux:

```bash
chmod +x ./scripts/*.sh
./scripts/dev.sh
./scripts/check.sh
./scripts/build.sh
```

What they do:

- `dev`: runs `npm ci`, builds frontend, starts `npm run build:watch` to keep `backend/static` synced, then starts backend with `go run .` from `backend/`.
- `check`: runs backend tests, frontend type check, lint, and production build.
- `build`: builds frontend, syncs static assets, builds the Go binary with build metadata, and creates `dist/package`.

Optional image-mode compatibility black-box test:

```powershell
$env:RUN_IMAGE_MODE_COMPAT_TESTS = "1"
./scripts/check.ps1
```

```bash
RUN_IMAGE_MODE_COMPAT_TESTS=1 ./scripts/check.sh
```

### Backend commands

```powershell
cd backend
go run .
go test ./...
go test ./api -run TestImageModeCompatibilityBlackBox -count=1
go test ./api -run TestName -count=1
```

Use `go test ./internal/config -run TestName -count=1` for a single package/test under `backend/internal/...`.

### Frontend commands

```powershell
cd web
npm ci
npm run dev
npm run build
npm run build:watch
npm run lint
npm run test
npx tsc --noEmit
npx vitest run src/app/image/task-runtime.test.ts
npx vitest run src/app/image/task-runtime.test.ts -t "case name"
```

The repository uses npm scripts in automation; do not switch package managers just because `bun.lock` exists.

## Backend Architecture

Startup path:

1. `backend/main.go` creates a `config.Config`, ensures runtime config files exist, loads defaults plus `data/config.toml`, applies environment overrides, optionally overlays Redis config when `storage.config_backend=redis`, validates static assets, creates the account store, then starts `api.SetupRouter`.
2. `backend/api/router.go` delegates to `NewServer(...).Handler()`.
3. `backend/api/server.go` owns shared runtime state: config, account store, sync clients, request logs, image admission control, image task manager, and upstream image client factories.

HTTP routes are registered in `Server.Handler()` in three broad groups:

- Public/system: `/auth/login`, `/auth/register`, `/version`, `/health`, and web app fallback.
- Admin/workspace: `/api/accounts`, `/api/config`, `/api/sync`, `/api/requests`, `/api/image/conversations`, `/api/image/tasks`, `/api/favorites/{type}`, gallery, quota, diagnostics, users, and announcement APIs. Admin routes use `requireAdminAuth`; workspace routes use `requireWorkspaceAuth`.
- OpenAI-compatible image APIs: `/v1/images/generations`, `/v1/images/edits`, `/v1/chat/completions`, `/v1/responses`, `/v1/models`, and image file serving. These use `requireImageAuth`.

Image workflow shape:

- Frontend workspace creates queued tasks through `/api/image/tasks`.
- `api/image_task_manager.go` manages task lifecycle, queueing, concurrency, SSE subscribers, cancellation, retry backoff, and task snapshots.
- Task execution routes through account selection/admission, then one of the image clients: official ChatGPT legacy client, Responses client, or CPA route-aware client.
- Generated image metadata is represented via `internal/imagehistory`; image bytes/files are served through `/v1/files/image...` routes.

Storage/config shape:

- `internal/config` defines TOML-backed runtime config sections including app, server, chatgpt, accounts, storage, sync, proxy, CPA, NewAPI, and Sub2API.
- Account storage supports local/current, SQLite, and Redis paths via `internal/accounts`.
- User login/session/quota data lives in SQLite through `internal/users`; per-user template/image favorites are stored in `app_user_favorites`.
- Config storage can be file or Redis (`internal/configstore`).
- Image conversation/data modes can be browser or server, controlled by storage config and reflected in frontend store behavior.

External integration packages:

- `internal/cliproxy`, `internal/newapi`, and `internal/sub2api` are source/sync integrations.
- `handler/` contains upstream ChatGPT/Responses client and transport logic.
- `internal/outboundproxy` centralizes outbound proxy behavior.

## Frontend Architecture

Entry/routing:

- `web/src/main.tsx` mounts React under `BrowserRouter`.
- `web/src/App.tsx` defines routes for home, login, image workspace/history/gallery, accounts, settings, startup check, and request logs.
- `web/src/app/layout.tsx` wraps routed pages with the app shell/navigation.

API and auth:

- `web/src/lib/request.ts` owns the Axios instance, base URL normalization, auth bearer injection from localforage, `401` redirect behavior, and normalized `ApiError` handling.
- `web/src/lib/api.ts` defines API-facing types and functions used by pages/hooks.
- `web/src/store/auth.ts` persists the auth key and user in localforage.

Image workspace state:

- `web/src/app/image/page.tsx` composes the image workspace.
- Image-specific hooks live under `web/src/app/image/hooks/`; `use-image-submit.ts` builds conversation turns and creates backend image tasks.
- `web/src/store/image-conversations.ts` manages conversation history, switching between browser localforage storage and server `/api/image/conversations` storage.
- `web/src/app/image/task-runtime.ts` contains task runtime behavior and has a Vitest test at `web/src/app/image/task-runtime.test.ts`.

Styling/build:

- Vite config uses React, Tailwind CSS v4 plugin, alias `@ -> web/src`, and a build-only plugin that syncs `web/dist` to `backend/static`.
- ESLint is flat config with TypeScript, React Hooks, React Refresh, and Prettier config; `@typescript-eslint/no-unused-vars` and `no-explicit-any` are disabled.

## Local Data and Generated Files

Common generated/runtime paths are intentionally local and should not be treated as source:

- `backend/data/config.toml`
- `backend/data/config.example.toml`
- `backend/data/accounts_state.json`
- `backend/data/auths/*.json`
- `backend/data/sync_state/*.json`
- `backend/data/tmp/`
- `backend/data/last-startup-error.txt`
- `backend/static/`
- `web/dist/`
- `dist/`
- `web/vite-watch.*.log`

## Notes for Changes

- For full-stack changes touching frontend output, remember that `npm run build` or the provided scripts sync `web/dist` into `backend/static`.
- For UI changes, run the frontend/backend locally and verify the relevant route in a browser when possible; type checks and builds are not enough to verify behavior.
- Keep API contract changes synchronized between `backend/api/*` handlers/types and `web/src/lib/api.ts` plus affected stores/hooks.
