# abctl

Interactive terminal UI for inspecting AuthBridge's in-memory session store.
`abctl` connects to the session API exposed by an AuthBridge sidecar
(default `http://localhost:9094`, typically reached via `kubectl port-forward`)
and lets you browse active sessions, follow a session's event stream live,
and read individual events as pretty-printed JSON.

```
┌─ abctl · http://localhost:9094 ────────────────────────────────┐
│ ID                       UPDATED    EVENTS  ACTIVE             │
│ ► ctx-abc-1234…          3s ago     42      ●                  │
│   ctx-def-5678…          18m ago    15                         │
│   default                1h ago     8                          │
│                                                                 │
│ ● connected   2.1 ev/s   drops: 0                              │
│ [↑↓/jk] nav  [↵] drill  [/] filter  [p] pause  [q] quit        │
└─────────────────────────────────────────────────────────────────┘
```

## Build

```sh
cd authbridge/cmd/abctl
go build .
```

Produces a single static binary (~10 MB).

## Run

`abctl` discovers AuthBridge agents in your current `kubectl` context
and lets you pick one:

```sh
./abctl
```

You'll see a Namespaces pane listing each namespace that contains an
AuthBridge agent. Enter drills into the Pods pane for that namespace;
Enter on a pod starts a `kubectl port-forward` automatically and drops
you into the session-events view. Esc backs out. `q` (or Ctrl+C) quits
and tears the port-forward down.

The picker shells out to `kubectl` — whatever context you're in is the
context abctl uses. There's no separate auth.

### Power-user / scripting bypass

Pass `--endpoint` to skip the picker entirely:

```sh
kubectl port-forward -n team1 pod/weather-agent-xxxx 9094:9094 &
./abctl --endpoint http://localhost:9094
```

This preserves the pre-picker behavior for scripts, CI, or remote
session APIs that aren't in your kube context.

## Panes

The UI has three panes. `Enter` drills in; `Esc` backs out.

- **Sessions** (default): table of active sessions in the store, most
  recently updated first. Columns: ID, updated (relative), event count,
  active marker.
- **Events**: per-session event table. Columns: time, direction (in/out),
  phase (req/resp), protocol (a2a/mcp/inf), method or model, HTTP status,
  duration, host. Live-updates while in view — if the cursor is on the
  last row, it auto-follows new events.
- **Detail**: pretty-printed JSON of a single event. Scroll with arrow
  keys; `y` yanks to `/tmp/abctl-event-<timestamp>.json` and flashes the
  path in the footer.

## Keybindings

| Key | Context | Action |
|---|---|---|
| `↑ ↓` / `k j` | picker, list | navigate rows |
| `Enter` | namespaces | open the namespace |
| `Enter` | pods | port-forward + connect |
| `Esc` | pods | back to namespaces |
| `r` | namespaces, pods | reload agent list from cluster |
| `Enter` / `→` / `l` | sessions, events | drill into selection |
| `Esc` / `←` / `h` | detail, events | back out |
| `Esc` | sessions, pipeline | (picker mode) tear down port-forward and back to pods |
| `/` | sessions, events | filter (substring match; Enter commits, Esc cancels) |
| `s` | events | toggle skip-row visibility (default: hidden; the events footer shows the hidden count) |
| `p` | any | pause/resume stream |
| `y` | detail | yank event JSON to `/tmp` |
| `g` / `G` | lists | jump to top / bottom |
| `e` | pipeline | edit pipeline subtree in `$EDITOR` |
| `y` | edit/diff | apply the edit |
| `N` | edit/diff | abort the edit |
| `r` | edit/error | re-open the editor with the same tempfile |
| `Esc` | edit/* | abort the edit, return to Pipeline pane |
| `?` | any | (reserved for future help overlay) |
| `q` / `Ctrl+C` | any | quit |

## Editing the pipeline

Press `e` on the Pipeline pane to edit the agent's runtime `pipeline:`
subtree in `$EDITOR` (or `vi` if unset). On save, abctl shows a diff
and asks `apply this change? (y/N)`. Confirming runs
`kubectl apply --server-side` against the per-agent ConfigMap, then
polls the framework's `/reload/status` until the reload completes
(success or failure).

The single edit flow covers four operations:
- **Edit a value** — change a config field of an existing plugin
- **Reorder** — move a plugin's lines up or down
- **Remove** — delete a plugin's entry from `inbound:` or `outbound:`
- **Add** — append a new plugin entry

All four work because they're all just lines you change inside the
pipeline subtree.

### Permissions

abctl shells out to `kubectl`; kubectl uses your kubeconfig. Editing
requires `update` on `configmaps` in the agent's namespace (in
addition to `get pods` which the picker already needs). RBAC denial
surfaces verbatim in the overlay.

### Tempfile lifecycle

abctl writes the editable pipeline subtree to `$TMPDIR/abctl-pipeline-*.yaml`
on every edit. The tempfile is **left in place on every exit path**
(success, error, abort) so an interrupted edit is recoverable. Clean
up `/tmp/abctl-pipeline-*` periodically.

### Hot-reload window

The framework reloads via a config-file watcher; kubelet syncs
ConfigMap edits into the pod's mount within ~60s, then the framework
debounces and reloads. Total wall-clock from `apply` to reload is
typically under 90s. abctl shows a spinner during the wait. If
`/reload/status` doesn't observe a successful reload within 120s,
abctl gives up watching and tells you to check `kubectl logs deploy/<agent>`.

## Trust model

`abctl` does no authentication — same as the server. Use only against
sidecars reachable via in-cluster networking or a local port-forward.
Session events contain raw user messages, LLM completions, and tool
results; treat the output accordingly.

## Architecture

- `apiclient/` — HTTP + SSE client. Sole owner of the `:9094` wire format.
  Auto-reconnects with exponential backoff (1s → 30s, capped, indefinite).
- `tui/` — Bubble Tea model/update/view. All state mutation runs on the
  Tea event loop; the SSE goroutine produces messages the loop consumes.
- `main.go` — flag parsing, signal handling, wires `tui.Run`.

## Deferred to later PRs

- Native clipboard (currently writes to `/tmp`).
- Fuzzy search beyond substring match.
- Per-user filtering (`Identity.Subject == X`).
- Krew plugin packaging.
