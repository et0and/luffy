package shoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// Listener receives global chat messages, mirroring discover.py.
//
// Incoming shoutbox packets are HTML lines sent by another box. Duplicates
// are ignored, then the content is handed to Handler which normally appends
// it to the chat store and regenerates the HTML frame.
type Listener struct {
	Addr     string
	Handler  func(content string) error
	Logger   *slog.Logger
	mu       sync.Mutex
	lastSeen string
}

// ListenAndServe binds a UDP socket and processes datagrams until ctx is
// cancelled. The default address is [::]:12556, matching the original dual
// stack handling.
func (l *Listener) ListenAndServe(ctx context.Context) error {
	addr := l.Addr
	if addr == "" {
		addr = fmt.Sprintf(":%d", Port)
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("shoutbox: listen %s: %w", addr, err)
	}
	return l.Serve(ctx, conn)
}

// Serve processes datagrams from an existing connection until ctx is
// cancelled. It is used by tests with an ephemeral socket.
func (l *Listener) Serve(ctx context.Context, conn net.PacketConn) error {
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 64*1024)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("shoutbox: read: %w", err)
		}
		l.handle(string(buf[:n]))
	}
}

// Handle processes one raw datagram. Exported for tests.
func (l *Listener) Handle(raw string) {
	l.handle(raw)
}

func (l *Listener) handle(raw string) {
	msg, err := Parse(raw)
	if err != nil {
		if errors.Is(err, ErrNotPiratebox) {
			return
		}
		if l.Logger != nil {
			l.Logger.Warn("ignoring malformed broadcast", "error", err)
		}
		return
	}
	if msg.Type != TypeShoutbox {
		if l.Logger != nil {
			l.Logger.Debug("ignoring non-shoutbox message", "type", msg.Type)
		}
		return
	}
	l.mu.Lock()
	if raw == l.lastSeen {
		l.mu.Unlock()
		return
	}
	l.lastSeen = raw
	l.mu.Unlock()

	content, err := msg.Content()
	if err != nil {
		if l.Logger != nil {
			l.Logger.Warn("undecodable shoutbox message", "error", err)
		}
		return
	}
	if l.Handler == nil {
		return
	}
	if err := l.Handler(content); err != nil && l.Logger != nil {
		l.Logger.Error("shoutbox handler failed", "error", err)
	}
}

// LastSeen returns the most recently accepted packet, for diagnostics.
func (l *Listener) LastSeen() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastSeen
}
