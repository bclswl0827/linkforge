//go:build linux

package linkforge

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
)

func (c *Client) configureDHCP6(parent context.Context, name string, cfg DHCPConfig) (*Lease, error) {
	link, err := c.link(name)
	if err != nil {
		return nil, err
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %q is down; call SetUp before Configure", name)
	}

	options := make([]nclient6.ClientOpt, 0, 2)
	if cfg.Timeout > 0 {
		options = append(options, nclient6.WithTimeout(cfg.Timeout))
	}
	if cfg.Retries > 0 {
		options = append(options, nclient6.WithRetry(cfg.Retries))
	}
	dhcpClient, err := nclient6.New(name, options...)
	if err != nil {
		return nil, fmt.Errorf("create DHCPv6 client for %q: %w", name, err)
	}
	raConn, ifi, err := listenRA(name)
	if err != nil {
		_ = dhcpClient.Close()
		return nil, err
	}

	leaseCtx, cancel := context.WithCancel(parent)
	session := &dhcp6Session{
		name:      name,
		linkIndex: link.Attrs().Index,
		client:    dhcpClient,
		raConn:    raConn,
		cancel:    cancel,
		done:      make(chan struct{}),
		raDone:    make(chan struct{}),
		dns:       make(map[string]raDNSServer),
		metric:    cfg.Metric,
		dnsPath:   dnsPath(cfg),
	}
	rawLease, err := dhcpClient.RapidSolicit(leaseCtx)
	if err != nil {
		abortDHCP6(session, c)
		return nil, fmt.Errorf("request DHCPv6 lease on %q: %w", name, err)
	}
	session.setLease(rawLease)
	advertisement, router, err := solicitRA(leaseCtx, raConn, ifi, cfg.RATimeout)
	if err != nil {
		abortDHCP6(session, c)
		return nil, fmt.Errorf("configure IPv6 RA on %q: %w", name, err)
	}
	if err := c.applyRA(session, advertisement, router); err != nil {
		abortDHCP6(session, c)
		return nil, fmt.Errorf("configure IPv6 RA on %q: %w", name, err)
	}
	result, err := c.applyLease6(session, rawLease)
	if err != nil {
		abortDHCP6(session, c)
		return nil, fmt.Errorf("apply DHCPv6 lease on %q: %w", name, err)
	}

	c.mu.Lock()
	c.sessions[name] = session
	c.mu.Unlock()
	go c.maintainDHCP6(leaseCtx, session)
	go c.maintainRA(leaseCtx, session)
	return result, nil
}

func abortDHCP6(session *dhcp6Session, c *Client) {
	session.cancel()
	_ = session.client.Close()
	_ = session.raConn.Close()
	_ = session.cleanupResources(c)
	_ = c.removeSessionDNS(session.name, session.dnsPath)
}

func (c *Client) maintainDHCP6(ctx context.Context, session *dhcp6Session) {
	defer close(session.done)
	var retryWait time.Duration
	for {
		rawLease := session.currentLease()
		wait := retryWait
		if wait <= 0 {
			wait = timeUntilRenewal6(rawLease)
		}
		retryWait = 0
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-timer.C:
		}

		acquireCtx, acquireCancel := context.WithTimeout(ctx, 30*time.Second)
		updated, err := session.client.RapidSolicit(acquireCtx)
		acquireCancel()
		if err == nil {
			if _, err = c.applyLease6(session, updated); err == nil {
				session.setLease(updated)
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		// nclient6 does not expose Renew. Retry Solicitation after the
		// existing IA_NA lifetime has reached T1 or after five seconds.
		retryWait = 5 * time.Second
	}
}
