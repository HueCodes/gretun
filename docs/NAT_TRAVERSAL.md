# How a gretun tunnel comes up

This is the reader-friendly walkthrough. For authoritative wire details,
see [PROTOCOL.md](PROTOCOL.md).

If you haven't read it yet: [Tailscale — How NAT Traversal Works](https://tailscale.com/blog/how-nat-traversal-works).
Everything below is a deliberate simplification of that design, with
the novelty that the data plane stays in the Linux kernel via FOU.

---

## The problem

Two hosts behind consumer NAT want a direct tunnel. Neither has a public
IP. Neither has a port forwarded. **Plain GRE can't traverse NAT at all**
— it's IP protocol 47, no UDP/TCP, so home routers can't demux it and
won't keep state for an outbound "connection".

## The fix, in three moves

### 1. Wrap GRE in UDP with Linux FOU

`CONFIG_NET_FOU` has been in Linux since 3.18. Telling the kernel:

```
ip fou add port 7777 ipproto 47
ip link add tun0 type gre \
   local 1.2.3.4 remote 5.6.7.8 \
   encap fou encap-sport auto encap-dport 7777 encap-csum
```

...makes outbound GRE packets look like:

```
[ IP | UDP(dst=7777) | GRE | inner payload ]
```

Outer protocol is now 17 (UDP), not 47 (GRE). Standard NAT UDP mapping
applies. gretun does exactly these two calls via netlink (see
`internal/tunnel/gre.go`).

### 2. Learn each side's public `ip:port` with STUN

`gretund` opens one UDP socket (the **disco socket**) and binds port 0.
It sends a STUN Binding request to a public STUN server. The server
replies with the XOR-MAPPED-ADDRESS it saw — that's this node's public
`ip:port` as far as the internet is concerned. That tuple goes into the
node's endpoint list.

### 3. Punch a hole with disco `ping`

Both nodes publish their endpoint lists to the coordinator. The
coordinator doesn't decide what to do with them — it just hands each
node the other's list.

Each side sends a sealed disco `ping` to every candidate endpoint of
the other. For NATs that are *endpoint-independent* (full cone or
address-restricted), the first outbound ping from A creates a NAT
mapping that admits B's reply. First `pong` wins; the state machine
moves to `direct`; `gretund` issues `LinkAdd(Gretun{Encap*})` pointed at
the winning endpoint.

For *symmetric* NAT, port prediction via a birthday-paradox probe is
required. gretun detects this (by comparing the STUN mappings from two
different servers) and logs; the mitigation is gated behind
`--aggressive-punch`.

---

## Why the coordinator can't read the tunnel

Two keypairs per node:

- **Node key** (Ed25519): HTTP auth. Coordinator sees the public half;
  signatures prove liveness and possession.
- **Disco key** (Curve25519): NaCl-box sealing of envelope bodies.
  Coordinator sees the public half; ciphertext it relays is
  end-to-end-encrypted with keys it never has.

Tunnel payload never touches the coordinator — it rides the direct UDP
path. The coordinator's worst-case compromise is: "who is talking to
whom, and what public `ip:port` they advertised." Same failure mode as
Tailscale's control plane.

---

## The state machine

```
    ┌───────────────────────────────────────────────────┐
    │                                                   │
    ▼                                                   │
 unknown ──(endpoints learned)──▶ have_endpoints        │
                                    │                   │
                                    ▼                   │
                                 punching               │
                              (send ping every 250ms    │
                               to every candidate;      │
                               5s overall budget)       │
                                    │                   │
                     ┌── pong rx ───┼── timeout ───┐    │
                     ▼                             ▼    │
                   direct                         relay │
                (kernel GRE+FOU                  (signal │
                 link is up,                      only;  │
                 keepalives                       data   │
                 every 25s)                       plane  │
                                                  TBD)   │
                     │                             │    │
                     └── pong miss > 75s ──────────┴────┘
                                 (re-punch)
```

States transition on events: peer updates from the coordinator, packets
arriving on the disco socket, or timer ticks. The source of truth is
`internal/daemon/peer.go`.

## Cheat-sheet for debugging

```bash
# Is my NAT what I think it is?
gretun stun
gretun stun --server stun.cloudflare.com:3478
# Two different public ports → symmetric NAT.

# What peers does the coordinator know about?
curl -s http://coord.example.com:8443/debug/peers | jq

# What state is my daemon in?
curl -s http://127.0.0.1:9100/metrics | grep gretun_peers

# Did FOU come up?
ip -d link show gretun0      # should show "encap fou"
ip fou show                  # should list the RX port

# Is a tunnel actually passing traffic?
sudo gretun health
```
