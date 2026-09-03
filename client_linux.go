//go:build linux

package linkforge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	netlink "github.com/vishvananda/netlink"
)

// Mode selects how an interface receives its IP configuration.
type Mode uint8

const (
	ModeStatic Mode = iota
	ModeDHCP
)

// AddressFamily selects the address family used by DHCP.
type AddressFamily uint8

const (
	FamilyIPv4 AddressFamily = iota
	FamilyIPv6
)

// StaticConfig describes the address and optional default route to install.
// The interface should have one configuration owner. Existing addresses and
// routes are not flushed, but a matching default route may be replaced.
type StaticConfig struct {
	Address *net.IPNet
	Gateway net.IP
	Metric  int
}

// DHCPConfig controls the DHCP exchange and the default route installed
// from the lease. FamilyIPv4 is the default. For DHCPv6, the default route
// and additional DNS servers are learned from Router Advertisements. A zero
// Timeout or Retries uses dependency defaults. A zero RATimeout uses ten
// seconds. If WriteDNS is true, DNSPath is replaced with the DNS servers from
// DHCP and RA; an empty DNSPath defaults to /etc/resolv.conf.
type DHCPConfig struct {
	Family    AddressFamily
	Timeout   time.Duration
	Retries   int
	Metric    int
	RATimeout time.Duration
	WriteDNS  bool
	DNSPath   string
}

// Config is the high-level interface configuration request.
type Config struct {
	Interface string
	Mode      Mode
	Static    StaticConfig
	DHCP      DHCPConfig
}

// Lease is the part of a DHCP lease needed by a network configuration consumer.
// DNS is always reported to the caller and is only written to a resolver file
// when DHCPConfig.WriteDNS is true. For DHCPv6, Gateway and DNS can include
// information learned from IPv6 Router Advertisements.
type Lease struct {
	Family        AddressFamily
	Address       *net.IPNet
	Gateway       net.IP
	DNS           []net.IP
	Server        net.IP
	LeaseTime     time.Duration
	RenewalTime   time.Duration
	RebindingTime time.Duration
}

// Client owns a rtnetlink handle and any DHCP sessions started through it.
// A Client is safe for concurrent use.
type Client struct {
	handle *netlink.Handle

	mu       sync.Mutex
	closed   bool
	sessions map[string]dhcpSession
	locks    map[string]*sync.Mutex

	dnsMu    sync.Mutex
	dnsFiles map[string]*managedDNSFile
}

// New creates a client with a dedicated rtnetlink handle.
func New() (*Client, error) {
	h, err := netlink.NewHandle()
	if err != nil {
		return nil, fmt.Errorf("open rtnetlink handle: %w", err)
	}
	return &Client{
		handle:   h,
		sessions: make(map[string]dhcpSession),
		locks:    make(map[string]*sync.Mutex),
		dnsFiles: make(map[string]*managedDNSFile),
	}, nil
}

// Netlink returns the underlying handle so callers retain access to the full
// vishvananda/netlink API. The handle is owned by Client and must not be closed
// separately.
func (c *Client) Netlink() *netlink.Handle {
	return c.handle
}

// Close stops DHCP sessions, removes resources owned by those sessions, and
// closes the rtnetlink handle. Close is idempotent.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	sessions := make([]dhcpSession, 0, len(c.sessions))
	for name, session := range c.sessions {
		delete(c.sessions, name)
		sessions = append(sessions, session)
	}
	c.mu.Unlock()

	var joined error
	for _, session := range sessions {
		joined = errors.Join(joined, c.stopSession(session))
	}
	c.handle.Close()
	return joined
}

// Configure applies a static address or starts a managed DHCP session.
// Configure does not automatically bring the interface up; call SetUp first.
// Applying a new configuration stops an existing DHCP session for the same
// interface and removes resources installed by that session.
func (c *Client) Configure(ctx context.Context, cfg Config) (*Lease, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	unlock, err := c.lockInterface(cfg.Interface)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := c.stopDHCP(cfg.Interface); err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case ModeStatic:
		if err := c.configureStatic(cfg.Interface, cfg.Static); err != nil {
			return nil, err
		}
		return nil, nil
	case ModeDHCP:
		switch cfg.DHCP.Family {
		case FamilyIPv4:
			return c.configureDHCP(ctx, cfg.Interface, cfg.DHCP)
		case FamilyIPv6:
			return c.configureDHCP6(ctx, cfg.Interface, cfg.DHCP)
		default:
			return nil, fmt.Errorf("unknown DHCP address family %d", cfg.DHCP.Family)
		}
	default:
		return nil, fmt.Errorf("unknown mode %d", cfg.Mode)
	}
}

// StopDHCP stops and cleans up the DHCP session for an interface. It does not
// change the administrative state of the link. The caller must ensure that no
// other network manager changes the same interface concurrently.
func (c *Client) StopDHCP(name string) error {
	unlock, err := c.lockInterface(name)
	if err != nil {
		return err
	}
	defer unlock()
	return c.stopDHCP(name)
}
