# Troubleshooting Guide

This guide covers common issues you might encounter when using gretun and how to resolve them.

## Table of Contents

- [Permission Errors](#permission-errors)
- [Tunnel Creation Issues](#tunnel-creation-issues)
- [NAT Traversal Issues](#nat-traversal-issues)
- [ICMP Probe Failures](#icmp-probe-failures)
- [Network Connectivity Issues](#network-connectivity-issues)
- [Kernel Module Issues](#kernel-module-issues)
- [Validation Errors](#validation-errors)

---

## Permission Errors

### Error: "requires root privileges or CAP_NET_ADMIN capability"

**Cause:** GRE tunnel operations require network administration privileges.

**Solutions:**

1. **Run with sudo** (recommended for development/testing):
   ```bash
   sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2
   ```

2. **Grant CAP_NET_ADMIN capability** (for production without sudo):
   ```bash
   sudo setcap cap_net_admin+ep $(which gretun)
   ```

   ⚠️ **Security Note:** Granting capabilities allows the binary to perform privileged operations without sudo. Only do this if you trust the binary and understand the security implications.

3. **Verify capabilities:**
   ```bash
   getcap $(which gretun)
   # Should show: /path/to/gretun = cap_net_admin+ep
   ```

---

## Tunnel Creation Issues

### Error: "tunnel <name> already exists"

**Cause:** A tunnel with this name already exists on the system.

**Solutions:**

1. **List existing tunnels:**
   ```bash
   sudo gretun list
   ```

2. **Delete the existing tunnel:**
   ```bash
   sudo gretun delete --name tun0
   ```

3. **Or use a different name:**
   ```bash
   sudo gretun create --name tun1 --local 10.0.0.1 --remote 10.0.0.2
   ```

### Error: "tunnel name exceeds maximum length"

**Cause:** Linux interface names are limited to 15 characters.

**Solution:** Use a shorter name:
```bash
# Bad:  gre-tunnel-site-a-to-site-b
# Good: site-a-to-b
sudo gretun create --name site-a-to-b --local 10.0.0.1 --remote 10.0.0.2
```

### Error: "tunnel name uses reserved prefix"

**Cause:** The name starts with a reserved prefix (eth, lo, wlan, docker, etc.) that might conflict with system interfaces.

**Solution:** Use a different prefix:
```bash
# Potentially problematic:
sudo gretun create --name eth99 ...

# Better:
sudo gretun create --name gre-site-a ...
sudo gretun create --name tun0 ...
```

### Error: "operation not supported"

**Cause:** The GRE kernel module is not loaded.

**Solutions:**

1. **Load the GRE module:**
   ```bash
   sudo modprobe ip_gre
   ```

2. **Verify it's loaded:**
   ```bash
   lsmod | grep gre
   # Should show: ip_gre, gre
   ```

3. **Load automatically at boot** (add to `/etc/modules`):
   ```bash
   echo "ip_gre" | sudo tee -a /etc/modules
   ```

---

## NAT Traversal Issues

### Error: "FOU kernel module may be missing"

**Cause:** gretun asked for `--encap fou` or `gretun up` booted a FOU RX port,
but the kernel doesn't have the `fou` module loaded (or compiled).

**Solutions:**

```bash
# Load the module
sudo modprobe fou

# Verify kernel config
grep CONFIG_NET_FOU /boot/config-$(uname -r)
# want: CONFIG_NET_FOU=y (or =m)
#       CONFIG_NET_FOU_IP_TUNNELS=y
```

### Error: "EADDRINUSE" on the FOU port

**Cause:** Another process (or a previous gretun run) already bound that UDP
port for FOU. `FouAdd` is safe to retry (EEXIST is tolerated), but if the
kernel reports EADDRINUSE it usually means a different userspace listener
is squatting on the port.

**Solutions:**

1. Pick a different `--fou-port` or `--encap-dport`.
2. Check `ss -ulnp | grep :7777`.
3. `ip fou show` to confirm any kernel-side port.

### `gretun up` fails with `register: 401`

**Cause:** The coordinator rejected the signed request, most commonly due to:

- clock skew between the node and the coordinator beyond ±60s
- the node key changed on disk and the coordinator cached the old mapping
  (not applicable for memstore — coordinator is memstore-only)

**Solutions:**

```bash
date -u                          # compare both hosts
rm -r ~/.config/gretun/keys.json # regenerate if keys got corrupted
```

### Peer stuck in `state=punching`

**Cause:** Hole punching has not completed within the 5-second budget. Common
reasons:

- One or both peers on symmetric NAT (two STUN servers reported different
  public ports). Retry with `--aggressive-punch`.
- The kernel FOU port's NAT mapping is on a different outbound interface
  than the disco socket's.
- A firewall (local or ISP) drops inbound UDP from untrusted sources.

**Debug:**

```bash
# What endpoints is the peer actually advertising?
curl -s http://coord.example.com:8443/debug/peers | jq

# Is our disco socket sending pings?
curl -s http://127.0.0.1:9100/metrics | grep disco

# Are we receiving anything?
sudo tcpdump -i any -n udp and port 7777
```

### Peer reaches `state=direct` but no data flows

**Cause:** The punch validated the disco socket path, but the FOU port's
mapping turned out to be different.

**Debug:**

```bash
ip -d link show gretun0        # look for "encap fou" and the peer IP
sudo tcpdump -i any -n udp and port 7777
ping -c 3 100.64.0.2            # the peer's tunnel IP
```

If the outbound UDP flow's source port at the NAT isn't the one the peer
has in its `EncapDport`, the kernel won't send (or the NAT won't admit the
reverse flow). This is the limitation data-plane relay is meant to work
around; see `docs/ARCHITECTURE.md`.

---

## ICMP Probe Failures

### Error: "failed to listen: permission denied"

**Cause:** ICMP echo requires raw socket access (root or CAP_NET_RAW).

**Solution:** Run with sudo or grant capability:
```bash
sudo gretun probe --target 192.168.1.2

# Or grant capability:
sudo setcap cap_net_raw+ep $(which gretun)
```

### Probe shows "failed to receive reply: timeout"

**Possible Causes:**

1. **Target is unreachable through the tunnel**

   **Debug:**
   ```bash
   # Check if tunnel is up
   sudo gretun status --name tun0

   # Check routes
   ip route get 192.168.1.2

   # Try ping directly
   ping -I tun0 192.168.1.2
   ```

2. **Firewall blocking ICMP**

   **Debug:**
   ```bash
   # Check firewall rules
   sudo iptables -L -n -v

   # Allow ICMP temporarily for testing
   sudo iptables -I INPUT -p icmp --icmp-type echo-request -j ACCEPT
   sudo iptables -I OUTPUT -p icmp --icmp-type echo-reply -j ACCEPT
   ```

3. **Remote endpoint not configured**

   **Solution:** Ensure both sides of the tunnel are configured with matching parameters:
   ```bash
   # Host A
   sudo gretun create --name to-b --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.1.1/30

   # Host B
   sudo gretun create --name to-a --local 10.0.0.2 --remote 10.0.0.1 --tunnel-ip 192.168.1.2/30
   ```

4. **MTU issues**

   **Solution:** Adjust MTU to account for GRE overhead (24 bytes):
   ```bash
   # Standard Ethernet MTU is 1500, so GRE tunnel should be 1476
   sudo ip link set tun0 mtu 1476
   ```

### Probe shows "unhealthy" but some probes succeed

**Cause:** Intermittent connectivity or threshold not met.

**Solution:** Adjust threshold and count:
```bash
# Send 10 probes, require 7 successes
sudo gretun probe --target 192.168.1.2 --count 10 --threshold 7

# Increase timeout for slow links
sudo gretun probe --target 192.168.1.2 --timeout 5s
```

---

## Network Connectivity Issues

### Tunnel is up but no traffic flows

**Checklist:**

1. **Verify tunnel IPs are assigned:**
   ```bash
   sudo gretun status --name tun0
   # Should show tunnel_ip
   ```

2. **Check routing:**
   ```bash
   ip route
   # Should have routes through the tunnel

   # Add route if missing:
   sudo ip route add 192.168.2.0/24 dev tun0
   ```

3. **Verify endpoints can reach each other:**
   ```bash
   # Test outer (transport) connectivity
   ping 10.0.0.2
   ```

4. **Check for asymmetric routing:**
   ```bash
   # On both sides:
   sudo tcpdump -i any -n proto gre
   # Should see bidirectional GRE packets
   ```

5. **Verify firewall allows GRE (IP protocol 47):**
   ```bash
   sudo iptables -I INPUT -p gre -j ACCEPT
   sudo iptables -I OUTPUT -p gre -j ACCEPT
   ```

### Error: "address already in use"

**Cause:** The tunnel IP is assigned to another interface.

**Solutions:**

1. **Check existing addresses:**
   ```bash
   ip addr show
   ```

2. **Use a different tunnel IP:**
   ```bash
   sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --tunnel-ip 192.168.100.1/30
   ```

---

## Kernel Module Issues

### How to verify GRE support

```bash
# Check if module is available
modinfo ip_gre

# Check if module is loaded
lsmod | grep gre

# Load module
sudo modprobe ip_gre

# Unload module (only if no tunnels exist)
sudo modprobe -r ip_gre
```

### Error: "modprobe: FATAL: Module ip_gre not found"

**Cause:** Kernel doesn't have GRE support compiled (rare on modern distributions).

**Solutions:**

1. **Install kernel modules package:**
   ```bash
   # Ubuntu/Debian
   sudo apt install linux-modules-extra-$(uname -r)

   # RHEL/CentOS
   sudo yum install kernel-modules-extra
   ```

2. **Update kernel:**
   ```bash
   # Most modern kernels (3.10+) have GRE support
   uname -r  # Check version
   ```

---

## Validation Errors

### Error: "CIDR uses network address"

**Cause:** You're trying to assign the network address (first IP in subnet).

**Example:**
```bash
# Bad: 192.168.1.0 is the network address for /24
sudo gretun create --name tun0 ... --tunnel-ip 192.168.1.0/24

# Good: Use a host address
sudo gretun create --name tun0 ... --tunnel-ip 192.168.1.1/24
```

### Error: "CIDR uses broadcast address"

**Cause:** You're trying to assign the broadcast address (last IP in subnet).

**Example:**
```bash
# Bad: 192.168.1.255 is broadcast for 192.168.1.0/24
sudo gretun create --name tun0 ... --tunnel-ip 192.168.1.255/24

# Good: Use a host address
sudo gretun create --name tun0 ... --tunnel-ip 192.168.1.1/24
```

### Error: "local IP and remote IP cannot be the same"

**Cause:** Source and destination must be different.

**Solution:** Use different IPs for local and remote:
```bash
# Bad:
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.1

# Good:
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2
```

### Error: "local IP cannot be loopback address"

**Cause:** GRE tunnels can't use loopback addresses (127.0.0.0/8).

**Solution:** Use the actual interface IP:
```bash
# Find your interface IP
ip addr show

# Use that IP:
sudo gretun create --name tun0 --local 192.168.1.100 --remote 10.0.0.2
```

---

## Debug Mode

Enable verbose logging for debugging:

```bash
sudo gretun create --name tun0 --local 10.0.0.1 --remote 10.0.0.2 --verbose
```

View system logs:
```bash
# journalctl (systemd)
sudo journalctl -f -u gretun

# dmesg for kernel messages
sudo dmesg -T | grep -i gre
```

---

## Getting Help

If you've tried the solutions above and still have issues:

1. **Check the issue tracker:** [github.com/HueCodes/gretun/issues](https://github.com/HueCodes/gretun/issues)
2. **Gather diagnostic information:**
   ```bash
   # System info
   uname -a
   gretun version

   # Network config
   ip addr show
   ip route show

   # Tunnel status
   sudo gretun list --json

   # Kernel modules
   lsmod | grep gre
   ```
3. **Open a new issue** with the diagnostic information

---

## Common Patterns

### Site-to-Site VPN Setup

**Scenario:** Connect two private networks over the internet.

```bash
# Site A (10.0.1.0/24) <--> Site B (10.0.2.0/24)
# Public IPs: A=203.0.113.1, B=198.51.100.1

# On Site A:
sudo gretun create \
  --name to-site-b \
  --local 203.0.113.1 \
  --remote 198.51.100.1 \
  --tunnel-ip 192.168.100.1/30 \
  --key 1001

sudo ip route add 10.0.2.0/24 dev to-site-b

# On Site B:
sudo gretun create \
  --name to-site-a \
  --local 198.51.100.1 \
  --remote 203.0.113.1 \
  --tunnel-ip 192.168.100.2/30 \
  --key 1001

sudo ip route add 10.0.1.0/24 dev to-site-a

# Verify:
sudo gretun probe --target 192.168.100.2  # From Site A
sudo gretun probe --target 192.168.100.1  # From Site B
```

### Multiple Tunnels

**Scenario:** Hub-and-spoke topology.

```bash
# Hub (connects to multiple spokes)
sudo gretun create --name spoke1 --local 10.0.0.1 --remote 10.0.1.1 --tunnel-ip 192.168.100.1/30 --key 1001
sudo gretun create --name spoke2 --local 10.0.0.1 --remote 10.0.2.1 --tunnel-ip 192.168.100.5/30 --key 1002
sudo gretun create --name spoke3 --local 10.0.0.1 --remote 10.0.3.1 --tunnel-ip 192.168.100.9/30 --key 1003

# List all:
sudo gretun list
```

---

## Performance Tuning

### High packet loss

- Increase MTU headroom: `sudo ip link set tun0 mtu 1476`
- Check for fragmentation: `sudo tcpdump -i tun0 -n`
- Verify no bandwidth limits on outer interface

### High latency

- Check probe results: `sudo gretun probe --target <ip> --count 10`
- Verify routing is optimal: `traceroute <target>`
- Monitor system load: `top`, `htop`

---

**Last Updated:** 2026-02-11
