# Repository guidance

## Project

This repository contains ToneShelf, a server-hosted, multi-device music player service. Local Windows execution is only the initial development and debugging setup. The service must remain deployable to Windows x64, Linux x64, Linux ARM64, and NAS environments. Phone and desktop browsers access the same server-managed music library and play audio on their own devices.

The repository is documentation-only at present. The planned implementation is a Go server plus a React/TypeScript web client, with platform-neutral client logic reserved for a future React Native client.

## Sources of truth

Read the applicable sources before changing behavior or architecture:

1. `docs/product-requirements.md` owns product behavior, scope, and acceptance scenarios.
2. `docs/technical-design.md` owns architecture, technology choices, storage, deployment, and platform boundaries.
3. `README.md` is a concise project overview.

Do not duplicate these documents in code comments or other guidance. If they conflict, identify the conflict and resolve it explicitly instead of silently choosing one.

## Planned repository boundaries

Use one Git repository with independently buildable applications:

```text
apps/
  server/        Go server
  web/           React web client
  mobile/        Future React Native client; not part of the initial implementation
packages/        Platform-neutral TypeScript packages shared by web and future mobile clients
api/             API contracts when introduced
docs/            Product and technical documentation
scripts/         Cross-platform development, build, backup, and release scripts
```

The server and clients may be built and released separately even though they share a repository.

## Architecture constraints

- Keep the Go server portable across Windows and Linux. Do not hard-code path separators, drive letters, usernames, process paths, or Windows-only filesystem behavior.
- Support Windows x64, Linux x64, and Linux ARM64 release artifacts.
- Keep the SQLite WAL database on a local filesystem or equivalent local container volume, never on SMB or NFS.
- Treat uploaded names and metadata as untrusted input. Server-generated IDs must determine storage paths.
- Preserve atomic file-version replacement, quota accounting, delayed trash cleanup, and recoverable background jobs described in the technical design.
- The initial client is responsive React DOM for phone and desktop browsers. Do not introduce React Native or React Native Web unless explicitly requested.
- Keep shared client domain types, API access, validation, random-selection policy, and playback policy platform-neutral.
- Browser globals and browser-only types such as `window`, `document`, `HTMLAudioElement`, `File`, and `sessionStorage` belong behind Web adapters and must not leak into shared packages.
- Use explicit platform interfaces or equivalent boundaries for audio playback, session persistence, file selection, and application lifecycle.
- Treat the server as the authority for music, lyrics, playlists, quota, and task state. Browser storage is only for the current playback session.
- Bind to loopback by default until remote-access authentication, proxy, and TLS requirements are explicitly designed.

## Change discipline

- Inspect the current worktree before editing and preserve unrelated user changes.
- Keep each change scoped to one coherent requirement. Do not implement adjacent features without authorization.
- Update product or technical documentation when a change alters their owned decisions.
- Database schema changes must use versioned migrations; never mutate a user database ad hoc.
- Do not commit media files, databases, generated playback files, credentials, secrets, logs, backups, or user data.
- Do not claim a check passed unless it was run and its result was observed.
- Prefer focused tests for changed behavior, followed by the relevant broader checks.

## Verification

Use the repository's actual scripts and manifests once they exist. Do not invent successful commands for unscaffolded components.

- Documentation-only changes: run `git diff --check` and inspect links, headings, terminology, and source-of-truth consistency.
- Go changes: format changed Go files and run the applicable focused tests, then `go test ./...`; run `go vet ./...` when the module is present and the change warrants it.
- TypeScript changes: run the package-manager scripts for type checking, linting, tests, and production build that exist in the relevant workspace.
- API changes: verify the server contract and every affected client together.
- Storage, migrations, deletion, replacement, quota, and recovery changes require failure-path and restart/recovery coverage, not only happy-path tests.
- Cross-platform filesystem or process changes require reasoning or tests for both Windows and Linux behavior.

## Git workflow

- After one coherent user requirement is fully implemented and all relevant verification has passed, automatically create a local Git commit without waiting for an additional request.
- Never push automatically. A push always requires an explicit user request.
- Stage only files that belong to the completed requirement. Do not include unrelated pre-existing worktree changes.
- Write commit subjects and bodies in concise Chinese by default. Keep technical identifiers and proper nouns in their conventional form when translating them would reduce clarity.
- Use the lightweight Conventional Commits form `<type>(<optional scope>): <简短中文描述>`.
- Use `feat` for user-visible functionality, `fix` for bug fixes, `docs` for documentation-only changes, `refactor` for behavior-preserving restructuring, `test` for test-only changes, `build` for build or dependency changes, `ci` for CI changes, and `chore` only when no more specific type fits.
- Omit the scope unless it adds useful context; when useful, prefer stable scopes such as `server`, `web`, `mobile`, `api`, or `docs`.
- Mark breaking changes with `!` before the colon and explain the impact in the body or a `BREAKING CHANGE:` footer.
- Keep the description as short and specific as practical, using an action-oriented summary. Add a body only when the rationale or migration impact cannot be understood from the subject and diff.
- Do not commit when relevant verification failed, was interrupted, or remains incomplete. Report the blocker and leave the verified state visible in the worktree.
- Treat several implementation steps for one requirement as one commit rather than committing every intermediate edit.
- When the completed work is only a narrow refinement, correction, or follow-up to `HEAD`, automatically amend it into `HEAD` instead of creating a new commit, but only when `HEAD` is confirmed local and unpushed and the amendment contains no unrelated changes.
- If `HEAD` may already be pushed or shared, its publication state is uncertain, or the new work is independently meaningful, create a new commit instead of rewriting history.
- When amending, preserve the existing subject unless the amendment changes what the commit fundamentally represents; keep any revised first line concise.
- After committing or amending, report the commit hash, subject, verification performed, and whether the worktree is clean.
