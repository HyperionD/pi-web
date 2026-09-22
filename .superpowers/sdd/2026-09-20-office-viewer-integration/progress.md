# SDD ledger — plan: docs/superpowers/plans/2026-09-20-office-viewer-integration.md

## Preflight scan

| Item | Shared interface/files | Finding | Ruling |
|---|---|---|---|
| Task 1 → Task 2 | `static/file-viewer/`, dependency versions | Task 1 stages assets and Task 2 consumes their manifest/paths; no contradiction. | Proceed; Task 2 must use the manifest path contract. |
| Task 2 → Task 3 | `mountOfficePreview`, `OfficePreviewHandle` | Task 3 owns UI lifecycle while Task 2 owns viewer setup/destroy; boundaries are explicit. | Proceed. |
| Task 3 → Task 4 | raw URL and download action | Task 4 strengthens the existing raw contract without adding a proxy. | Proceed. |
| Task 3 → Task 5 | `data-file-preview-office`, error/download hooks | Browser tests consume stable hooks created by Task 3. | Proceed. |
| Task 5 → Task 6 | generated assets and test results | Task 6 synchronizes only after compatibility tests. | Proceed. |
| Task 1 | asset script/test and package dependency | Self-consistent; staged asset selection is narrower than full demo assets. | Proceed. |
| Task 2 | adapter API/test | Self-consistent; dynamic import and credentials are testable. | Proceed. |
| Task 3 | preview lifecycle/tests | Self-consistent; stale identity and destroy behavior are explicit. | Proceed. |
| Task 4 | backend tests | Self-consistent; existing implementation may already pass. | Proceed without unnecessary backend changes. |
| Task 5 | browser fixtures/config | Self-consistent; Chrome 107 project is additive. | Proceed. |
| Task 6 | build/embed validation | Self-consistent; generated assets remain scoped. | Proceed. |

## Rulings

- Ruling: use the existing functional branch/worktree instead of creating another worktree — the user explicitly requested execution on the current feature branch, and it is not `main`; cost if wrong is reduced isolation, recoverable through git commits.
- Ruling: use the published package version matching the reviewed repository (`3.1.2`) — this keeps renderer, core, Worker and asset versions aligned; cost if wrong is a dependency availability or API mismatch discovered during Task 1.

Task 1: complete (commits 30cafcff..021662ae, review clean after 3 fix rounds)
Task 2: complete (`mountOfficePreview` adapter, dynamic imports, fetch credentials/abort, lifecycle tests)
Task 3: complete (Office routing, stale request cleanup, error fallback, panel styling/tests)
Task 4: complete (DOCX raw MIME, range/download headers, traversal regression test)
Task 5: complete (self-hosted asset smoke test; optional `PI_WEB_CHROME107_PATH` Playwright project)
Task 6: complete (12 MiB staged assets, `build:single`, frontend/backend checks; Chrome 107 binary was not available locally)
