package ports

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"

	"github.com/sustinbebustin/mws/internal/config"
)

// SlotPorts lists the port of every named port at slot, in declaration
// order: base + slot*stride.
func SlotPorts(policy config.PortPolicy, slot int) []int {
	out := make([]int, len(policy.Ports))
	for i, p := range policy.Ports {
		out[i] = p.Base + slot*policy.Stride
	}
	return out
}

// probeHosts are bound in turn; a listener on either the loopback or the
// wildcard address makes one of the two binds fail on Linux and macOS alike.
var probeHosts = []string{"127.0.0.1", ""}

// Listeners bind-checks each TCP port and returns, in input order, those
// that already have a listener. A bind failing for any reason other than
// the address being in use (a privileged port, for one) is an error, since
// the probe cannot tell whether the port is free.
func Listeners(ports []int) ([]int, error) {
	var busy []int
	for _, port := range ports {
		inUse, err := hasListener(port)
		if err != nil {
			return nil, err
		}
		if inUse {
			busy = append(busy, port)
		}
	}
	return busy, nil
}

func hasListener(port int) (bool, error) {
	for _, host := range probeHosts {
		l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if errors.Is(err, syscall.EADDRINUSE) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("probe port %d: %w", port, err)
		}
		if err := l.Close(); err != nil {
			return false, fmt.Errorf("probe port %d: close probe listener: %w", port, err)
		}
	}
	return false, nil
}
