# gretun

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org/)
[![CI](https://github.com/HueCodes/gretun/actions/workflows/ci.yml/badge.svg)](https://github.com/HueCodes/gretun/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Linux](https://img.shields.io/badge/Platform-Linux-FCC624?logo=linux)](https://www.linux.org/)

</div>

A **NAT-traversing peer-to-peer GRE overlay** for Linux, built as a portfolio piece. Two hosts behind consumer NAT can run `gretun up --coordinator <url>` and get a direct, kernel-fastpath GRE tunnel between them — no port forwarding, no static IPs, no manual config.

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


## Quickstart

**Run a coordinator** (any reachable host):

```bash
make build
./bin/gretun-coord --listen :8443 --pool 100.64.0.0/24
```

**On each peer** (Linux, root or `CAP_NET_ADMIN`):

```bash
sudo ./bin/gretun up --coordinator http://coord.example.com:8443 \
  --node-name site-a
# Tunnel comes up in ~1-5s (hole punch + NAT probe). Peers appear in
# 100.64.0.0/24 and are pingable once both sides reach `state=direct`.
```

**Spot-check your NAT**:

```bash
gretun stun
# 203.0.113.42:54321  (via stun.cloudflare.com:3478)
```

## CLI reference

```
gretun up         start the daemon (NAT traversal)
gretun stun       print this host's public UDP endpoint
gretun create     create a plain GRE tunnel (optionally with --encap fou)
gretun delete     tear down a tunnel
gretun list       list GRE tunnels
gretun status     inspect one tunnel
gretun health     ICMP probe all tunnels
gretun probe      ICMP probe one host
gretun-coord      coordinator server
```

All commands support `--json` and `--verbose`.

### GRE-over-UDP (FOU) directly

The FOU encap is useful standalone, too — bare GRE can still be run point-to-point between hosts with known endpoints:

```bash
sudo gretun create --name tun0 \
  --local 192.0.2.1 --remote 192.0.2.2 \
  --encap fou --encap-dport 7777 \
  --tunnel-ip 100.64.0.1/30

# Equivalent iproute2:
#   ip fou add port 7777 ipproto 47
#   ip link add tun0 type gretap \
#     local 192.0.2.1 remote 192.0.2.2 \
#     encap fou encap-dport 7777 encap-csum
```

Requires kernel modules `fou` + `ip_gre` (`modprobe fou ip_gre`; `CONFIG_NET_FOU=y` + `CONFIG_NET_FOU_IP_TUNNELS=y`). Default MTU is 1468 to accommodate the 32-byte outer header (IP 20 + UDP 8 + GRE 4).

## Metrics

Passing `--metrics-addr :9100` to `gretun up` exposes Prometheus counters at `/metrics`:

- `gretun_peers{state="direct|relay|punching|..."}`
- `gretun_disco_pings_sent_total`, `gretun_disco_pongs_received_total`
- `gretun_hole_punch_duration_seconds`

## Install / build

```bash
go install github.com/HueCodes/gretun/cmd/gretun@latest
go install github.com/HueCodes/gretun/cmd/gretun-coord@latest

# or from source
git clone https://github.com/HueCodes/gretun.git
cd gretun
make build
```


## Testing

```bash
make test          # unit tests (race detector)
make cover         # coverage report
make vet           # go vet
make lint          # golangci-lint
```

Linux-only integration: `internal/daemon` ships with a state-machine test; a netns+MASQUERADE harness that exercises real hole punching is a planned follow-up.

## References

- [Tailscale — How NAT Traversal Works](https://tailscale.com/blog/how-nat-traversal-works) (read before the code)
- [tailscale/disco/disco.go](https://github.com/tailscale/tailscale/blob/main/disco/disco.go) — wire format we mirror
- [LWN — Foo Over UDP](https://lwn.net/Articles/614348/)
- RFC 8489 (STUN), RFC 5128 (NAT types)

## License

MIT
