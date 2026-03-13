# gretun

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org/)
[![CI](https://github.com/HueCodes/gretun/actions/workflows/ci.yml/badge.svg)](https://github.com/HueCodes/gretun/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Linux](https://img.shields.io/badge/Platform-Linux-FCC624?logo=linux)](https://www.linux.org/)

</div>

A CLI tool for managing GRE (Generic Routing Encapsulation) tunnels on Linux. Uses the netlink API to create, inspect, delete, and health-check GRE tunnel interfaces directly from userspace.

## Usage

```bash
# Create a GRE tunnel between two endpoints
sudo gretun create --name site-b \
  --local 10.0.0.1 --remote 10.0.0.2 \
  --tunnel-ip 192.168.100.1/30

# List all GRE tunnels
sudo gretun list

# NAME      LOCAL      REMOTE     KEY  TUNNEL IP          STATUS
# site-b    10.0.0.1   10.0.0.2   -    192.168.100.1/30   up

# Check tunnel connectivity
sudo gretun probe --target 192.168.100.2

# Health check all tunnels (with optional watch mode)
sudo gretun health
sudo gretun health --watch --interval 10s

# Get detailed tunnel status
sudo gretun status --name site-b

# Delete a tunnel
sudo gretun delete --name site-b
```

All commands support `--json` for machine-readable output and `--verbose` for debug logging.

## Install

```bash
go install github.com/HueCodes/gretun/cmd/gretun@latest
```

Or build from source:

```bash
git clone https://github.com/HueCodes/gretun.git
cd gretun
make build          # build on Linux
make build-cross    # cross-compile for Linux amd64 from any OS
```

## Architecture

```
cmd/gretun/             CLI entry point and cobra commands
internal/tunnel/        GRE tunnel CRUD via netlink
  ├── gre.go            Create, Delete, Get, AssignIP
  ├── list.go           List all GRE tunnels
  ├── validate.go       Input validation (names, IPs, CIDRs)
  ├── errors.go         Typed errors (exists, not found, permission, etc.)
  ├── errtranslate.go   Syscall → user-friendly error translation
  └── netlink.go        Netlinker interface for testability
internal/health/        ICMP probing for tunnel connectivity checks
internal/capabilities/  Linux capability checking (CAP_NET_ADMIN)
internal/version/       Build-time metadata injection
```

Key design decisions:
- **Netlinker interface** abstracts all netlink calls behind a mockable interface, enabling comprehensive unit testing without root or real interfaces
- **Typed errors** with `TranslateNetlinkError` convert raw syscall errors into actionable messages with hints
- **Concurrent health probes** via bounded worker pool with context-aware cancellation

## Testing

```bash
make test       # run all tests with race detector
make cover      # run tests with coverage report
make vet        # go vet
make lint       # golangci-lint (requires golangci-lint installed)
```

Tests use a hand-written mock of the `Netlinker` interface to verify tunnel operations, validation, error translation, and cleanup behavior without requiring root privileges.

## License

MIT
