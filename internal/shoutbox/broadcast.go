package shoutbox

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
)

// PacketSender abstracts UDP datagram delivery so the fan-out logic can be
// tested without touching the network.
type PacketSender interface {
	Send(destination string, port int, payload []byte) error
}

// UDPSender sends datagrams with the kernel default routing, using IPv6 for
// addresses that contain a colon and IPv4 broadcast otherwise.
type UDPSender struct{}

// Send implements PacketSender.
func (UDPSender) Send(destination string, port int, payload []byte) error {
	network := "udp4"
	if strings.Contains(destination, ":") {
		network = "udp6"
	}
	address := net.JoinHostPort(destination, strconv.Itoa(port))
	conn, err := net.Dial(network, address)
	if err != nil {
		return fmt.Errorf("shoutbox: dial %s: %w", address, err)
	}
	defer conn.Close()
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("shoutbox: send to %s: %w", address, err)
	}
	return nil
}

// Broadcaster fans a payload out to every configured destination, matching
// the behaviour of broadcast.py.
type Broadcaster struct {
	Destinations []string
	Port         int
	Sender       PacketSender
	Logger       *slog.Logger
}

// Send delivers payload to all destinations. Errors are logged and do not
// stop the remaining deliveries, like the original `continue` loop.
func (b *Broadcaster) Send(payload string) error {
	if len(b.Destinations) == 0 {
		return fmt.Errorf("shoutbox: no broadcast destinations configured")
	}
	port := b.Port
	if port == 0 {
		port = Port
	}
	sender := b.Sender
	if sender == nil {
		sender = UDPSender{}
	}
	var firstErr error
	for _, dest := range b.Destinations {
		if err := sender.Send(dest, port, []byte(payload)); err != nil {
			if b.Logger != nil {
				b.Logger.Warn("broadcast failed", "destination", dest, "error", err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
