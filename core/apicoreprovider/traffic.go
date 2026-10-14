package apicoreprovider

import (
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

func (p *Provider) TotalTraffic() (spec.APITrafficTotal, error) {
	return p.totalOf(func(totals stats.TrafficTotals) stats.NetworkBytes { return totals.All })
}

func (p *Provider) TotalTCPTraffic() (spec.APITrafficTotal, error) {
	return p.totalOf(func(totals stats.TrafficTotals) stats.NetworkBytes { return totals.TCP })
}

func (p *Provider) TotalUDPTraffic() (spec.APITrafficTotal, error) {
	return p.totalOf(func(totals stats.TrafficTotals) stats.NetworkBytes { return totals.UDP })
}

func (p *Provider) totalOf(network func(stats.TrafficTotals) stats.NetworkBytes) (spec.APITrafficTotal, error) {
	if p.stats == nil {
		return spec.APITrafficTotal{}, spec.ErrAPIStatisticsDisabled
	}
	bytes := network(p.stats.TotalTraffic())
	return spec.APITrafficTotal{UplinkBytes: bytes.UplinkBytes, DownlinkBytes: bytes.DownlinkBytes}, nil
}
