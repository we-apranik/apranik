package registry

import (
	"errors"

	"github.com/we-apranik/apranik/module/api/clash"
	dnsdoh "github.com/we-apranik/apranik/module/dns/doh"
	dnsdot "github.com/we-apranik/apranik/module/dns/dot"
	dnstcp "github.com/we-apranik/apranik/module/dns/tcp"
	dnsudp "github.com/we-apranik/apranik/module/dns/udp"

	"github.com/we-apranik/apranik/module/proxy/http"
	"github.com/we-apranik/apranik/module/proxy/socks5"
	"github.com/we-apranik/apranik/module/proxy/trojan"
	"github.com/we-apranik/apranik/module/proxy/vless"
	httpsniffer "github.com/we-apranik/apranik/module/sniffer/http"
	tlssniffer "github.com/we-apranik/apranik/module/sniffer/tls"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/module/transport/base/udp"
	"github.com/we-apranik/apranik/module/transport/overlay/fragment"
	"github.com/we-apranik/apranik/module/transport/overlay/httpupgrade"
	"github.com/we-apranik/apranik/module/transport/overlay/proxyprotocol"
	tlstransport "github.com/we-apranik/apranik/module/transport/overlay/tls"
	"github.com/we-apranik/apranik/module/transport/overlay/websocket"
)

// Default is the only place in the project that names a module. Adding one is
// a new folder plus one line here.
func Default() (*Registry, error) {
	moduleRegistry := New()

	return moduleRegistry, errors.Join(
		moduleRegistry.Transports().RegisterBaseStream(tcp.NewBaseStreamTransportModule),
		moduleRegistry.Transports().RegisterBaseDatagram(udp.NewBaseDatagramTransportModule),
		moduleRegistry.Transports().RegisterOverlayStream(tlstransport.NewOverlayStreamTransportModule),
		moduleRegistry.Transports().RegisterOverlayStream(websocket.NewOverlayStreamTransportModule),
		moduleRegistry.Transports().RegisterOverlayStream(httpupgrade.NewOverlayStreamTransportModule),
		moduleRegistry.Transports().RegisterOverlayStream(fragment.NewOverlayStreamTransportModule),
		moduleRegistry.Transports().RegisterOverlayStream(proxyprotocol.NewOverlayStreamTransportModule),
		moduleRegistry.ProxyProtocols().RegisterStream(socks5.NewProxyModule),
		moduleRegistry.ProxyProtocols().RegisterStream(http.NewProxyModule),
		moduleRegistry.ProxyProtocols().RegisterStreamAndDatagramOverStream(trojan.NewProxyModule),
		moduleRegistry.ProxyProtocols().RegisterStreamAndDatagramOverStream(vless.NewProxyModule),
		moduleRegistry.Sniffers().Register(tlssniffer.NewSnifferModule),
		moduleRegistry.Sniffers().Register(httpsniffer.NewSnifferModule),
		moduleRegistry.DNSServers().RegisterDatagram(dnsudp.NewDatagramDNSServerModule),
		moduleRegistry.DNSServers().RegisterStream(dnstcp.NewStreamDNSServerModule),
		moduleRegistry.DNSServers().RegisterStream(dnsdot.NewStreamDNSServerModule),
		moduleRegistry.DNSServers().RegisterStream(dnsdoh.NewStreamDNSServerModule),
		moduleRegistry.APIs().Register(clash.NewAPIModule),
	)
}
