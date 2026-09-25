# Repository Guidelines for Agents

## Commit Messages

- All commit messages **MUST** follow the [Conventional Commits](https://www.conventionalcommits.org/) format in English.
- Use lowercase types: `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `chore:`, `ci:`.
- Keep descriptions short, concise, and written in the imperative mood (e.g., `feat: add max bot service`, `fix: update docker deploy path`).
- Do not add `Co-Authored-By` or AI assistant trailers to commit messages.

## Code & Quality Standards

- Always run `task lint` before committing (verifies both frontend ESLint and backend `golangci-lint`).
- Always run `task test` to ensure backend unit tests pass.
- Maintain existing architecture:
  - `miniapp/`: React 19 + TypeScript + Vite frontend powered by Bun and `@maxhub/max-ui`.
  - `backend/`: Go 1.25 HTTP server serving embedded SPA assets and handling the MAX Bot API service.
  - Deployments: Docker Compose with Traefik reverse proxy and Let's Encrypt TLS.

## Codegen & Contracts

- Single source of truth for API contracts is `contracts/openapi.yaml`.
- Run `task gen` (or `task contracts:gen-openapi`) to regenerate:
  - Backend Go server contracts: `backend/contracts/` (via `ogen`).
  - Frontend TypeScript client: `miniapp/contracts/` (via `openapi-typescript-codegen`).
- Generated contracts are committed to git; always run `task gen`, `task lint`, and `task test` when modifying contracts.
- Generated contract paths are marked with `linguist-generated=true` in `.gitattributes` to collapse them in PR review diffs.
