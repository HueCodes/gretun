# gretun

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Linux](https://img.shields.io/badge/Linux-Only-FCC624?logo=linux)](https://www.linux.org/)

</div>

GRE tunnel management CLI for Linux. Uses netlink to create, list, delete, and health-check GRE tunnels.

## Install

```bash
go install github.com/HueCodes/gretun/cmd/gretun@latest
```

Or build from source:

```bash
git clone https://github.com/HueCodes/gretun.git
cd gretun
make build
```

## Usage

Requires root privileges.

```bash
# Create a tunnel
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2

# With GRE key and tunnel IP
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --key 12345 --tunnel-ip 192.168.1.1/30

# List tunnels
sudo gretun list
sudo gretun list --json

# Tunnel status
sudo gretun status --name tun0

# Health probe
sudo gretun probe --target 192.168.1.2
sudo gretun probe --target 192.168.1.2 --count 5 --threshold 3

# Delete
sudo gretun delete --name tun0

# Version
gretun version
```

## Site-to-Site Example

```
                        GRE Tunnel
    +-----------+                        +-----------+
    |  Host A   |========================|  Host B   |
    | 10.0.0.1  |    encapsulated pkts   | 10.0.0.2  |
    +-----------+                        +-----------+
         |                                     |
    192.168.1.1/30  <-- tunnel IPs -->  192.168.1.2/30
```

Host A:
```bash
sudo gretun create --name site-b --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30 --key 1001
```

Host B:
```bash
sudo gretun create --name site-a --local 10.0.0.2 --remote 10.0.0.1 --tunnel-ip 192.168.1.2/30 --key 1001
```

Verify:
```bash
sudo gretun probe --target 192.168.1.2
```

## Project Structure

```
cmd/gretun/           CLI entry point and cobra commands
internal/tunnel/      GRE tunnel CRUD via netlink
internal/health/      ICMP health probing
internal/version/     Build version info
```

## License

MIT
