//go:build linux

package linkforge

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	netlink "github.com/vishvananda/netlink"
)

func (c *Client) applyLease(session *dhcp4Session, raw *nclient4.Lease) (*Lease, error) {
	if raw == nil || raw.ACK == nil {
		return nil, errors.New("DHCP lease or ACK is nil")
	}
	ip := raw.ACK.YourIPAddr.To4()
	mask := raw.ACK.SubnetMask()
	if ip == nil {
		return nil, errors.New("DHCP ACK has no IPv4 address")
	}
	if mask == nil {
		return nil, errors.New("DHCP ACK has no valid IPv4 subnet mask")
	}
	ones, bits := mask.Size()
	if bits != 32 {
		return nil, errors.New("DHCP ACK has no valid IPv4 subnet mask")
	}
	address := &net.IPNet{IP: cloneIP(ip), Mask: net.CIDRMask(ones, 32)}
	leaseTime := raw.ACK.IPAddressLeaseTime(time.Hour)
	link, err := c.link(session.name)
	if err != nil {
		return nil, err
	}
	newAddr := &netlink.Addr{
		IPNet:       address,
		LinkIndex:   link.Attrs().Index,
		PreferedLft: int(leaseTime / time.Second),
		ValidLft:    int(leaseTime / time.Second),
	}
	oldAddr := session.currentAddr()
	if err := c.handle.AddrReplace(link, newAddr); err != nil {
		return nil, fmt.Errorf("replace DHCP address: %w", err)
	}

	var newRoute *netlink.Route
	routers := raw.ACK.Router()
	if len(routers) > 0 && routers[0].To4() != nil {
		newRoute = &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Gw:        cloneIP(routers[0].To4()),
			Scope:     netlink.SCOPE_UNIVERSE,
			Priority:  session.metric,
		}
		if err := c.handle.RouteReplace(newRoute); err != nil {
			if oldAddr == nil || !sameAddr(oldAddr, newAddr) {
				_ = c.handle.AddrDel(link, newAddr)
			}
			return nil, fmt.Errorf("replace DHCP default route: %w", err)
		}
	}
	if oldAddr != nil && !sameAddr(oldAddr, newAddr) {
		_ = c.handle.AddrDel(link, oldAddr)
	}
	oldRoute := session.currentRoute()
	if oldRoute != nil && !sameRoute(oldRoute, newRoute) {
		_ = c.handle.RouteDel(oldRoute)
	}
	session.setResources(newAddr, newRoute)

	renewalTime := raw.ACK.IPAddressRenewalTime(leaseTime / 2)
	rebindingTime := raw.ACK.IPAddressRebindingTime(leaseTime * 7 / 8)
	result := &Lease{
		Family:        FamilyIPv4,
		Address:       address,
		Gateway:       firstIPv4(raw.ACK.Router()),
		DNS:           cloneIPs(raw.ACK.DNS()),
		Server:        cloneIP(raw.ACK.ServerIdentifier()),
		LeaseTime:     leaseTime,
		RenewalTime:   renewalTime,
		RebindingTime: rebindingTime,
	}
	if err := c.setSessionDNS(session.name, session.dnsPath, result.DNS); err != nil {
		return nil, fmt.Errorf("apply DHCPv4 DNS: %w", err)
	}
	return result, nil
}
