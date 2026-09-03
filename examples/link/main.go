//go:build linux

package main

import (
	"flag"
	"log"

	"github.com/bclswl0827/linkforge"
)

func main() {
	name := flag.String("interface", "eth0", "interface to change")
	up := flag.Bool("up", false, "bring the interface up")
	down := flag.Bool("down", false, "bring the interface down")
	flag.Parse()

	if *up == *down {
		log.Fatal("specify exactly one of -up or -down")
	}
	client, err := linkforge.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if *up {
		err = client.SetUp(*name)
	} else {
		err = client.SetDown(*name)
	}
	if err != nil {
		log.Fatal(err)
	}
}
