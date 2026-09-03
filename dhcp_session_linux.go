//go:build linux

package linkforge

import (
	"context"
	"errors"
	"sync"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	netlink "github.com/vishvananda/netlink"
)

type dhcp4Session struct {
	name    string
	client  *nclient4.Client
	cancel  context.CancelFunc
	done    chan struct{}
	dnsPath string

	leaseMu sync.RWMutex
	lease   *nclient4.Lease
	addr    *netlink.Addr
	route   *netlink.Route
	metric  int
}

func (c *Client) stopDHCP(name string) error {
	c.mu.Lock()
	session := c.sessions[name]
	delete(c.sessions, name)
	c.mu.Unlock()
	if session == nil {
		return nil
	}
	return c.stopSession(session)
}

type dhcpSession interface {
	stop(*Client) error
}

func (c *Client) stopSession(session dhcpSession) error {
	return session.stop(c)
}

func (s *dhcp4Session) stop(c *Client) error {
	s.cancel()
	// Closing the packet connection unblocks a pending DHCP exchange.
	closeErr := s.client.Close()
	<-s.done
	return errors.Join(closeErr, s.cleanupResources(c), c.removeSessionDNS(s.name, s.dnsPath))
}

func (s *dhcp4Session) cleanupResources(c *Client) error {
	// Client.Close marks the client unavailable, so use the handle directly
	// here to allow cleanup of session resources during Client.Close.
	link, err := c.handle.LinkByName(s.name)
	if err != nil {
		return err
	}
	var cleanupErr error
	if addr := s.currentAddr(); addr != nil {
		cleanupErr = errors.Join(cleanupErr, ignoreMissing(c.handle.AddrDel(link, addr)))
	}
	if route := s.currentRoute(); route != nil {
		cleanupErr = errors.Join(cleanupErr, ignoreMissing(c.handle.RouteDel(route)))
	}
	return cleanupErr
}

func (s *dhcp4Session) currentLease() *nclient4.Lease {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.lease
}

func (s *dhcp4Session) setLease(lease *nclient4.Lease) {
	s.leaseMu.Lock()
	s.lease = lease
	s.leaseMu.Unlock()
}

func (s *dhcp4Session) currentAddr() *netlink.Addr {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.addr
}

func (s *dhcp4Session) currentRoute() *netlink.Route {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.route
}

func (s *dhcp4Session) setResources(addr *netlink.Addr, route *netlink.Route) {
	s.leaseMu.Lock()
	s.addr = addr
	s.route = route
	s.leaseMu.Unlock()
}
