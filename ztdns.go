package ztdns

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

var log = clog.NewWithPlugin("ztdns")

// ZTDns is a CoreDNS plugin that serves DNS records from ZeroTier Central.
type ZTDns struct {
	Next   plugin.Handler
	Zones  []string
	client *Client
}

// Name implements the plugin.Handler interface.
func (z *ZTDns) Name() string { return "ztdns" }

// ServeDNS implements the plugin.Handler interface.
func (z *ZTDns) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Only handle A and AAAA queries
	qtype := state.QType()
	qtypeStr := dns.TypeToString[qtype]
	qname := state.Name()
	zone := plugin.Zones(z.Zones).Matches(qname)

	log.Debugf("ServeDNS: query=%s type=%s zone=%s", qname, qtypeStr, zone)

	if qtype != dns.TypeA && qtype != dns.TypeAAAA {
		log.Debugf("ServeDNS: skipping non-A/AAAA query type %s", qtypeStr)
		return plugin.NextOrFailure(z.Name(), z.Next, ctx, w, r)
	}

	// Extract hostname from the query (remove zone suffix)
	hostname := extractHostname(qname, zone)
	log.Debugf("ServeDNS: extracted hostname=%q from qname=%q zone=%q", hostname, qname, zone)

	if hostname == "" {
		log.Debugf("ServeDNS: empty hostname, passing to next plugin")
		return plugin.NextOrFailure(z.Name(), z.Next, ctx, w, r)
	}

	// Look up the member
	member, err := z.client.Lookup(ctx, hostname)
	if err != nil {
		log.Errorf("lookup failed: %v", err)
		return plugin.NextOrFailure(z.Name(), z.Next, ctx, w, r)
	}

	if member == nil {
		log.Debugf("ServeDNS: no member found for %q, passing to next plugin", hostname)
		return plugin.NextOrFailure(z.Name(), z.Next, ctx, w, r)
	}

	// Build response
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	switch qtype {
	case dns.TypeA:
		if member.IPv4 != nil {
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   qname,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: member.IPv4,
			}
			m.Answer = append(m.Answer, rr)
			log.Debugf("ServeDNS: responding with A record: %s -> %v", qname, member.IPv4)
		} else {
			log.Debugf("ServeDNS: member %q has no IPv4 address", hostname)
		}
	case dns.TypeAAAA:
		if member.IPv6 != nil {
			rr := &dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   qname,
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				AAAA: member.IPv6,
			}
			m.Answer = append(m.Answer, rr)
			log.Debugf("ServeDNS: responding with AAAA record: %s -> %v", qname, member.IPv6)
		} else {
			log.Debugf("ServeDNS: member %q has no IPv6 address", hostname)
		}
	}

	log.Debugf("ServeDNS: sending response with %d answers", len(m.Answer))
	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

// extractHostname extracts the hostname part from a FQDN given the zone.
// e.g., "myhost.zt.example.com." with zone "zt.example.com." returns "myhost"
func extractHostname(qname, zone string) string {
	log.Debugf("Host: %s, Zone: %s", qname, zone)
	// Ensure both have trailing dots for comparison
	if !strings.HasSuffix(qname, ".") {
		qname += "."
	}
	if !strings.HasSuffix(zone, ".") {
		zone += "."
	}

	// Check if qname is in the zone
	if !strings.HasSuffix(qname, zone) {
		return ""
	}

	// Remove the zone suffix
	hostname := strings.TrimSuffix(qname, zone)
	hostname = strings.TrimSuffix(hostname, ".")

	return hostname
}
