# Examples

All examples use `linkforge` directly. They do not invoke `ip`, `dhclient`,
`ifconfig`, or any other external command.

Run them from this module with the required Linux capabilities, normally as
root:

```sh
go run ./examples/dhcp -interface eth0
go run ./examples/dhcp -interface eth0 -wait
go run ./examples/dhcp -interface eth0 -family 6 -wait
sudo go run ./examples/dhcp -interface eth0 -family 6 -write-dns -wait
go run ./examples/static -interface eth0 -address 192.0.2.10/24 -gateway 192.0.2.1
go run ./examples/static -interface eth0 -address 2001:db8::10/64 -gateway 2001:db8::1
go run ./examples/link -interface eth0 -up
go run ./examples/link -interface eth0 -down
go run ./examples/inspect
```

`dhcp` obtains a DHCPv4 lease by default; use `-family 6` for DHCPv6. Use
`-wait` to keep it alive so the library can renew or reacquire the lease.
Use `-write-dns` to replace `/etc/resolv.conf` with the learned DNS servers, or
combine it with `-dns-path /path/to/resolv.conf` to select another file. The
original file content is restored when the last session using that path stops.
`static` configures an IPv4 or IPv6 address and optional default route. For
DHCPv6, the default gateway and additional DNS servers are learned from Router
Advertisements. `link` demonstrates administrative up/down operations.
`inspect` uses `Client.Netlink()` to access the underlying
`vishvananda/netlink` handle and list links, addresses and routes.

## DHCPv6 prerequisites

DHCPv6 and Router Solicitation require an IPv6 link-local address (`fe80::/10`)
on the interface. Verify it before running the DHCPv6 example:

```sh
ip -6 addr show dev eth0 scope link
sysctl net.ipv6.conf.eth0.disable_ipv6
```

If `disable_ipv6` is `1`, temporarily enable IPv6 and wait for the link-local
address to appear:

```sh
sudo sysctl -w net.ipv6.conf.eth0.disable_ipv6=0
ip -6 addr show dev eth0 scope link
sudo go run ./examples/dhcp -interface eth0 -family 6 -wait
```

Replace `eth0` with the target interface. If NetworkManager or another network
manager changes `disable_ipv6` back to `1`, update its connection profile as
well. The library receives RA and manages the default route itself, so the
kernel `accept_ra` setting does not need to be enabled.
