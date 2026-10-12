package muxcool

import (
	"errors"
	"net"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

// The 65535 IDs of one main connection cannot be opened in a test one by one, so
// the counter is moved close to the end.
func TestTheIDSpaceOfAMainConnectionRunsOut(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	client := NewClient(clientSide, ClientOptions{IdleTimeout: -1})
	defer client.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, err := serverSide.Read(buffer); err != nil {
				return
			}
		}
	}()

	client.connection.mutex.Lock()
	client.connection.nextID = idCount - 3
	client.connection.mutex.Unlock()

	destination, _ := spec.NewDestinationFromDomain("example.com", 80)
	var ids []uint16
	for range 2 {
		sub, err := client.connection.open(NetworkTCP, destination)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sub.id)
	}
	if ids[0] != idCount-3 || ids[1] != idCount-2 {
		t.Fatalf("IDs = %v", ids)
	}
	if !client.CanOpenSubConnection() {
		t.Error("CanOpenSubConnection = false with one ID left")
	}
	last, err := client.connection.open(NetworkTCP, destination)
	if err != nil || last.id != 65535 {
		t.Fatalf("the last ID = %v, %v; want 65535", last, err)
	}
	if client.CanOpenSubConnection() {
		t.Error("CanOpenSubConnection = true with every ID used")
	}
	if _, err := client.connection.open(NetworkTCP, destination); !errors.Is(err, ErrSubConnectionLimit) {
		t.Errorf("open past the last ID = %v, want ErrSubConnectionLimit", err)
	}
}
