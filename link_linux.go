//go:build linux

package linkforge

import (
	"errors"
	"fmt"
	"sync"

	netlink "github.com/vishvananda/netlink"
)

// SetUp brings an interface administratively up.
func (c *Client) SetUp(name string) error {
	unlock, err := c.lockInterface(name)
	if err != nil {
		return err
	}
	defer unlock()

	link, err := c.link(name)
	if err != nil {
		return err
	}
	return c.handle.LinkSetUp(link)
}

// SetDown stops a DHCP session, removes its configured address and route, then
// brings the interface administratively down.
func (c *Client) SetDown(name string) error {
	unlock, err := c.lockInterface(name)
	if err != nil {
		return err
	}
	defer unlock()

	if err := c.stopDHCP(name); err != nil {
		return err
	}
	link, err := c.link(name)
	if err != nil {
		return err
	}
	return c.handle.LinkSetDown(link)
}

func (c *Client) link(name string) (netlink.Link, error) {
	if name == "" {
		return nil, errors.New("interface name is empty")
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, errors.New("client is closed")
	}
	link, err := c.handle.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("find interface %q: %w", name, err)
	}
	return link, nil
}

func (c *Client) lockInterface(name string) (func(), error) {
	if name == "" {
		return nil, errors.New("interface name is empty")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("client is closed")
	}
	lock := c.locks[name]
	if lock == nil {
		lock = &sync.Mutex{}
		c.locks[name] = lock
	}
	c.mu.Unlock()
	lock.Lock()
	return lock.Unlock, nil
}
