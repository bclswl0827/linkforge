//go:build linux

package linkforge

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

func (c *Client) configureDHCP(parent context.Context, name string, cfg DHCPConfig) (*Lease, error) {
	link, err := c.link(name)
	if err != nil {
		return nil, err
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %q is down; call SetUp before Configure", name)
	}

	options := make([]nclient4.ClientOpt, 0, 2)
	if cfg.Timeout > 0 {
		options = append(options, nclient4.WithTimeout(cfg.Timeout))
	}
	if cfg.Retries > 0 {
		options = append(options, nclient4.WithRetry(cfg.Retries))
	}
	dhcpClient, err := nclient4.New(name, options...)
	if err != nil {
		return nil, fmt.Errorf("create DHCPv4 client for %q: %w", name, err)
	}

	leaseCtx, cancel := context.WithCancel(parent)
	rawLease, err := dhcpClient.Request(leaseCtx)
	if err != nil {
		cancel()
		_ = dhcpClient.Close()
		return nil, fmt.Errorf("request DHCPv4 lease on %q: %w", name, err)
	}
	session := &dhcp4Session{
		name:    name,
		client:  dhcpClient,
		cancel:  cancel,
		done:    make(chan struct{}),
		lease:   rawLease,
		metric:  cfg.Metric,
		dnsPath: dnsPath(cfg),
	}
	result, err := c.applyLease(session, rawLease)
	if err != nil {
		abortDHCP4(session, c)
		return nil, fmt.Errorf("apply DHCPv4 lease on %q: %w", name, err)
	}

	c.mu.Lock()
	c.sessions[name] = session
	c.mu.Unlock()
	go c.maintainDHCP(leaseCtx, session)
	return result, nil
}

func abortDHCP4(session *dhcp4Session, c *Client) {
	session.cancel()
	_ = session.client.Close()
	_ = session.cleanupResources(c)
	_ = c.removeSessionDNS(session.name, session.dnsPath)
}

func (c *Client) maintainDHCP(ctx context.Context, session *dhcp4Session) {
	defer close(session.done)
	for {
		rawLease := session.currentLease()
		wait := timeUntilRenewal(rawLease)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-timer.C:
		}

		renewCtx, cancel := context.WithTimeout(ctx, wait)
		updated, err := session.client.Renew(renewCtx, rawLease)
		cancel()
		if err == nil {
			if _, err = c.applyLease(session, updated); err == nil {
				session.setLease(updated)
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}

		// A failed unicast renewal can mean the client crossed T2 or the
		// server moved. A fresh broadcast DORA transaction handles both cases.
		acquireCtx, acquireCancel := context.WithTimeout(ctx, 30*time.Second)
		updated, requestErr := session.client.Request(acquireCtx)
		acquireCancel()
		if requestErr == nil {
			if _, requestErr = c.applyLease(session, updated); requestErr == nil {
				session.setLease(updated)
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		// Keep the current address while retrying. The old lease remains
		// usable until its expiry, and Request will retry on the next cycle.
	}
}
