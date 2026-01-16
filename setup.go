// Package ztdns implements a CoreDNS plugin that serves DNS records for
// ZeroTier network members by querying the ZeroTier Central API.
//
// # Domain Configuration
//
// This plugin reads member data from ZeroTier Central and does not perform
// any DNS domain configuration itself. The DNS zone configured in your
// Corefile must match the DNS domain configured in ZeroTier Central for
// your network. For example, if your Corefile serves "zt.example.com",
// your ZeroTier network's DNS settings in Central should also use
// "zt.example.com" as the search domain.
//
// # Example Corefile
//
//	zt.example.com {
//	    ztdns {
//	        token {$ZT_API_TOKEN}
//	        network abc123def456
//	        refresh 60s
//	    }
//	}
package ztdns

import (
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	plugin.Register("ztdns", setup)
}

func setup(c *caddy.Controller) error {
	z, err := parseConfig(c)
	if err != nil {
		return plugin.Error("ztdns", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		z.Next = next
		return z
	})

	return nil
}

func parseConfig(c *caddy.Controller) (*ZTDns, error) {
	var token string
	var networkID string
	var zones []string
	refresh := 60 * time.Second

	for c.Next() {
		zones = plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys)
		// ztdns block
		for c.NextBlock() {
			switch c.Val() {
			case "token":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				token = c.Val()
			case "network":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				networkID = c.Val()
			case "refresh":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				dur, err := time.ParseDuration(c.Val())
				if err != nil {
					return nil, c.Errf("invalid refresh duration: %v", err)
				}
				refresh = dur
			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	if token == "" {
		return nil, c.Err("token is required")
	}
	if networkID == "" {
		return nil, c.Err("network is required")
	}

	client := NewClient(token, networkID, refresh)

	return &ZTDns{
		Zones:  zones,
		client: client,
	}, nil
}
