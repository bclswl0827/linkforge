//go:build linux

package linkforge

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	netlink "github.com/vishvananda/netlink"
)

func (c *Client) applyLease6(session *dhcp6Session, raw *dhcpv6.Message) (*Lease, error) {
	if raw == nil {
		return nil, errors.New("DHCPv6 reply is nil")
	}
	iana := raw.Options.OneIANA()
	if iana == nil || iana.Options.OneAddress() == nil {
		return nil, errors.New("DHCPv6 reply has no IA_NA address")
	}
	iaaddr := iana.Options.OneAddress()
	ip := iaaddr.IPv6Addr.To16()
	if ip == nil || iaaddr.IPv6Addr.To4() != nil {
		return nil, errors.New("DHCPv6 IA_NA contains an invalid IPv6 address")
	}
	if iaaddr.ValidLifetime <= 0 {
		return nil, errors.New("DHCPv6 IA_NA address has expired")
	}

	link, err := c.link(session.name)
	if err != nil {
		return nil, err
	}
	address := &net.IPNet{IP: cloneIP(ip), Mask: net.CIDRMask(128, 128)}
	newAddr := &netlink.Addr{
		IPNet:       address,
		LinkIndex:   link.Attrs().Index,
		PreferedLft: int(iaaddr.PreferredLifetime / time.Second),
		ValidLft:    int(iaaddr.ValidLifetime / time.Second),
	}
	oldAddr := session.currentAddr()
	if err := c.handle.AddrReplace(link, newAddr); err != nil {
		return nil, fmt.Errorf("replace DHCPv6 address: %w", err)
	}
	if oldAddr != nil && !sameAddr(oldAddr, newAddr) {
		_ = c.handle.AddrDel(link, oldAddr)
	}
	session.setAddr(newAddr)
	gateway, raDNS := session.raConfiguration(time.Now())

	result := &Lease{
		Family:        FamilyIPv6,
		Address:       address,
		Gateway:       gateway,
		DNS:           mergeIPs(raDNS, raw.Options.DNS()),
		LeaseTime:     iaaddr.ValidLifetime,
		RenewalTime:   iana.T1,
		RebindingTime: iana.T2,
	}
	if err := c.setSessionDNS(session.name, session.dnsPath, result.DNS); err != nil {
		return nil, fmt.Errorf("apply DHCPv6 DNS: %w", err)
	}
	return result, nil
}
