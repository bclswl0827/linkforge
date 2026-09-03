//go:build linux

package linkforge

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/mdlayher/ndp"
	netlink "github.com/vishvananda/netlink"
)

type dhcp6Session struct {
	name      string
	linkIndex int
	client    *nclient6.Client
	raConn    *ndp.Conn
	cancel    context.CancelFunc
	done      chan struct{}
	raDone    chan struct{}
	dnsPath   string

	leaseMu      sync.RWMutex
	lease        *dhcpv6.Message
	addr         *netlink.Addr
	route        *netlink.Route
	gateway      net.IP
	routeExpires time.Time
	dns          map[string]raDNSServer
	metric       int
}

func (s *dhcp6Session) stop(c *Client) error {
	s.cancel()
	closeErr := errors.Join(s.client.Close(), s.raConn.Close())
	<-s.done
	<-s.raDone
	return errors.Join(closeErr, s.cleanupResources(c), c.removeSessionDNS(s.name, s.dnsPath))
}

func (s *dhcp6Session) cleanupResources(c *Client) error {
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

func (s *dhcp6Session) currentLease() *dhcpv6.Message {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.lease
}

func (s *dhcp6Session) setLease(lease *dhcpv6.Message) {
	s.leaseMu.Lock()
	s.lease = lease
	s.leaseMu.Unlock()
}

func (s *dhcp6Session) currentAddr() *netlink.Addr {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.addr
}

func (s *dhcp6Session) currentRoute() *netlink.Route {
	s.leaseMu.RLock()
	defer s.leaseMu.RUnlock()
	return s.route
}

func (s *dhcp6Session) setAddr(addr *netlink.Addr) {
	s.leaseMu.Lock()
	s.addr = addr
	s.leaseMu.Unlock()
}

func (s *dhcp6Session) currentDNS(now time.Time) []net.IP {
	_, raDNS := s.raConfiguration(now)
	lease := s.currentLease()
	if lease == nil {
		return raDNS
	}
	return mergeIPs(raDNS, lease.Options.DNS())
}
