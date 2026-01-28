# gretun

A CLI tool for managing GRE tunnels on Linux.

GRE (Generic Routing Encapsulation) tunnels are used to encapsulate network packets and transport them across a network. They are commonly used for connecting cloud VPCs, site-to-site connectivity, and network virtualization.

## Architecture

```
                        GRE Tunnel
    +-----------+                        +-----------+
    |  Host A   |========================|  Host B   |
    | 10.0.0.1  |    encapsulated pkts   | 10.0.0.2  |
    +-----------+                        +-----------+
         |                                     |
    192.168.1.1/30  <-- tunnel IPs -->  192.168.1.2/30
```

The tunnel encapsulates packets from the inner network (192.168.1.0/30) and sends them over the outer network (10.0.0.0/24).

## Installation

```bash
go install github.com/HueCodes/gretun/cmd/gretun@latest
```

Or build from source:

```bash
git clone https://github.com/HueCodes/gretun.git
cd gretun
go build -o gretun ./cmd/gretun
```

## Usage

gretun requires root privileges to manage network interfaces.

### Create a tunnel

```bash
# Basic tunnel
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2

# With GRE key for identification
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --key 12345

# With tunnel IP assigned
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30
```

### List tunnels

```bash
sudo gretun list

# JSON output
sudo gretun list --json
```

### Check tunnel status

```bash
sudo gretun status --name tun0
```

### Probe tunnel health

```bash
# Probe remote tunnel endpoint
sudo gretun probe --target 192.168.1.2

# Custom probe count and threshold
sudo gretun probe --target 192.168.1.2 --count 5 --threshold 3
```

### Delete a tunnel

```bash
sudo gretun delete --name tun0
```

## Example: Site-to-Site Tunnel

On Host A (10.0.0.1):

```bash
sudo gretun create --name site-b --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30 --key 1001
```

On Host B (10.0.0.2):

```bash
sudo gretun create --name site-a --local 10.0.0.2 --remote 10.0.0.1 --tunnel-ip 192.168.1.2/30 --key 1001
```

Verify connectivity:

```bash
# From Host A
sudo gretun probe --target 192.168.1.2
```

## Testing Locally

You can test gretun using network namespaces:

```bash
# Create two namespaces
sudo ip netns add ns1
sudo ip netns add ns2

# Create veth pair
sudo ip link add veth1 type veth peer name veth2
sudo ip link set veth1 netns ns1
sudo ip link set veth2 netns ns2

# Configure IPs
sudo ip netns exec ns1 ip addr add 10.0.0.1/24 dev veth1
sudo ip netns exec ns1 ip link set veth1 up
sudo ip netns exec ns1 ip link set lo up

sudo ip netns exec ns2 ip addr add 10.0.0.2/24 dev veth2
sudo ip netns exec ns2 ip link set veth2 up
sudo ip netns exec ns2 ip link set lo up

# Create GRE tunnels
sudo ip netns exec ns1 gretun create --name gre1 --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30
sudo ip netns exec ns2 gretun create --name gre2 --local 10.0.0.2 --remote 10.0.0.1 --tunnel-ip 192.168.1.2/30

# Test
sudo ip netns exec ns1 ping 192.168.1.2

# Cleanup
sudo ip netns del ns1
sudo ip netns del ns2
```

## Project Structure

```
gretun/
├── cmd/
│   └── gretun/
│       ├── main.go
│       └── commands/
│           ├── root.go      # CLI setup, root privileges check
│           ├── create.go    # Create tunnel
│           ├── delete.go    # Delete tunnel
│           ├── list.go      # List all tunnels
│           ├── status.go    # Show tunnel status
│           └── probe.go     # Health probing
├── internal/
│   ├── tunnel/
│   │   ├── types.go         # Config and Status structs
│   │   ├── gre.go           # Tunnel CRUD via netlink
│   │   └── list.go          # Enumerate tunnels
│   └── health/
│       └── probe.go         # ICMP probing
├── go.mod
└── README.md
```

## Why GRE Tunnels?

GRE tunnels are foundational to cloud network interconnection:

- Connect VPCs across cloud providers (AWS, GCP, Azure)
- Extend on-premises networks into the cloud
- Create overlay networks for multi-tenant isolation
- Transport protocols that routers might otherwise drop

This tool was built to understand cloud network interconnection patterns used in production infrastructure.

## License

MIT
