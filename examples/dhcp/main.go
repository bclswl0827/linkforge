//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bclswl0827/linkforge"
)

func main() {
	name := flag.String("interface", "eth0", "interface to configure")
	family := flag.String("family", "4", "DHCP address family: 4 or 6")
	wait := flag.Bool("wait", false, "keep running to renew the DHCP lease")
	writeDNS := flag.Bool("write-dns", false, "write learned DNS servers to a resolver file")
	dnsPath := flag.String("dns-path", "/etc/resolv.conf", "resolver file used with -write-dns")
	flag.Parse()

	dhcpFamily := linkforge.FamilyIPv4
	if *family == "6" {
		dhcpFamily = linkforge.FamilyIPv6
	} else if *family != "4" {
		log.Fatalf("invalid DHCP address family %q", *family)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := linkforge.New()
	if err != nil {
		log.Fatal(err)
	}

	if err := client.SetUp(*name); err != nil {
		_ = client.Close()
		log.Fatal(err)
	}
	lease, err := client.Configure(ctx, linkforge.Config{
		Interface: *name,
		Mode:      linkforge.ModeDHCP,
		DHCP: linkforge.DHCPConfig{
			Family:   dhcpFamily,
			Metric:   100,
			WriteDNS: *writeDNS,
			DNSPath:  *dnsPath,
		},
	})
	if err != nil {
		_ = client.Close()
		log.Fatal(err)
	}

	fmt.Printf("interface: %s\naddress: %s\ngateway: %s\ndns: %s\nlease: %s, renewal: %s\n",
		*name,
		lease.Address,
		lease.Gateway,
		joinIPs(lease.DNS),
		lease.LeaseTime,
		lease.RenewalTime,
	)
	if !*wait {
		return
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Print(err)
		}
	}()
	<-ctx.Done()
}

func joinIPs(ips []net.IP) string {
	values := make([]string, 0, len(ips))
	for _, ip := range ips {
		values = append(values, ip.String())
	}
	return strings.Join(values, ", ")
}
