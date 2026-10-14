package inbound_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/inbound"
	"github.com/we-apranik/apranik/spec"
)

type serverWithoutUsers struct{}

func (serverWithoutUsers) AcceptStream(context.Context, spec.Stream) (spec.Stream, spec.Metadata, error) {
	return nil, spec.Metadata{}, nil
}

func (serverWithoutUsers) IsDatagramOverStream(context.Context, spec.PeekableStream) (bool, error) {
	return false, nil
}

func (serverWithoutUsers) AcceptDatagramOverStream(context.Context, spec.Stream) (spec.Datagram, spec.Metadata, error) {
	return nil, spec.Metadata{}, nil
}

type serverWithUsers struct {
	serverWithoutUsers
	label string
}

func (serverWithUsers) AddUser(json.RawMessage) (string, error) { return "", nil }
func (serverWithUsers) RemoveUser(string) error                 { return nil }
func (s serverWithUsers) ListUsers() []string                   { return []string{s.label} }

func userManagerOfInbound(t *testing.T, config inbound.StreamConfig) spec.UserManager {
	t.Helper()
	managing, ok := inbound.NewStream(config).(component.UserManagingInbound)
	if !ok {
		t.Fatal("the stream inbound does not implement component.UserManagingInbound")
	}
	return managing.UserManager()
}

func TestTheUserManagerIsTheProxyServersWhenItHasOne(t *testing.T) {
	manager := userManagerOfInbound(t, inbound.StreamConfig{
		ProxyServer:              serverWithUsers{label: "proxy"},
		DatagramOverStreamServer: serverWithUsers{label: "datagram"},
	})

	if manager == nil || manager.ListUsers()[0] != "proxy" {
		t.Errorf("UserManager = %v, want the proxy server's", manager)
	}
}

func TestTheUserManagerFallsBackToTheDatagramOverStreamServer(t *testing.T) {
	manager := userManagerOfInbound(t, inbound.StreamConfig{
		ProxyServer:              serverWithoutUsers{},
		DatagramOverStreamServer: serverWithUsers{label: "datagram"},
	})

	if manager == nil || manager.ListUsers()[0] != "datagram" {
		t.Errorf("UserManager = %v, want the datagram-over-stream server's", manager)
	}
}

func TestTheUserManagerIsNilWhenNoServerCanChangeItsUsers(t *testing.T) {
	if manager := userManagerOfInbound(t, inbound.StreamConfig{ProxyServer: serverWithoutUsers{}, DatagramOverStreamServer: serverWithoutUsers{}}); manager != nil {
		t.Errorf("UserManager = %v, want nil", manager)
	}
	if manager := userManagerOfInbound(t, inbound.StreamConfig{ProxyServer: serverWithoutUsers{}}); manager != nil {
		t.Errorf("UserManager = %v, want nil with no datagram server", manager)
	}
}
