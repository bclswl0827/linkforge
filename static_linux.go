//go:build linux

package linkforge

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	netlink "github.com/vishvananda/netlink"
)

func (c *Client) configureStatic(name string, cfg StaticConfig) error {
	address, err := normalizeIPNet(cfg.Address)
	if err != nil {
		return fmt.Errorf("static address: %w", err)
	}
	var gateway net.IP
	if cfg.Gateway != nil {
		if address.IP.To4() != nil {
			gateway = cfg.Gateway.To4()
			if gateway == nil {
				return errors.New("static gateway must be an IPv4 address")
			}
		} else {
			gateway = cfg.Gateway.To16()
			if gateway == nil || cfg.Gateway.To4() != nil {
				return errors.New("static gateway must be an IPv6 address")
			}
		}
	}
	link, err := c.link(name)
	if err != nil {
		return err
	}
	family := netlink.FAMILY_V4
	if address.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	if cfg.FlushAddresses {
		addresses, err := c.handle.AddrList(link, family)
		if err != nil {
			return fmt.Errorf("list addresses on %q before flushing: %w", name, err)
		}
		for i := range addresses {
			oldAddress := &addresses[i]
			if family == netlink.FAMILY_V6 && oldAddress.IP.IsLinkLocalUnicast() {
				continue
			}
			// Removing a primary IPv4 address can also remove its secondaries.
			if err := c.handle.AddrDel(link, oldAddress); err != nil && !errors.Is(err, syscall.EADDRNOTAVAIL) {
				return fmt.Errorf("remove address %s on %q: %w", oldAddress.IPNet, name, err)
			}
		}
	}
	if err := c.handle.AddrReplace(link, &netlink.Addr{
		IPNet:     address,
		LinkIndex: link.Attrs().Index,
	}); err != nil {
		return fmt.Errorf("replace address on %q: %w", name, err)
	}
	if gateway == nil {
		return nil
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       nil,
		Gw:        cloneIP(gateway),
		Family:    family,
		Scope:     netlink.SCOPE_UNIVERSE,
		Priority:  cfg.Metric,
	}
	if err := c.handle.RouteReplace(route); err != nil {
		return fmt.Errorf("replace default route on %q: %w", name, err)
	}
	return nil
}
