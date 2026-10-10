package registry

import (
	"slices"

	"github.com/The-Apranik/Apranik/spec"
)

// ProxyModules is one constructed proxy protocol. Config is the value a config
// file's settings are decoded into, once. The same value is each kind field the
// protocol fills, and the kinds it does not fill are nil.
type ProxyModules struct {
	Config             spec.Config
	Stream             spec.StreamProxyModule
	DatagramOverStream spec.DatagramOverStreamProxyModule
	Datagram           spec.DatagramProxyModule
}

// ProxyProtocolRegistry is the proxy namespace: a transport and a proxy
// protocol may share a name without colliding. A protocol has one entry under
// its name, whatever kinds it fills, so it is built and decoded once.
type ProxyProtocolRegistry struct {
	protocols map[string]func() ProxyModules

	registered index
}

type proxyModule interface {
	spec.Config
	Info() spec.ModuleInfo
}

func (r *ProxyProtocolRegistry) RegisterStream(newModule func() spec.StreamProxyModule) error {
	return registerProxy(r, newModule, []Kind{KindStreamProxy}, func(module spec.StreamProxyModule) ProxyModules {
		return ProxyModules{Config: module, Stream: module}
	})
}

func (r *ProxyProtocolRegistry) RegisterDatagramOverStream(newModule func() spec.DatagramOverStreamProxyModule) error {
	return registerProxy(r, newModule, []Kind{KindDatagramOverStreamProxy}, func(module spec.DatagramOverStreamProxyModule) ProxyModules {
		return ProxyModules{Config: module, DatagramOverStream: module}
	})
}

func (r *ProxyProtocolRegistry) RegisterDatagram(newModule func() spec.DatagramProxyModule) error {
	return registerProxy(r, newModule, []Kind{KindDatagramProxy}, func(module spec.DatagramProxyModule) ProxyModules {
		return ProxyModules{Config: module, Datagram: module}
	})
}

func (r *ProxyProtocolRegistry) RegisterStreamAndDatagramOverStream(newModule func() spec.StreamAndDatagramOverStreamProxyModule) error {
	return registerProxy(r, newModule, []Kind{KindStreamProxy, KindDatagramOverStreamProxy}, func(module spec.StreamAndDatagramOverStreamProxyModule) ProxyModules {
		return ProxyModules{Config: module, Stream: module, DatagramOverStream: module}
	})
}

func (r *ProxyProtocolRegistry) RegisterStreamDatagramOverStreamAndDatagram(newModule func() spec.StreamDatagramOverStreamAndDatagramProxyModule) error {
	return registerProxy(r, newModule, []Kind{KindStreamProxy, KindDatagramOverStreamProxy, KindDatagramProxy}, func(module spec.StreamDatagramOverStreamAndDatagramProxyModule) ProxyModules {
		return ProxyModules{Config: module, Stream: module, DatagramOverStream: module, Datagram: module}
	})
}

// Lookup returns the constructor of the protocol called name, which must fill
// the wanted kind: a protocol that does not is the same error as one that is
// not registered, with the kinds it does fill listed.
func (r *ProxyProtocolRegistry) Lookup(name string, wanted Kind) (func() ProxyModules, error) {
	newModules, found := r.protocols[name]
	registered := r.registered.kinds(name)
	if !found || !slices.Contains(registered, wanted) {
		return nil, &UnknownModuleError{Name: name, Want: wanted, Registered: registered}
	}
	return newModules, nil
}

// Kinds reports which kinds a name fills.
func (r *ProxyProtocolRegistry) Kinds(name string) []Kind { return r.registered.kinds(name) }

// A method cannot take a type parameter, so the typed Register methods above
// share this function: modulesOf says which fields the constructor's value
// fills, and the compiler checks the value against them.
func registerProxy[Module proxyModule](
	r *ProxyProtocolRegistry,
	newModule func() Module,
	kinds []Kind,
	modulesOf func(Module) ProxyModules,
) error {
	info, err := describe(newModule, kinds[0])
	if err != nil {
		return err
	}
	if err := r.claim(info, kinds); err != nil {
		return err
	}

	if r.protocols == nil {
		r.protocols = make(map[string]func() ProxyModules)
	}
	r.protocols[info.Name] = func() ProxyModules { return modulesOf(newModule()) }
	return nil
}

func (r *ProxyProtocolRegistry) claim(info spec.ModuleInfo, kinds []Kind) error {
	switch info.Name {
	case FreedomProtocolName, BlockProtocolName, MixedProtocolName, URLTestProtocolName, FallbackProtocolName, SelectorProtocolName:
		return &ReservedNameError{Kind: kinds[0], Name: info.Name}
	}
	if len(r.registered.kinds(info.Name)) > 0 {
		return &DuplicateModuleError{Kind: kinds[0], Name: info.Name}
	}
	for _, kind := range kinds {
		r.registered.record(info, kind)
	}
	return nil
}

func (r *ProxyProtocolRegistry) Modules() []Listing { return r.registered.listings() }
