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

## Project Structure

```
cmd/gretun/           CLI entry point and cobra commands
internal/tunnel/      GRE tunnel CRUD via netlink
internal/health/      ICMP health probing
internal/version/     Build version info
```

## License

MIT
