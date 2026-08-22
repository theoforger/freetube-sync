# freetube-sync — Implementation Tasks

Work through stages in order. Each stage has a **Deliverable** — don't move
to the next stage until it's met and tests pass. See `CLAUDE.md` for
invariants, architecture, and conventions that apply throughout.

## Stage 0 — Project scaffolding

- [x] Init Go module `freetube-sync`.
- [x] `main.go` with subcommand dispatch on `os.Args[1]` (`serve`, `run`,
      `sync`, `status`, `inspect`) using stdlib `flag` per subcommand — no
      CLI framework dependency.
- [x] `internal/config`: resolve server URL / token / data dir from flags →
      env → `~/.config/freetube-sync/config.json`, in that precedence order.
- [x] `--version` flag.
- [x] `status` subcommand (stub output OK for now).

**Deliverable:** binary builds, parses subcommands, prints resolved config.

---

## Stage 1 — NeDB read/write layer

- [x] `internal/installdetect`: detect Flatpak (`flatpak info
      io.freetubeapp.FreeTube` or dir presence) vs. native
      (`~/.config/FreeTube/` + resolvable `freetube` binary).
- [x] If both found: prompt to choose, no silent default; persist choice to
      config. `--install` flag overrides per-invocation.
- [x] If neither found: hard error with a clear message.
- [x] Expose resolved `DBPath` and `LaunchCommand` from one detection call.
- [x] `internal/nedb`: parse `profiles.db` line-by-line into
      `[]map[string]interface{}` (schema-agnostic).
- [x] Locate "All Channels" profile; expose `GetSubscriptions()` /
      `SetSubscriptions()`.
- [x] Write path: temp file → `fsync` → backup existing file to
      `profiles.db.freetube-sync-bak` → atomic `rename`.
- [x] Round-trip test: parse a fixture, write it back unchanged, diff
      byte-for-byte (except subscriptions field if modified).
- [x] Add a real, scrubbed `profiles.db` fixture under `testdata/`.
- [x] Unit tests: add/remove subscriptions, verify other profiles/fields
      untouched, verify backup file created, verify atomic write (no partial
      file on simulated failure).

**Deliverable:** `freetube-sync inspect --db testdata/profiles.db` prints
parsed subscriptions correctly. No network code involved yet.

---

## Stage 2 — Safety checks (process/lockfile guard)

- [x] `internal/guard`: check `SingletonLock` file presence in the same
      config dir as the detected `profiles.db`.
- [x] Also check `/proc` for a running FreeTube/Flatpak process as a second
      signal.
- [x] Expose `IsSafeToWrite() (bool, error)`.
- [x] Tests: simulate lockfile present/absent, process present/absent.

**Deliverable:** `freetube-sync status` reports "safe to sync" or "FreeTube
is running, skipping."

---

## Stage 3 — Merge logic (pure, no I/O)

- [x] `internal/merge`: implement the LWW-element-set merge —
      `{channelId, lastAdded, lastRemoved}` in, merged set out.
- [x] Table-driven tests: add/add, add-vs-remove race (both orders),
      remove/remove, one-sided-empty, stale/older timestamps, empty input
      sets, large sets (perf sanity check only, not a real concern at this
      scale).
- [x] Confirm the function is commutative and idempotent via property-style
      tests (apply merge in both argument orders and twice in a row; results
      must be identical).

**Deliverable:** 100% branch coverage on `internal/merge`, no server or I/O
code involved.

---

## Stage 4 — Server

- [x] `internal/server`: `POST /sync` handler. Request: list of
      `{channelId, action}` events (no timestamps). Response: current
      authoritative subscribed channel ID list.
- [x] Server stamps `lastAdded`/`lastRemoved` on arrival — never trust a
      timestamp from the request body if one is ever present.
