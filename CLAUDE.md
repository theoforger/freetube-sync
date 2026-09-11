# freetube-sync

A single Go binary that syncs FreeTube subscriptions across devices, acting
as both server and client depending on the subcommand invoked.

See `TASKS.md` for the staged implementation checklist — work through it in
order and check items off as they're completed. Don't skip ahead to later
stages before earlier ones have passing tests; the ordering is deliberate
(see "Build order" at the bottom of this file).

## Non-negotiable invariants

These rules apply to every change in this codebase, regardless of what stage
is being worked on. Violating them is a correctness bug, not a style issue.

1. **Never write to `profiles.db` while FreeTube is running.** Every code
   path that touches the file must call the guard (`internal/guard`) first
   and abort if it's unsafe.
2. **Every write to `profiles.db` is atomic.** Write to a temp file, `fsync`,
   `rename` over the original. No code path may write in place or leave a
   partial-write window.
3. **Back up before overwrite.** Copy the existing `profiles.db` to
   `profiles.db.freetube-sync-bak` before the atomic rename.
4. **Preserve everything not related to subscriptions.** `profiles.db`
   contains other profiles, settings, etc. Parse it schema-agnostically
   (`map[string]interface{}` per line) and only ever mutate the "All
   Channels" profile's `subscriptions` field. Every other line/field must
   round-trip byte-for-byte.
5. **Clients never store or send timestamps.** Only the server assigns
   `lastAdded`/`lastRemoved` timestamps (on arrival). Clients send plain
   add/remove events computed by diffing current `profiles.db` state against
   a local shadow snapshot (`~/.config/freetube-sync/last-synced.json`).
   This exists specifically to prevent client clock skew from corrupting
   merge results — do not "simplify" this by having clients send their own
   timestamps.
6. **Network/server failures fail open.** A sync failure must never prevent
   FreeTube from launching or closing normally. Log and continue.
7. **No third-party dependencies unless there's no reasonable stdlib path.**
   `net/http`, `encoding/json`, `os/exec`, `flag`, `log/slog` cover
   essentially everything this project needs. Justify any addition to
   `go.mod` in the commit message.

## Architecture

One binary, mode selected by subcommand:

```
freetube-sync serve    --listen :8080 --data /var/lib/freetube-sync --token <secret>
freetube-sync run      --server https://host:8080 --token <secret> [--install=flatpak|native]
freetube-sync sync     --server https://host:8080 --token <secret> [--install=flatpak|native]
freetube-sync status   [--install=flatpak|native]
freetube-sync inspect  --db path/to/profiles.db
```

- `serve` — the server role. One HTTP endpoint (`POST /sync`), single-tenant
  (no `userId`/multi-tenancy), one canonical state file under `--data`.
- `run` — the daily-use client wrapper: sync → exec FreeTube → wait → sync.
  This is what the desktop alias (Stage 8) invokes.
- `sync` — one-shot client sync, no launch. Used standalone and by `run`.
- `status` / `inspect` — diagnostics, no side effects.

### Repo layout

