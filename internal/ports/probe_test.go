package ports

import (
	"net"
	"slices"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func listenLocal(t *testing.T) (net.Listener, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", l.Addr())
	}
	return l, addr.Port
}

func TestListenersReportsOnlyHeldPorts(t *testing.T) {
	held, busy := listenLocal(t)
	defer held.Close()
	freed, free := listenLocal(t)
	freed.Close()

	got, err := Listeners([]int{free, busy})
	if err != nil {
		t.Fatalf("Listeners: %v", err)
	}
	if want := []int{busy}; !slices.Equal(got, want) {
		t.Fatalf("Listeners = %v, want %v", got, want)
	}
}

func TestSlotPorts(t *testing.T) {
	policy := config.PortPolicy{Stride: 10, Ports: []config.Port{{Name: "web", Base: 5173}, {Name: "db", Base: 5432}}, Limit: 6010}
	if got, want := SlotPorts(policy, 2), []int{5193, 5452}; !slices.Equal(got, want) {
		t.Fatalf("SlotPorts = %v, want %v", got, want)
	}
}
