//go:build linux

package linkforge

import (
	"errors"
	"net"
)

func normalizeIPNet(input *net.IPNet) (*net.IPNet, error) {
	if input == nil || input.IP == nil {
		return nil, errors.New("address is nil")
	}
	ones, bits := input.Mask.Size()
	if bits != net.IPv4len*8 && bits != net.IPv6len*8 {
		return nil, errors.New("address mask is invalid")
	}
	if bits == net.IPv4len*8 {
		ip := input.IP.To4()
		if ip == nil {
			return nil, errors.New("IPv4 mask used with non-IPv4 address")
		}
		return &net.IPNet{IP: cloneIP(ip), Mask: net.CIDRMask(ones, bits)}, nil
	}
	if input.IP.To4() != nil {
		return nil, errors.New("IPv6 mask used with IPv4 address")
	}
	return &net.IPNet{IP: cloneIP(input.IP.To16()), Mask: net.CIDRMask(ones, bits)}, nil
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	return append(net.IP(nil), ip...)
}
