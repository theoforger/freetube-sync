# freetube-sync

Syncs [FreeTube](https://freetubeapp.io/) subscriptions across devices.

```
freetube-sync serve    --listen :8080 --data /var/lib/freetube-sync --token <secret>
freetube-sync run      --server https://host:8080 --token <secret> [--install=flatpak|native]
freetube-sync sync     --server https://host:8080 --token <secret> [--install=flatpak|native]
freetube-sync status   [--install=flatpak|native]
freetube-sync inspect  --db path/to/profiles.db
```

## AI usage disclosure

This project is planned and built mostly by AI with human influences
on many decisions.

It's a product of me wanting a quick-and-dirty way to sync across my
FreeTube instances. It does NOT reflect my personal opinion on using
AI for coding or in open-source projects.

## Setup walkthrough

### 1. Generate a token

Every client and the server authenticate with one shared token. Generate
it once and keep it somewhere safe (e.g. a password manager):

```sh
openssl rand -hex 32
```

### 2. Run the server

**Docker (recommended):**

```sh
cd deployments
cp .env.example .env
# edit .env: set TOKEN to the value you generated above
docker compose up -d
```

This publishes the server's port directly on the host — **only safe on a
trusted network** (Tailscale, WireGuard, or a LAN you control); there's no
TLS in this default setup. For public exposure, use the standalone Caddy
deployment instead:

```sh
cd deployments
cp .env.example .env
# edit .env: set TOKEN, and DOMAIN to your real public domain
docker compose -f compose.caddy.yaml up -d
```

**Without Docker:**

```sh
go build -o freetube-sync ./cmd/freetube-sync
./freetube-sync serve --listen :8080 --data /var/lib/freetube-sync --token <secret>
```

### 3. Install the client on each device

```sh
go build -o freetube-sync ./cmd/freetube-sync
./scripts/install-client.sh
```

Detects whether FreeTube is Flatpak or native (asks if both are found),
writes a "FreeTube (synced)" launcher to your application menu, copies the
`freetube-sync` binary to `~/.local/bin`, and saves the server URL/token to
`~/.config/freetube-sync/config.json` — never baked into the launcher
itself. Safe to re-run, including after switching install types.

Launch FreeTube from the new "FreeTube (synced)" entry instead of the
original one — each launch syncs, opens FreeTube, waits for it to close,
then syncs again.

Remove with `./scripts/uninstall-client.sh` (`--keep-config` to keep your
server URL/token for a later reinstall).

### 4. (Optional) periodic background sync

`freetube-sync run` only syncs at FreeTube's launch and exit. To also
catch up between sessions, install the systemd user timer, which runs
`freetube-sync sync` every 30 minutes (skipped automatically while
FreeTube is open — the guard check handles that):

```sh
mkdir -p ~/.config/systemd/user
cp init/systemd/freetube-sync.* ~/.config/systemd/user/
systemctl --user enable --now freetube-sync.timer
```

## Token rotation

1. Pick a new token (`openssl rand -hex 32`).
2. Update the server: change `TOKEN` in `deployments/.env` and run
   `docker compose up -d` again from `deployments/` (or restart
   `serve --token <new-token>` if running without Docker).
3. On each client, re-run `./scripts/install-client.sh` — it prompts for a
   token, defaulting to the existing one; type the new one instead.
4. Until a client is updated, its syncs fail closed on auth (401), then
   fail open as usual: FreeTube still launches/closes normally, just
   without syncing, until the token is fixed.

There's no multi-token support — the server is single-tenant with one
canonical subscription state, and every client shares one token.

## Conflict behavior

Subscriptions merge as an LWW-element-set: the server timestamps every
add/remove event on arrival (clients never send timestamps, to keep client
clock skew from corrupting merges — see `internal/merge`), and a channel
is "subscribed" iff its most recent add is newer than its most recent
remove. That makes the merge commutative, associative, and idempotent —
it doesn't matter what order two devices' syncs arrive in, or whether a
sync is retried, the result converges the same either way.

Practically: if you subscribe to a channel on device A and unsubscribe
from it on device B before A has synced, whichever event reaches the
server *later* wins, regardless of which device acted first. There's no
manual conflict resolution — the timestamps make one deterministic
outcome.

Only the "All Channels" profile's subscriptions sync. Other profiles,
watch history, and playlists are untouched.

## Diagnostics

- `freetube-sync status` — resolved config, detected install, whether
  it's currently safe to write `profiles.db`.
- `freetube-sync inspect --db path/to/profiles.db` — prints a database's
  parsed subscriptions, no side effects.
- `--dry-run` on `sync`/`run` — previews the local add/remove diff without
  writing `profiles.db`, the shadow snapshot, or contacting the server.

## License

[AGPLv3](LICENSE). If you run a modified version of the server for others
over a network, you must make your modified source available to them.
