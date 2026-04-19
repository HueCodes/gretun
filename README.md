# gretun

gretun is a NAT-traversing peer-to-peer GRE overlay for Linux. Two hosts behind consumer NAT can run `gretun up --coordinator <url>` and get a direct, kernel-fastpath GRE tunnel between them without port forwarding, static IPs, or manual config. The control plane uses Ed25519 and Curve25519 disco envelopes (Tailscale-compatible wire format); the data plane is pure kernel GRE-over-FOU.

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org/)
[![CI](https://github.com/HueCodes/gretun/actions/workflows/ci.yml/badge.svg)](https://github.com/HueCodes/gretun/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Linux](https://img.shields.io/badge/Platform-Linux-FCC624?logo=linux)](https://www.linux.org/)

</div>

## Features

* **FOU Encapsulation**: Wraps GRE (IP proto 47) in UDP so consumer NATs can map it and it can be hole-punched
* **STUN Endpoint Discovery**: Each node discovers its public `ip:port` via STUN over a shared userspace UDP socket
* **Coordinator**: Small HTTP service that registers peers and relays sealed disco envelopes; holds no node private keys
* **Disco Envelopes**: 6-byte magic plus sender Curve25519 pubkey plus NaCl-box sealed body (Tailscale-compatible format)
* **Hole Punching**: Each side sends disco pings to published endpoints; first pong wins. Symmetric-NAT detection built in
* **Kernel Fastpath**: After the path is validated, `gretund` calls `FouAdd` plus `LinkAdd(Gretun{EncapType:FOU, EncapDport})` and exits the data path
* **Aggressive-Punch Mitigation**: Symmetric-NAT 256-socket probe is opt-in (~98% success at 1024 probes per Tailscale)
* **Prometheus Metrics**: `gretun_peers`, `gretun_disco_pings_sent_total`, `gretun_hole_punch_duration_seconds`, and more
* **Standalone GRE**: `gretun create` still works for point-to-point GRE (bare or with FOU encap) between known endpoints
* **JSON Output**: All commands support `--json` and `--verbose`

## How It Works

1. **FOU is the trick.** Bare GRE is IP protocol 47 with no UDP header, so consumer NATs cannot map it. [Linux FOU](https://lwn.net/Articles/614348/) (Foo-over-UDP, kernel 3.18+) wraps the GRE packet in a plain UDP header so the outer is just UDP. Any NAT that can forward UDP works.
2. **STUN** on a shared userspace UDP socket tells each node its own public `ip:port`.
3. **Coordinator** (small HTTP server) swaps endpoints between peers and relays [Tailscale-style disco envelopes](https://tailscale.com/blog/how-nat-traversal-works). The coordinator never holds node private keys; compromise leaks only the public peer graph.
4. **Hole punching.** Each side sends disco `ping` messages to the other's published endpoints; the first `pong` wins.
5. **Kernel owns the data path.** Once validated, the daemon configures FOU and GRE via netlink and steps out. Every packet after that is kernel fastpath.

Full write-up in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) (disco socket vs FOU port split, threat model, relay deferral rationale).

## Architecture

```
                   ┌──────────────────────┐
                   │  gretun-coord (HTTP) │
                   │   registry + relay   │
                   └────▲────────▲────────┘
              register │          │ register
                       │          │
   ┌───────────────────┴───┐  ┌───┴────────────────────┐
   │ node A (behind NAT)   │  │ node B (behind NAT)    │
   │   ed25519 + disco key │  │   ed25519 + disco key  │
   │   disco socket (UDP)  │  │   disco socket (UDP)   │
   │   kernel FOU RX port  │  │   kernel FOU RX port   │
   └───────┬───────────────┘  └────────────┬───────────┘
           │    GRE-over-UDP (FOU) direct  │
           └───────────────────────────────┘
```

### Packages

| Package | Purpose |
|---------|---------|
| `cmd/gretun` | CLI entry point (`up`, `stun`, `create`, `list`, `status`, and more) |
| `cmd/gretun-coord` | Coordinator HTTP server (registry + signaling relay) |
| `internal/daemon` | `gretun up` peer state machine; kernel FOU+GRE setup |
| `internal/disco` | STUN client, disco envelope format, NaCl-box sealing |
| `internal/coord` | Peer registry, signaling relay, pool allocation |
| `internal/tunnel` | GRE link create and delete via netlink; encap config |
| `internal/health` | ICMP probe of tunnels |
| `internal/capabilities` | `CAP_NET_ADMIN` check |

## Installation

### Prerequisites
* Linux (kernel 3.18+)
* Kernel modules `fou` and `ip_gre`: `modprobe fou ip_gre` (requires `CONFIG_NET_FOU=y` and `CONFIG_NET_FOU_IP_TUNNELS=y`)
* Go 1.23+
* Root or `CAP_NET_ADMIN`

### Build
```bash
git clone https://github.com/HueCodes/gretun.git
cd gretun
make build
```

Or via `go install`:
```bash
go install github.com/HueCodes/gretun/cmd/gretun@latest
go install github.com/HueCodes/gretun/cmd/gretun-coord@latest
```

## Usage

### Run a coordinator

Any reachable host:

```bash
./bin/gretun-coord --listen :8443 --pool 100.64.0.0/24
```

### Bring up a peer

Linux, root or `CAP_NET_ADMIN`:

```bash
sudo ./bin/gretun up --coordinator http://coord.example.com:8443 \
  --node-name site-a
```

Tunnel comes up in ~1-5s (hole punch plus NAT probe). Peers appear in the coordinator's pool and are pingable once both sides reach `state=direct`.

### STUN spot-check

```bash
gretun stun
# 203.0.113.42:54321  (via stun.cloudflare.com:3478)
```

### Plain GRE (point-to-point, known endpoints)

```bash
sudo gretun create --name tun0 \
  --local 192.0.2.1 --remote 192.0.2.2 \
  --encap fou --encap-dport 7777 \
  --tunnel-ip 100.64.0.1/30
```

Equivalent iproute2:

```
ip fou add port 7777 ipproto 47
ip link add tun0 type gretap \
  local 192.0.2.1 remote 192.0.2.2 \
  encap fou encap-dport 7777 encap-csum
```

Default MTU is 1468 to accommodate the 32-byte outer header (IP 20 + UDP 8 + GRE 4).

## CLI Reference

| Command | Purpose |
|---------|---------|
| `gretun up` | Start the hole-punching daemon |
| `gretun stun` | Print this host's public UDP endpoint |
| `gretun create` | Create a plain GRE tunnel (optional `--encap fou`) |
| `gretun delete` | Tear down a tunnel |
| `gretun list` | List GRE tunnels |
| `gretun status` | Inspect one tunnel |
| `gretun health` | ICMP probe all tunnels |
| `gretun probe` | ICMP probe one host |
| `gretun-coord` | Coordinator server |

All commands support `--json` and `--verbose`.

## Metrics

Passing `--metrics-addr :9100` to `gretun up` exposes Prometheus counters at `/metrics`:

* `gretun_peers{state="direct|relay|punching|..."}`
* `gretun_disco_pings_sent_total`, `gretun_disco_pongs_received_total`
* `gretun_hole_punch_duration_seconds`

## Limitations

* **No tunnel encryption.** GRE + FOU are plaintext. For confidentiality over untrusted networks, pair gretun with WireGuard or IPsec at the inner layer. (Encrypting the outer would just reinvent WireGuard.)
* **No data-plane relay yet.** If two peers cannot be hole-punched (symmetric NAT on both sides with `--aggressive-punch` off), the daemon reaches `state=relay` and logs; data does not flow. Signaling is always relayable. DERP-style data relay is a natural follow-up.
* **Symmetric-NAT punching** is detected but the 256-socket mitigation is opt-in (`--aggressive-punch`). ~98% success at 1024 probes per [Tailscale](https://tailscale.com/blog/how-nat-traversal-works).
* **Linux-only.** Data plane is kernel FOU+GRE. The daemon exits early on non-Linux. A userspace netstack mode (like `tailscaled --tun=userspace-networking`) is a possible next step.
* **No ACLs or tailnet-style policy.** The coordinator is a dumb registry.

## Testing

```bash
make test          # unit tests (race detector)
make cover         # coverage report
make vet           # go vet
make lint          # golangci-lint
```

`internal/daemon` ships with a state-machine test. A netns plus MASQUERADE harness that exercises real hole punching is a planned follow-up.

## References

* [Tailscale: How NAT Traversal Works](https://tailscale.com/blog/how-nat-traversal-works)
* [tailscale/disco/disco.go](https://github.com/tailscale/tailscale/blob/main/disco/disco.go) (wire format mirrored here)
* [LWN: Foo Over UDP](https://lwn.net/Articles/614348/)
* RFC 8489 (STUN), RFC 5128 (NAT types)

## License

MIT. See `LICENSE` for details.
