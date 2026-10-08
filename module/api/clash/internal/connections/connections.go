// Package connections serves /connections: the live conversations, as a
// snapshot or pushed over a WebSocket, and closing them.
package connections

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/memory"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	defaultInterval = time.Second
	minimumInterval = 100 * time.Millisecond
)

func Register(router *web.Router, provider spec.APICoreProvider, hub *stream.Hub) {
	handler := handler{provider: provider, hub: hub}
	router.Handle("/connections", web.Methods{"GET": handler.list, "DELETE": handler.closeAll})
	router.Handle("/connections/{id}", web.Methods{"DELETE": handler.closeOne})
}

type handler struct {
	provider spec.APICoreProvider
	hub      *stream.Hub
}

type snapshotBody struct {
	DownloadTotal uint64           `json:"downloadTotal"`
	UploadTotal   uint64           `json:"uploadTotal"`
	Memory        uint64           `json:"memory"`
	Connections   []connectionBody `json:"connections"`
}

type connectionBody struct {
	ID          string       `json:"id"`
	Metadata    metadataBody `json:"metadata"`
	Upload      uint64       `json:"upload"`
	Download    uint64       `json:"download"`
	Start       time.Time    `json:"start"`
	Chains      []string     `json:"chains"`
	Rule        string       `json:"rule"`
	RulePayload string       `json:"rulePayload"`
}

type metadataBody struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	InboundName     string `json:"inboundName"`
	InboundUser     string `json:"inboundUser"`
	Host            string `json:"host"`
	SniffHost       string `json:"sniffHost"`
	DNSMode         string `json:"dnsMode"`
	ProcessPath     string `json:"processPath"`
}

// list answers once for a plain request and every interval milliseconds for a
// WebSocket.
func (h handler) list(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.snapshot()
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	if !web.IsWebSocketUpgrade(r) {
		web.WriteJSON(w, http.StatusOK, snapshot)
		return
	}

	interval, err := intervalOf(r)
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "%v", err)
		return
	}
	h.hub.Serve(w, r, func(messages *stream.Stream) {
		message, _ := json.Marshal(snapshot)
		if messages.Send(message) != nil {
			return
		}
		messages.EveryInterval(interval, func() ([]byte, bool) {
			snapshot, err := h.snapshot()
			if err != nil {
				return nil, false
			}
			message, _ := json.Marshal(snapshot)
			return message, true
		})
	})
}

func intervalOf(r *http.Request) (time.Duration, error) {
	text := r.URL.Query().Get("interval")
	if text == "" {
		return defaultInterval, nil
	}
	milliseconds, err := strconv.Atoi(text)
	if err != nil || time.Duration(milliseconds)*time.Millisecond < minimumInterval {
		return 0, web.BadParameter("interval is a whole number of milliseconds, at least %d", minimumInterval.Milliseconds())
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func (h handler) snapshot() (snapshotBody, error) {
	total, err := h.provider.TotalTraffic()
	if err != nil {
		return snapshotBody{}, err
	}
	conversations, err := h.provider.LiveConversations()
	if err != nil {
		return snapshotBody{}, err
	}
	snapshot := snapshotBody{
		DownloadTotal: total.DownlinkBytes,
		UploadTotal:   total.UplinkBytes,
		Memory:        memory.InUse(),
		Connections:   make([]connectionBody, len(conversations)),
	}
	for index, conversation := range conversations {
		snapshot.Connections[index] = connectionOf(conversation)
	}
	return snapshot, nil
}

func connectionOf(conversation spec.APILiveConversation) connectionBody {
	metadata := metadataBody{
		Network:     string(conversation.Network),
		Type:        inboundType(conversation.InboundProtocol),
		SourceIP:    conversation.Source.Addr().String(),
		SourcePort:  strconv.Itoa(int(conversation.Source.Port())),
		InboundName: conversation.InboundTag,
		InboundUser: conversation.User,
		SniffHost:   conversation.SniffedDomain,
		DNSMode:     "normal",
	}
	if !conversation.Source.IsValid() {
		metadata.SourceIP, metadata.SourcePort = "", ""
	}
	if domain, isDomain := conversation.Destination.Domain(); isDomain {
		metadata.Host = domain
	} else if address, isIP := conversation.Destination.IP(); isIP {
		metadata.DestinationIP = address.String()
	}
	metadata.DestinationPort = strconv.Itoa(int(conversation.Destination.Port()))
	if conversation.SniffedDomain != "" {
		metadata.Host = conversation.SniffedDomain
	}

	// Clash lists the proxy that carried it first and the group last; core lists
	// the group first.
	chains := make([]string, len(conversation.OutboundTags))
	for index, tag := range conversation.OutboundTags {
		chains[len(chains)-1-index] = tag
	}
	return connectionBody{
		ID:       conversation.ID,
		Metadata: metadata,
		Upload:   conversation.UplinkBytes,
		Download: conversation.DownlinkBytes,
		Start:    conversation.StartedAt,
		Chains:   chains,
		Rule:     conversation.RuleLocation,
	}
}

// The protocol names Clash shows for the inbounds it knows; any other is shown as
// the registry names it.
func inboundType(protocolName string) string {
	switch protocolName {
	case "socks5":
		return "Socks5"
	case "http":
		return "HTTP"
	case "mixed":
		return "Mixed"
	case "trojan":
		return "Trojan"
	default:
		return protocolName
	}
}

func (h handler) closeAll(w http.ResponseWriter, r *http.Request) {
	if err := h.provider.CloseAllLiveConversations(); err != nil {
		web.WriteProviderError(w, err)
		return
	}
	web.WriteNoContent(w)
}

func (h handler) closeOne(w http.ResponseWriter, r *http.Request) {
	if err := h.provider.CloseLiveConversation(r.PathValue("id")); err != nil {
		web.WriteProviderError(w, err)
		return
	}
	web.WriteNoContent(w)
}
