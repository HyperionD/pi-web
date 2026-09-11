# Backend layout

The backend is a Go server embedded into the root `pi-web` binary package.

```text
.
├── *.go                     # CLI, version command, binary entrypoint
├── internal/piweb/          # HTTP API, pi runner, session/workspace store
└── static/                  # embedded Astro build for release binaries
```

## Rules

- Root Go files own process startup, CLI flags, version output, and static embedding.
- `internal/piweb` owns testable server behavior and workspace/session domain logic.
- Static assets are committed under `static` so release binaries include a complete UI.
- Keep Go tests next to the file/feature they cover.

See `../internal/piweb/README.md` for the internal package map.

Release binaries are built from the Astro output embedded in `static/`. Before a release, run `bun run build:single`, verify the generated assets and execute the release checklist under `docs/releases/`.
