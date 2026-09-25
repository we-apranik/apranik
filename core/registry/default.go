package registry

import (
	"errors"

	"github.com/The-Apranik/Apranik/module/proxy/http"
	"github.com/The-Apranik/Apranik/module/proxy/socks5"
	httpsniffer "github.com/The-Apranik/Apranik/module/sniffer/http"
	tlssniffer "github.com/The-Apranik/Apranik/module/sniffer/tls"
	"github.com/The-Apranik/Apranik/module/transport/tcp"
	"github.com/The-Apranik/Apranik/module/transport/udp"
)

// Default is the only place in the project that names a module. Adding one is
// a new folder plus one line here.
func Default() (*Registry, error) {
	moduleRegistry := New()

	return moduleRegistry, errors.Join(
		moduleRegistry.Transports().RegisterBaseStream(tcp.NewBaseStreamTransportModule),
		moduleRegistry.Transports().RegisterBaseDatagram(udp.NewBaseDatagramTransportModule),
		moduleRegistry.ProxyProtocols().RegisterStream(socks5.NewStreamProxyModule),
		moduleRegistry.ProxyProtocols().RegisterStream(http.NewStreamProxyModule),
		moduleRegistry.Sniffers().Register(tlssniffer.NewSnifferModule),
		moduleRegistry.Sniffers().Register(httpsniffer.NewSnifferModule),
	)
}
