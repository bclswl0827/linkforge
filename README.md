# Linkforge

**Linkforge** is a Go library for configuring Linux network interfaces. It combines
rtnetlink-based link/address/route management with native Go DHCPv4 and DHCPv6
clients — no external commands (`ip`, `dhclient`, etc.) are executed.

## Features

- **Interface lifecycle**: `SetUp` / `SetDown` with per-interface locking
- **Static configuration**: IPv4/IPv6 addresses + optional default route
- **DHCPv4**: Full DORA exchange, lease renewal (T1/T2), rebind, re-acquisition
- **DHCPv6**: Rapid Commit (Solicit/Reply), IA_NA address management, T1 renewal
- **IPv6 RA**: Router Solicitation, managed default route, and RDNSS discovery
- **Thread-safe**: Concurrent operations on different interfaces
- **Context-aware**: All blocking operations respect context cancellation
- **No external dependencies**: Pure Go; no `ip`, `dhclient`, `ifconfig`, etc.

## Requirements

- Linux kernel (rtnetlink, AF_PACKET)
- Go 1.22+
- Capabilities: `CAP_NET_ADMIN` (link/addr/route), `CAP_NET_RAW` (DHCP)

## Installation

```bash
go get github.com/bclswl0827/linkforge
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net"

    "github.com/bclswl0827/linkforge"
)

func main() {
    ctx := context.Background()

    client, err := linkforge.New()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Bring interface up
    if err := client.SetUp("eth0"); err != nil {
        log.Fatal(err)
    }

    // Static IP
    lease, err := client.Configure(ctx, linkforge.Config{
        Interface: "eth0",
        Mode:      linkforge.ModeStatic,
        Static: linkforge.StaticConfig{
            Address: &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
            Gateway: net.ParseIP("192.0.2.1"),
        },
    })
    // lease is nil for static mode
}
```

## API Overview

| Type            | Purpose                                                     |
| --------------- | ----------------------------------------------------------- |
| `Client`        | Main entry point; owns rtnetlink handle and DHCP sessions   |
| `Config`        | High-level configuration request (static or DHCP)           |
| `Mode`          | `ModeStatic` or `ModeDHCP`                                  |
| `AddressFamily` | `FamilyIPv4` or `FamilyIPv6`                                |
| `StaticConfig`  | Static address + optional gateway/metric                    |
| `DHCPConfig`    | DHCP/RA timeout, retries, metric, optional DNS file         |
| `Lease`         | Result of DHCP configuration (address, gateway, DNS, times) |

## DHCP Behavior

- **DHCPv4**: `Request` → `Renew` at T1 → `Rebind` at T2 → `Request` (broadcast) on expiry
- **DHCPv6**: `RapidSolicit` plus Router Solicitation → retry Solicit on renewal failure
- Leases are applied via `AddrReplace` and `RouteReplace` (idempotent)
- RA default routes and RDNSS entries are refreshed and removed according to their advertised lifetimes
- DHCP and RA DNS servers are always returned in `Lease.DNS`
- `DHCPConfig.WriteDNS` optionally writes them to `DNSPath` (default `/etc/resolv.conf`)
- A managed DNS file is replaced completely; its original content is restored when the last owning session stops
- The selected RA router is returned in the DHCPv6 lease's `Gateway`
