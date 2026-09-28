# Walkthrough: Miniapp Integration with OpenAPI Contracts & Backend

We have migrated the React miniapp from static mocks (`miniapp/src/mocks.ts`) to the live Go backend endpoints defined in [`contracts/openapi.yaml`](contracts/openapi.yaml). All screens now use `openapi-fetch` authenticated via MAX Bridge (`window.WebApp.initData`), directly typed against the generated OpenAPI contracts, with permission-aware card reporting and deletion.

---

## 1. Authentication & MAX Bridge Integration

- **[`miniapp/contracts/index.ts`](miniapp/contracts/index.ts)**:
  - Added an `onRequest` middleware interceptor to `openapi-fetch` that automatically reads `window.WebApp?.initData` from the MAX Bridge library (`https://st.max.ru/js/max-web-app.js`) and attaches it as the `X-Init-Data` HTTP header on all API calls.
  - Exported the configured singleton `api` and `createApiClient`.
- **[`miniapp/src/api.ts`](miniapp/src/api.ts)**:
  - Re-exported the client and types for clean imports across all screens.
- **[`miniapp/src/main.tsx`](miniapp/src/main.tsx)**:
  - Extended global TypeScript `Window.WebApp` declaration to type `initData?: string`.

---

## 2. Contracts & Type Model Consolidation

- **[`miniapp/src/types.ts`](miniapp/src/types.ts)**:
  - Replaced manual, decoupled interfaces with direct type re-exports from [`miniapp/contracts/schema.ts`](miniapp/contracts/schema.ts) as the single source of truth:
    - `Card`, `CardSet`, `TodayData`, `AnswerResult`, `CardIssueReason`, `TableLayout`, `TableItem`, `CardKind`, `CardAnswer`.
- **[`miniapp/src/mocks.ts`](miniapp/src/mocks.ts)**:
  - Completely removed `mocks.ts` from the codebase.

---

## 3. Screen Implementations

- **[`Home.tsx`](miniapp/src/screens/Home.tsx)**:
  - Loads live overview statistics and set lists via `api.GET('/')` (`GetToday`).
  - Gracefully handles loading and offline error retries.
- **[`JoinSet.tsx`](miniapp/src/screens/JoinSet.tsx)**:
  - Enrolls user into a set by 6-digit code via `api.POST('/sets/join', { body: { code } })`.
  - Automatically transitions to the joined set on success or displays an invalid code notice on 400/404.
- **[`SetScreen.tsx`](miniapp/src/screens/SetScreen.tsx)**:
  - Fetches set metadata via `api.GET('/sets/{setId}')` and its cards via `api.GET('/sets/{setId}/cards')`.
  - Deletes/leaves sets via `api.DELETE('/sets/{setId}')`.
  - Passes full `Card` objects upon interaction; deprecated the card undo restore toast.
- **[`Share.tsx`](miniapp/src/screens/Share.tsx)**:
  - Fetches share code details via `api.GET('/sets/{setId}/share')` and copies the invitation text to clipboard.
- **[`Feed.tsx`](miniapp/src/screens/Feed.tsx)**:
  - Fetches set cards via `api.GET('/sets/{setId}/cards')` (when `setId` is given) or adaptive mix via `api.GET('/feed')`.
  - Evaluates user answers immediately client-side for smooth flips and voice support.
  - Submits batch session results via `api.POST('/results', { body: results })` when the session completes.
- **[`CardIssue.tsx`](miniapp/src/screens/CardIssue.tsx) & [`CardEdit.tsx`](miniapp/src/screens/CardEdit.tsx)**:
  - **Ownership checks**:
    - **Non-owners** (members who joined via share code, where `set.authorName` exists): can only report issues via `api.POST('/cards/{cardId}/issue')`. Deletion via `DELETE` and card editing are prevented. If reported during a feed session, the user resumes the feed to continue showing remaining cards.
    - **Owners**: can report and delete via `DELETE /cards/{cardId}` or edit via `api.PUT('/cards/{cardId}')`.
- **[`App.tsx`](miniapp/src/App.tsx)**:
  - Orchestrates screen routing, ownership checks, card state propagation, and feed continuation.

---

## 4. Verification & Quality Gates

### Automated Verification
```bash
# 1. Type checking and production bundle build
cd miniapp && bun run build
# Output:
# ✓ 53 modules transformed.
# dist/index.html                   0.55 kB │ gzip:  0.36 kB
# dist/assets/index-CzI87YKP.css   74.25 kB │ gzip: 11.65 kB
# dist/assets/index-D97xgyjh.js   309.89 kB │ gzip: 93.45 kB
# ✓ built in 178ms

# 2. Linting (both miniapp ESLint and Go golangci-lint)
task lint
# miniapp: eslint . -> 0 issues
# backend: golangci-lint run ./... -> 0 issues

# 3. Unit & Integration Tests
task test
# All backend HTTP transport, usecase, bot, and repository tests pass cleanly.
```