Follows [golang-standards/project-layout](https://github.com/golang-standards/project-layout).

```
cmd/freetube-sync/main.go        # subcommand dispatch
cmd/freetube-sync/{serve,run,sync,status,inspect}.go  # one file per subcommand
cmd/freetube-sync/install.go     # shared install-detection prompt/persist glue
internal/config/                 # flag/env/config-file resolution
internal/installdetect/          # Flatpak vs native detection, db path + launch cmd
internal/nedb/                   # profiles.db read/write (line-delimited JSON)
internal/guard/                  # "is FreeTube running" check
internal/merge/                  # pure LWW-element-set merge logic (no I/O)
internal/server/                 # HTTP handler + state storage
internal/shadow/                 # client's local shadow snapshot (last-synced.json)
internal/clientsync/             # client sync cycle: guard -> diff -> POST -> overwrite
internal/runner/                 # `run` subcommand's sync -> exec -> sync cycle
scripts/install-client.sh        # desktop alias installer
scripts/uninstall-client.sh
compose.yaml                     # default, no reverse proxy
compose.caddy.yaml               # standalone alternative, with Caddy for TLS
compose.override.yaml            # local network override for actual deployment
Caddyfile
deployments/Dockerfile           # build: context is repo root, so this stays put
.env.example
init/systemd/                    # optional systemd user service + timer
test/profiles.db                 # real, scrubbed fixture (Stage 1)
```

`cmd/freetube-sync` is the only package importable as `main`; everything
under `internal/` is private to this module (standard Go enforcement, not
just convention). Package names are single lowercase words, no
underscores/mixedCaps (`clientsync`, `installdetect`, not `client_sync` or
`installDetect`) — keep new packages consistent with that.

### Install detection (Flatpak vs. native)

`internal/installdetect` resolves, once, both the `profiles.db` path and the
launch command:

- **Flatpak**: `flatpak info io.freetubeapp.FreeTube` succeeds, or
  `~/.var/app/io.freetubeapp.FreeTube/` exists →
  db path `~/.var/app/io.freetubeapp.FreeTube/config/FreeTube/profiles.db`,
  launch command `flatpak run io.freetubeapp.FreeTube`.
- **Native Linux**: `~/.config/FreeTube/` exists and a `freetube` binary is
  resolvable → db path `~/.config/FreeTube/profiles.db`, launch command =
  resolved binary path.
- **Both present**: prompt the user to choose — no silent default. Persist
  the choice to `~/.config/freetube-sync/config.json`. `--install` flag
  overrides for a single invocation.
- **Neither found**: hard error.

`scripts/install-client.sh` mirrors this same detection logic in shell
(independently — it must not require the Go binary to already be built) to
decide which `.desktop` file to source the icon/name from and what `Exec=`
to write.

### Sync protocol

- `POST /sync` body: list of `{channelId, action: "add"|"remove"}` events —
  no timestamps.
- Server stamps `lastAdded`/`lastRemoved` on arrival using its own clock,
  merges into canonical per-channel state
  (`{channelId, lastAdded, lastRemoved}`), derives current subscribed status
  as `lastAdded > lastRemoved`.
- Response body: the full current authoritative set of subscribed channel
  IDs (plain list, no timestamps).
- Client overwrites `profiles.db`'s subscriptions with the response and
  updates its local shadow snapshot to match.
- Merge function must be commutative, associative, and idempotent — this is
  what makes it safe to apply regardless of sync order or retries. See
  `internal/merge` tests for the required coverage (add/add, add-vs-remove
  race, remove/remove, stale timestamps, empty sets).

### Sync timing

Sync happens only at `run`'s launch and exit boundaries (or on-demand via
`sync`) — never per subscribe/unsubscribe action, and never while FreeTube
is open. This is a consequence of invariant #1, not a shortcut. Don't
introduce a filesystem watcher or any mechanism that writes to `profiles.db`
while FreeTube might be running.

## Conventions

- Go stdlib only where possible (see invariant #7).
- Every function touching `profiles.db` gets a test using a real, scrubbed
  fixture file under `test/` — not a synthetic minimal file. This is
  the highest-risk part of the codebase (real user data corruption); treat it
  accordingly.
- `internal/merge` must have 100% branch coverage — it's a pure function,
  there's no excuse not to.
- Prefer table-driven tests throughout.
- Structured logging via `log/slog`, not `fmt.Println`, anywhere past
  Stage 0.

## Assumptions & non-goals

- **Linux only.** `internal/guard` relies on `/proc` and Flatpak
  conventions; no Windows/macOS support planned.
- **Single-user.** One server, one canonical subscription set. Not
  multi-tenant.
- **"All Channels" profile only**, for now — sub-profiles, watch history,
  and playlists are out of scope until this core is solid.
- **Not real-time** — eventual consistency across sessions, not live sync.

## Build order

Work `TASKS.md` top to bottom through Stage 9 — that's the complete,
functional project. **Stage 10 (Release & CI) is optional** and should only
be started if explicitly asked for; don't build it automatically just
because it's next in the file. Stages 1–3 (NeDB layer, guard, merge logic)
are pure/local and fully testable without a server or Docker — deliberate,
since file corruption (invariant #1–4) is the highest-risk failure mode and
needs to be de-risked with real fixture data before any networking exists.
Stage 4 onward is comparatively low-risk plumbing on top of an
already-tested core.
