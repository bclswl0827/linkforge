//go:build linux

package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/bclswl0827/linkforge"
)

func main() {
	name := flag.String("interface", "eth0", "interface to configure")
	cidr := flag.String("address", "192.0.2.10/24", "IP address in CIDR notation")
	gateway := flag.String("gateway", "", "optional default gateway")
	metric := flag.Int("metric", 100, "default route metric")
	flag.Parse()

	ip, network, err := net.ParseCIDR(*cidr)
	if err != nil || ip == nil {
		log.Fatalf("invalid IP address %q", *cidr)
	}
	network.IP = ip

	var gatewayIP net.IP
	if *gateway != "" {
		gatewayIP = net.ParseIP(*gateway)
		if gatewayIP == nil {
			log.Fatalf("invalid gateway %q", *gateway)
		}
	}

	client, err := linkforge.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.SetUp(*name); err != nil {
		log.Fatal(err)
	}
	if _, err := client.Configure(context.Background(), linkforge.Config{
		Interface: *name,
		Mode:      linkforge.ModeStatic,
		Static: linkforge.StaticConfig{
			Address: network,
			Gateway: gatewayIP,
			Metric:  *metric,
		},
	}); err != nil {
		log.Fatal(err)
	}

	log.Printf("configured %s with %s", *name, network)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