- [x] Bearer token middleware, checked against `--token`.
- [x] Storage: single canonical state file under `--data`, atomic write
      (temp + rename, matching Stage 1's pattern).
- [x] Per-process mutex around read-modify-write of the state file.
- [x] Optional `X-Device-Id` header accepted and logged, not used in merge
      logic.
- [x] `serve` subcommand wires handler into `http.ListenAndServe`.
- [x] `log/slog` structured logging on every sync request (device id if
      present, event count, resulting subscribed count).
- [x] Integration test: spin up server in-process, POST events via
      `httptest`, assert merged state.

**Deliverable:** `freetube-sync serve` runs standalone; testable with
`curl`.

---

## Stage 5 — Client sync command

- [x] Local shadow snapshot: `~/.config/freetube-sync/last-synced.json`
      holding the last successfully-synced subscription set.
- [x] `sync` subcommand: guard check → read `profiles.db` subscriptions →
      diff against shadow snapshot → compute add/remove events → POST to
      server → overwrite `profiles.db` subscriptions with response →
      overwrite shadow snapshot.
- [x] Fail open: on any network error, log a warning and exit 0 (not an
      error) — local state must be left untouched.
- [x] Tests: first-run (no shadow snapshot yet), no-op sync (nothing
      changed), local-only changes, remote-only changes, both changed,
      server unreachable.

**Deliverable:** two local test directories (simulating two devices) synced
successfully against one running `serve` instance.

---

## Stage 6 — `run` wrapper

- [x] `run` subcommand: guard check → `sync` (fail open) → `os/exec` the
      detected/overridden launch command, inherited stdio → wait for exit →
      `sync` again (fail open).
- [x] Explicit `-- <command>` after flags overrides detected launch command.
- [x] Test: `run` against a fake "FreeTube" (a short-lived test binary/shell
      script) confirms sync-before and sync-after both fire, and that a
      down server doesn't prevent the fake binary from launching.

**Deliverable:** `freetube-sync run` launches FreeTube (Flatpak or native,
no flag needed in the common case) and syncs on both ends of the session.

---

## Stage 7 — Docker setup

- [x] Multi-stage `Dockerfile`: build stage `CGO_ENABLED=0` static compile,
      final stage `distroless/static` or `scratch`.
- [x] `compose.yaml` (default): single service, port published
      directly, bind-mounted `--data` volume, `TOKEN`/`LISTEN` via `.env`,
      `restart: unless-stopped`. Commit `.env.example`, gitignore `.env`.
- [x] `compose.caddy.yaml` (standalone, opt-in — not layered with
      `compose.yaml`): its own `caddy` service, `Caddyfile` with
      `reverse_proxy freetube-sync:8080` + automatic HTTPS, `freetube-sync`
      not port-published directly. Run with
      `docker compose -f compose.caddy.yaml up -d`.
- [x] README section: default = trusted network only (Tailscale/WireGuard/
      LAN); `compose.caddy.yaml` = safe for public exposure.

**Deliverable:** `docker compose up -d` runs the server; `compose.caddy.yaml`
verified separately with a real or local domain.

> Note: Docker isn't installed in the environment this was built in, so
> the compose files are syntax-validated (YAML parses) but not build/run-
> verified end to end. Worth an actual `docker compose up -d` smoke test
> before relying on this in production.

---

## Stage 8 — Desktop alias script

- [x] `scripts/install-client.sh`: detect Flatpak vs. native in shell
      (independent of the Go binary), matching `internal/installdetect`'s
      logic.
- [x] Both present → prompt, save choice to
      `~/.config/freetube-sync/config.json`.
- [x] Neither present → error out, no broken desktop entry written.
- [x] Write `~/.local/share/applications/freetube-synced.desktop` with the
      correct `Exec=` and icon/name sourced from the real FreeTube
      `.desktop` file (Flatpak export path or native applications dir).
- [x] Copy `freetube-sync` binary to `~/.local/bin`.
- [x] Prompt for/read server URL + token, write to
      `~/.config/freetube-sync/config.json` (not baked into `Exec=`).
- [x] Idempotent — safe to re-run, including after switching install types.
- [x] `scripts/uninstall-client.sh`: removes desktop entry, binary, config.
- [x] Requires a locally-built `freetube-sync` binary to copy from (Stage 10,
      if built later, can replace this with a downloaded release binary —
      not required for this stage to be complete).

**Deliverable:** running the script once on a real device produces a working
launcher icon that transparently syncs, correct for whichever install type
is present.

---

## Stage 9 — Hardening & polish

- [x] Optional systemd user timer unit for periodic `sync`-only runs,
      skipped if FreeTube is running (guard handles this automatically).
- [x] Config validation with clear error messages (missing token, unreachable
      server URL format, etc.).
- [x] `--dry-run` flag for `sync`/`run`: compute and print the merge result
      without writing anything.
- [x] README: setup walkthrough, token rotation steps, explanation of
      conflict behavior (link to `CLAUDE.md`'s sync protocol section or
      inline summary).

**Deliverable:** project is usable end-to-end by someone other than the
person who built it, following just the README.

---

## Stage 10 (optional) — Release & CI

Not part of the default build — only work on this stage if explicitly
requested. Stages 0–9 form the complete, functional project on their own.

- [ ] GitHub Actions workflow (`go test ./...`, `go vet`) on every push/PR.
- [ ] Separate release workflow, triggered on tag push: cross-compile static
      binaries for `linux/amd64` and `linux/arm64`, publish to GitHub
      Releases with checksums.
- [ ] Same workflow builds and pushes the Docker image to GHCR.
- [ ] Update `install-client.sh` to download the matching release binary
      instead of requiring a local build (do this once Stage 6 is stable —
      see `CLAUDE.md`'s Build order note).

**Deliverable:** a tagged release produces downloadable binaries and a
published Docker image with no manual build step required.
