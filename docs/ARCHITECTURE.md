# Architecture

## Overview

```
                         ┌────────────────────────┐
                         │    coordinator (Go)    │
                         │   HTTPS + JSON + LP    │
                         │  - peer registry       │
                         │  - endpoint exchange   │
                         │  - signaling relay     │
                         │  (never sees plaintext │
                         │   tunnel or disco key) │
                         └───────▲───────▲────────┘
              register/pull/push │       │ register/pull/push
                                 │       │
    ┌────────────────────────────┴───┐ ┌─┴────────────────────────────┐
    │ node A (behind NAT)            │ │ node B (behind NAT)            │
    │                                │ │                                │
    │  gretund daemon                │ │  gretund daemon                │
    │   ├─ identity: ed25519 + disco │ │   ├─ identity                  │
    │   ├─ STUN on shared UDP socket │ │   ├─ STUN on shared UDP socket │
    │   ├─ disco signaling (NaCl box)│ │   ├─ disco signaling           │
    │   ├─ hole-punch probes         │ │   ├─ hole-punch probes         │
    │   └─ orchestrates kernel:      │ │   └─ orchestrates kernel:      │
    │      fou add + gretun encap    │ │      fou add + gretun encap    │
    │         │                      │ │         │                      │
    └─────────┼──────────────────────┘ └─────────┼──────────────────────┘
              │ GRE-over-UDP (FOU)               │
              └──────────────── direct ──────────┘
              (falls back to DERP-style relay if unreachable)
```

## Responsibilities

1. **`gretund`** (userspace): identity, signaling, STUN, hole punching,
   keepalives, coordinator RPC. Holds all private keys.
2. **Kernel data plane** (`fou` + `ip_gre`): once a hole is punched and the
   daemon knows the peer's public `ip:port`, it issues `FouAdd` +
   `LinkAdd(Gretun{Encap*})` and steps out. The kernel fast path handles
   every data packet.
3. **Coordinator**: public-endpoint swap + sealed-envelope relay. No
   private keys, no plaintext — its compromise leaks only the public
   peer graph.

## Disco socket vs kernel FOU port

Every node runs two UDP "surfaces":

- The **disco socket** (userspace, the `gretund` process): STUN + disco
  ping/pong. The mapping learned by STUN on this socket is what we
  advertise as `source=stun` endpoints.
- The **kernel FOU RX port**: where GRE-over-UDP data packets arrive.
  The kernel demuxes based on `{family, port, protocol=47}`.

Hole punching happens on the disco socket. Once a path is validated, the
daemon creates the GRE link with `EncapDport` pointing at the peer's FOU
port. Because almost every consumer NAT treats UDP mappings as per-socket
rather than per-destination for endpoint-independent mapping, the FOU
port's mapping is distinct from (but stable alongside) the disco socket's.
That's the same assumption WireGuard + magicsock make for `EndpointIndependentMapping`.

If this assumption fails on a symmetric NAT, the daemon falls back to the
`relay` state (see below), or — with `--aggressive-punch` — port-prediction
probing of the peer's likely FOU port range.

## Why kernel-owned data path

Running the data plane in userspace (à la `wireguard-go`) would pull in
packet I/O loops, a netstack, and performance tuning. Keeping the kernel
on the fast path lets this project stay focused on NAT traversal and the
control plane. Userspace netstack mode (`tailscaled --tun=userspace-networking`
in Tailscale parlance) is a natural follow-up.

## Data-plane relay: deferred, not hidden

The daemon's state machine ends in `relay` when a peer can't be reached
directly. In this first cut, **only disco signaling is relayed** — the
coordinator queues and delivers sealed envelopes, which is enough to
let two peers on compatible NATs rendezvous. True data-plane relay
(Tailscale-style DERP — a persistent HTTPS tunnel that carries every
wrapped GRE packet through the coordinator) is explicitly out of scope
for this milestone.

Trade-offs:

- **For**: data-plane relay is the only way to connect two symmetric-NAT
  peers without port prediction. Without it, some peer pairs will show
  `state=relay` and never carry traffic.
- **Against**: implementing DERP properly means an HTTPS-streamed UDP
  pipe, backpressure, idle timeouts, and per-tenant rate limiting. It's
  a whole project on its own, and the coordinator's blast-radius story
  gets meaningfully larger when it can see bytes.

The state machine's `relay` state logs `"data-plane relay not yet
implemented — peer unreachable"`. Upgrading later should require zero
wire-format changes in disco or the coordinator API; just new endpoints
for the relay stream.

## Security model

- **Node key** (Ed25519) — authenticates HTTP requests to the coordinator.
  Private half never leaves the node.
- **Disco key** (Curve25519) — seals signaling envelopes with `nacl/box`.
  The coordinator can enqueue and deliver envelopes but cannot read them.
- **Tunnel data** — plaintext GRE. For confidentiality over untrusted
  networks, pair gretun with WireGuard or IPsec (documented in the README).

The reviewer narrative: "losing the coordinator reveals only the public
peer graph. No tunnel keys, no signaling plaintext — the sealed bytes on
its disk are encrypted with Curve25519 keys it never sees."
