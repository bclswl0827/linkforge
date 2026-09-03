//go:build linux

package linkforge

import (
	"errors"
	"fmt"
	"net"

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
		Family:    netlink.FAMILY_V4,
		Scope:     netlink.SCOPE_UNIVERSE,
		Priority:  cfg.Metric,
	}
	if address.IP.To4() == nil {
		route.Family = netlink.FAMILY_V6
	}
	if err := c.handle.RouteReplace(route); err != nil {
		return fmt.Errorf("replace default route on %q: %w", name, err)
	}
	return nil
}
