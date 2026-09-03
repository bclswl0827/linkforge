//go:build linux

package main

import (
	"fmt"
	"log"

	"github.com/bclswl0827/linkforge"
	netlink "github.com/vishvananda/netlink"
)

func main() {
	client, err := linkforge.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	links, err := client.Netlink().LinkList()
	if err != nil {
		log.Fatal(err)
	}
	for _, link := range links {
		attrs := link.Attrs()
		fmt.Printf("link %s index=%d mtu=%d flags=%s hw=%s\n",
			attrs.Name, attrs.Index, attrs.MTU, attrs.Flags, attrs.HardwareAddr)

		addresses, err := client.Netlink().AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			log.Printf("addresses for %s: %v", attrs.Name, err)
			continue
		}
		for _, address := range addresses {
			fmt.Printf("  addr %s\n", address)
		}

		routes, err := client.Netlink().RouteList(link, netlink.FAMILY_ALL)
		if err != nil {
			log.Printf("routes for %s: %v", attrs.Name, err)
			continue
		}
		for _, route := range routes {
			fmt.Printf("  route %s\n", route)
		}
	}
}
