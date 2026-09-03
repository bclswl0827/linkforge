//go:build linux

package linkforge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/mdlayher/ndp"
	netlink "github.com/vishvananda/netlink"
	"golang.org/x/net/ipv6"
)

const (
	defaultRATimeout      = 10 * time.Second
	routerSolicitInterval = 4 * time.Second
	maxRouterSolicits     = 3
)

var allRouters = netip.MustParseAddr("ff02::2")

type raDNSServer struct {
	ip      net.IP
	expires time.Time
}

func listenRA(name string) (*ndp.Conn, *net.Interface, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return nil, nil, fmt.Errorf("look up interface for IPv6 RA: %w", err)
	}
	conn, _, err := ndp.Listen(ifi, ndp.LinkLocal)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for IPv6 RA on %q: %w", name, err)
	}

	var filter ipv6.ICMPFilter
	filter.SetAll(true)
	filter.Accept(ipv6.ICMPTypeRouterAdvertisement)
	if err := conn.SetICMPFilter(&filter); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("filter IPv6 RA on %q: %w", name, err)
	}
	if err := conn.SetControlMessage(ipv6.FlagHopLimit, true); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("enable IPv6 RA hop-limit validation on %q: %w", name, err)
	}
	return conn, ifi, nil
}

func solicitRA(ctx context.Context, conn *ndp.Conn, ifi *net.Interface, timeout time.Duration) (*ndp.RouterAdvertisement, netip.Addr, error) {
	if timeout <= 0 {
		timeout = defaultRATimeout
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}

	solicitation := &ndp.RouterSolicitation{}
	if len(ifi.HardwareAddr) == 6 {
		solicitation.Options = append(solicitation.Options, &ndp.LinkLayerAddress{
			Direction: ndp.Source,
			Addr:      ifi.HardwareAddr,
		})
	}

	var nextSolicit time.Time
	for sent := 0; ; {
		if err := ctx.Err(); err != nil {
			return nil, netip.Addr{}, err
		}
		now := time.Now()
		if !now.Before(deadline) {
			return nil, netip.Addr{}, errors.New("timed out waiting for IPv6 router advertisement")
		}
		if sent < maxRouterSolicits && (nextSolicit.IsZero() || !now.Before(nextSolicit)) {
			if err := conn.WriteTo(solicitation, nil, allRouters); err != nil {
				return nil, netip.Addr{}, fmt.Errorf("send IPv6 router solicitation: %w", err)
			}
			sent++
			nextSolicit = now.Add(routerSolicitInterval)
		}

		readDeadline := now.Add(time.Second)
		if deadline.Before(readDeadline) {
			readDeadline = deadline
		}
		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return nil, netip.Addr{}, fmt.Errorf("set IPv6 RA read deadline: %w", err)
		}
		message, control, from, err := conn.ReadFrom()
		if err != nil {
			if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
				continue
			}
			return nil, netip.Addr{}, fmt.Errorf("receive IPv6 router advertisement: %w", err)
		}
		advertisement, ok := validRA(message, control, from)
		if !ok || advertisement.RouterLifetime <= 0 {
			continue
		}
		return advertisement, from, nil
	}
}

func validRA(message ndp.Message, control *ipv6.ControlMessage, from netip.Addr) (*ndp.RouterAdvertisement, bool) {
	advertisement, ok := message.(*ndp.RouterAdvertisement)
	if !ok || control == nil || control.HopLimit != ndp.HopLimit {
		return nil, false
	}
	if !from.Is6() || !from.IsLinkLocalUnicast() {
		return nil, false
	}
	return advertisement, true
}

