//go:build linux

package linkforge

import (
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	netlink "github.com/vishvananda/netlink"
)

func timeUntilRenewal(lease *nclient4.Lease) time.Duration {
	leaseTime := lease.ACK.IPAddressLeaseTime(time.Hour)
	renewalTime := lease.ACK.IPAddressRenewalTime(leaseTime / 2)
	if renewalTime <= 0 {
		renewalTime = leaseTime / 2
	}
	wait := renewalTime - time.Since(lease.CreationTime)
	if wait <= 0 {
		return 5 * time.Second
	}
	return wait
}

func timeUntilRenewal6(lease *dhcpv6.Message) time.Duration {
	iana := lease.Options.OneIANA()
	if iana == nil {
		return 5 * time.Second
	}
	wait := iana.T1
	if wait <= 0 {
		if address := iana.Options.OneAddress(); address != nil {
			wait = address.ValidLifetime / 2
		}
	}
	if wait <= 0 {
		return 5 * time.Second
	}
	return wait
}

func ignoreMissing(err error) error {
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENODEV) {
		return nil
	}
	return err
}

func sameAddr(a, b *netlink.Addr) bool {
	return a != nil && b != nil && a.IPNet != nil && b.IPNet != nil && a.IPNet.String() == b.IPNet.String()
}

func sameRoute(a, b *netlink.Route) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.LinkIndex == b.LinkIndex && a.Priority == b.Priority && a.Gw.Equal(b.Gw) && a.Dst == nil && b.Dst == nil
}

func firstIPv4(ips []net.IP) net.IP {
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return cloneIP(ip4)
		}
	}
	return nil
}

func cloneIPs(ips []net.IP) []net.IP {
	result := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		result = append(result, cloneIP(ip))
	}
	return result
}

func mergeIPs(groups ...[]net.IP) []net.IP {
	seen := make(map[string]struct{})
	var result []net.IP
	for _, ips := range groups {
		for _, ip := range ips {
			if ip == nil {
				continue
			}
			key := ip.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, cloneIP(ip))
		}
	}
	return result
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
