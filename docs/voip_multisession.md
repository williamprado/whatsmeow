# VoIP multi-session host + affinity router (P1)

Implements `docs/voip_production.md` §4.1 — the scaling building blocks for
running VoIP past one node:

- **`voip/host`** — runs many WhatsApp accounts (one `*whatsmeow.Client` + one
  `voip.Manager` each) inside a single worker process.
- **`voip/router`** — a consistent-hash ring mapping **account → worker**, so
  every node computes the same owner with no coordination beyond the shared
  worker list.

> ⚠️ Same ban-risk rules as the rest of `voip/`: everything stays behind
> `voip.Config.Enabled` (default false); disposable accounts only.

## Why affinity routing

Each WhatsApp account is one Signal device session: a call for account A can
only be handled by the process holding A's session, so calls **cannot be
load-balanced freely** (§0 of the production guide). The ring shards accounts
across workers deterministically, and adding/removing a worker only remaps the
accounts owned by that worker — live sessions on other workers don't move.

## `voip/router` — the ring

```go
ring := router.NewRing(0, "worker-1", "worker-2", "worker-3") // 0 = default replicas

owner := ring.Owner(account)          // which worker holds this account
mine  := ring.Owns(workerID, account) // is it me?
ring.Add("worker-4")                  // scale out: only ~1/4 of accounts remap
ring.Remove("worker-2")               // drain: only worker-2's accounts remap
```

Deterministic (same worker set ⇒ same assignment, regardless of add order),
stdlib-only, safe for concurrent use. The API layer uses `Owner` to route
signaling requests; each worker uses `Owns` to decide which accounts to host.

## `voip/host` — the multi-session worker

```go
h := host.New(host.Config{
    Manager:        voip.Config{Enabled: true, MaxConcurrentCalls: 1}, // per account
    MaxActiveCalls: 8,                                                 // per worker (CPU cap)
}, waLogger, slog.Default())

// Host-level callbacks, tagged with the account (register BEFORE attaching):
h.OnIncomingCall(func(account string, c *call.CallInfo) { ... })
h.OnCallStateChange(func(account string, c *call.CallInfo) { ... })
h.OnCallEnded(func(account string, c *call.CallInfo) { ... })
h.OnPeerAudio(func(account, callID string, pcm []float32) { ... })

// Host only the accounts the ring assigns to this worker:
n, err := h.Restore(ctx, container, func(a string) bool { return ring.Owns(workerID, a) })

// Per-account call actions (ErrNoSession if the account lives on another worker):
callID, err := h.StartCall(ctx, account, peer, false)
err = h.AcceptCall(ctx, account, callID)
err = h.EndCall(ctx, account, callID, "")
err = h.FeedCapturedPCM(account, callID, pcm)

h.Shutdown() // graceful: ends calls, stops managers, disconnects clients
```

Other entry points: `Add(ctx, container, jid)` hosts one stored device;
`Attach(account, client)` registers a caller-built client **without**
connecting (pairing flows, tests); `Remove(account)` evicts one account when
the ring moves it elsewhere.

### Capacity

Two independent caps (§4.2 — calls are CPU-bound, MLow + pion per active call):

- `voip.Config.MaxConcurrentCalls` — per **account** (kept from Phase 1).
- `host.Config.MaxActiveCalls` — per **worker**, across all hosted accounts.
  `StartCall` fails fast with `ErrAtCapacity` before dialing; size it from a
  CPU-per-call benchmark with ~60–70% peak headroom.

`Host.ActiveCalls()` (and the new `Manager.ActiveCalls()`) expose the live
count for metrics and autoscaling signals.

## Target topology

```
                 ┌──────────── API layer (auth, tenants) ────────────┐
   operator ────►│  ring.Owner(account) ──► forward to that worker   │
                 └──────┬──────────────────────┬─────────────────────┘
                        ▼                      ▼
                 VoIP worker-1          VoIP worker-2        ... (shared ring)
                 host.Host (accts       host.Host (accts
                  ring says are mine)    ring says are mine)
                        └──────────── Postgres (sessions, CDR) ──────┘
```

All workers share the Postgres session store (P0), so an account can be
re-hosted by whichever worker the ring assigns after a topology change —
re-pairing is not needed, but **live calls do not migrate** (a call ends if its
worker dies; drain instead of hard-killing).

## Status

- Unit-tested: ring determinism/distribution/minimal-remap; host lifecycle,
  affinity errors, capacity plumbing, disabled-manager propagation, shutdown.
- Not yet wired into the demo `voip-bridge-server` (still single-session by
  design); production wires `host.Host` + `router.Ring` into the AtendZappy
  backend per the topology above.
- Remaining P1 items: relay reconnect, call timeouts, server-side
  backpressure, CPU-per-call benchmark (see `docs/voip_production.md` §4.2–4.3).