func (c *Client) applyRA(session *dhcp6Session, advertisement *ndp.RouterAdvertisement, from netip.Addr) error {
	gateway := net.IP(from.WithZone("").AsSlice())
	oldRoute := session.currentRoute()
	oldGateway, _ := session.raConfiguration(time.Now())

	if advertisement.RouterLifetime <= 0 {
		if oldRoute != nil && oldGateway.Equal(gateway) {
			if err := ignoreMissing(c.handle.RouteDel(oldRoute)); err != nil {
				return fmt.Errorf("remove expired IPv6 RA default route: %w", err)
			}
			session.setRARoute(nil, nil, time.Time{})
		}
		session.updateRADNS(advertisement.Options, time.Now())
		return nil
	}

	route := &netlink.Route{
		LinkIndex: session.linkIndex,
		Gw:        cloneIP(gateway),
		Family:    netlink.FAMILY_V6,
		Scope:     netlink.SCOPE_UNIVERSE,
		Priority:  session.metric,
	}
	if err := c.handle.RouteReplace(route); err != nil {
		return fmt.Errorf("replace IPv6 RA default route: %w", err)
	}
	if oldRoute != nil && !sameRoute(oldRoute, route) {
		_ = c.handle.RouteDel(oldRoute)
	}
	now := time.Now()
	session.setRARoute(route, gateway, now.Add(advertisement.RouterLifetime))
	session.updateRADNS(advertisement.Options, now)
	return nil
}

func (c *Client) maintainRA(ctx context.Context, session *dhcp6Session) {
	defer close(session.raDone)
	for {
		dnsChanged, err := session.expireRA(c, time.Now())
		if dnsChanged {
			_ = c.setSessionDNS(session.name, session.dnsPath, session.currentDNS(time.Now()))
		}
		if err != nil && ctx.Err() != nil {
			return
		}
		if err := session.raConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return
		}
		message, control, from, err := session.raConn.ReadFrom()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
				continue
			}
			return
		}
		advertisement, ok := validRA(message, control, from)
		if !ok {
			continue
		}
		if err := c.applyRA(session, advertisement, from); err == nil {
			_ = c.setSessionDNS(session.name, session.dnsPath, session.currentDNS(time.Now()))
		}
	}
}

func (s *dhcp6Session) setRARoute(route *netlink.Route, gateway net.IP, expires time.Time) {
	s.leaseMu.Lock()
	s.route = route
	s.gateway = cloneIP(gateway)
	s.routeExpires = expires
	s.leaseMu.Unlock()
}

func (s *dhcp6Session) updateRADNS(options []ndp.Option, now time.Time) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	for _, option := range options {
		rdnss, ok := option.(*ndp.RecursiveDNSServer)
		if !ok {
			continue
		}
		for _, server := range rdnss.Servers {
			key := server.String()
			if rdnss.Lifetime <= 0 {
				delete(s.dns, key)
				continue
			}
			s.dns[key] = raDNSServer{
				ip:      net.IP(server.AsSlice()),
				expires: now.Add(rdnss.Lifetime),
			}
		}
	}
}

func (s *dhcp6Session) raConfiguration(now time.Time) (net.IP, []net.IP) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	var gateway net.IP
	if s.route != nil && now.Before(s.routeExpires) {
		gateway = cloneIP(s.gateway)
	}
	keys := make([]string, 0, len(s.dns))
	for key, server := range s.dns {
		if !now.Before(server.expires) {
			delete(s.dns, key)
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	dns := make([]net.IP, 0, len(keys))
	for _, key := range keys {
		dns = append(dns, cloneIP(s.dns[key].ip))
	}
	return gateway, dns
}

func (s *dhcp6Session) expireRA(c *Client, now time.Time) (bool, error) {
	s.leaseMu.Lock()
	dnsChanged := false
	for key, server := range s.dns {
		if !now.Before(server.expires) {
			delete(s.dns, key)
			dnsChanged = true
		}
	}
	if s.route == nil || now.Before(s.routeExpires) {
		s.leaseMu.Unlock()
		return dnsChanged, nil
	}
	route := s.route
	s.leaseMu.Unlock()

	if err := ignoreMissing(c.handle.RouteDel(route)); err != nil {
		return dnsChanged, fmt.Errorf("remove expired IPv6 RA default route: %w", err)
	}
	s.setRARoute(nil, nil, time.Time{})
	return dnsChanged, nil
}
