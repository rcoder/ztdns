package ztdns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIURL = "https://api.zerotier.com/api/v1"
)

// Member represents a ZeroTier network member.
type Member struct {
	NodeID      string
	Name        string
	Description string
	IPv4        net.IP // IPv4 address (nil if none assigned)
	IPv6        net.IP // IPv6 address (nil if none assigned)
	Online      bool
	Authorized  bool
}

// Client is a ZeroTier Central API client with caching.
type Client struct {
	apiURL    string
	token     string
	networkID string
	client    *http.Client

	mu       sync.RWMutex
	members  map[string]*Member // hostname -> member
	lastSync time.Time
	ttl      time.Duration
}

// NewClient creates a new ZeroTier API client.
func NewClient(token, networkID string, ttl time.Duration) *Client {
	return &Client{
		apiURL:    defaultAPIURL,
		token:     token,
		networkID: networkID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		members: make(map[string]*Member),
		ttl:     ttl,
	}
}

// apiMember represents the JSON structure from ZeroTier API.
type apiMember struct {
	NodeID      string `json:"nodeId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      struct {
		Authorized  bool     `json:"authorized"`
		IPAssignments []string `json:"ipAssignments"`
	} `json:"config"`
	Online bool `json:"online"`
}

// Lookup returns a member by hostname.
func (c *Client) Lookup(ctx context.Context, hostname string) (*Member, error) {
	log.Debugf("Lookup: hostname=%q", hostname)

	if err := c.refreshIfNeeded(ctx); err != nil {
		log.Errorf("Error refreshing member list: %s", err)
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	lookupKey := strings.ToLower(hostname)
	member, ok := c.members[lookupKey]
	if !ok {
		log.Debugf("Lookup: %q not found in cache (have %d members: %v)",
			lookupKey, len(c.members), c.memberKeys())
		return nil, nil
	}
	log.Debugf("Lookup: found %q -> nodeId=%s IPv4=%v IPv6=%v",
		lookupKey, member.NodeID, member.IPv4, member.IPv6)
	return member, nil
}

// memberKeys returns the list of cached member hostnames (for debug logging).
func (c *Client) memberKeys() []string {
	keys := make([]string, 0, len(c.members))
	for k := range c.members {
		keys = append(keys, k)
	}
	return keys
}

// refreshIfNeeded fetches members from the API if the cache is stale.
func (c *Client) refreshIfNeeded(ctx context.Context) error {
	c.mu.RLock()
	needsRefresh := c.lastSync.IsZero() || time.Since(c.lastSync) > c.ttl
	c.mu.RUnlock()

	if !needsRefresh {
		return nil
	}
	log.Infof("Refreshing member list...")
	return c.refresh(ctx)
}

// refresh fetches all members from the ZeroTier API.
func (c *Client) refresh(ctx context.Context) error {
	url := fmt.Sprintf("%s/network/%s/member", c.apiURL, c.networkID)
	log.Debugf("API request: GET %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		log.Debugf("API request failed: %v", err)
		return fmt.Errorf("fetching members: %w", err)
	}
	defer resp.Body.Close()

	log.Debugf("API response status: %d %s", resp.StatusCode, resp.Status)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Debugf("API error response body: %s", string(body))
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Read body for logging and decoding
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	log.Debugf("API response body (%d bytes): %s", len(body), string(body))

	var apiMembers []apiMember
	if err := json.Unmarshal(body, &apiMembers); err != nil {
		log.Debugf("JSON decode error: %v", err)
		return fmt.Errorf("decoding response: %w", err)
	}

	log.Debugf("API returned %d members from network %s", len(apiMembers), c.networkID)

	members := make(map[string]*Member)
	var skippedUnauthorized, skippedNoIP, skippedNoHostname int
	for _, am := range apiMembers {
		log.Debugf("Processing member: nodeId=%s name=%q authorized=%v online=%v ips=%v",
			am.NodeID, am.Name, am.Config.Authorized, am.Online, am.Config.IPAssignments)

		member := c.convertMember(am)
		if member == nil {
			if !am.Config.Authorized {
				skippedUnauthorized++
			} else {
				skippedNoIP++
			}
			continue
		}

		hostname := sanitizeHostname(member.Name)
		if hostname == "" {
			log.Debugf("  -> skipped: empty hostname after sanitization (name=%q)", member.Name)
			skippedNoHostname++
			continue
		}

		log.Debugf("  -> added as %q: IPv4=%v IPv6=%v", hostname, member.IPv4, member.IPv6)
		members[hostname] = member
	}

	log.Debugf("Member processing complete: %d usable, %d unauthorized, %d no-IP, %d no-hostname",
		len(members), skippedUnauthorized, skippedNoIP, skippedNoHostname)

	c.mu.Lock()
	c.members = members
	c.lastSync = time.Now()
	c.mu.Unlock()

	log.Infof("Refreshed member list: %d members cached", len(members))

	return nil
}

// convertMember converts an API member to our Member type.
func (c *Client) convertMember(am apiMember) *Member {
	// Skip unauthorized members
	if !am.Config.Authorized {
		log.Debugf("  -> skipped: not authorized")
		return nil
	}

	// Parse all assigned IPs, keeping first IPv4 and first IPv6
	var ipv4, ipv6 net.IP
	for _, ipStr := range am.Config.IPAssignments {
		parsed := net.ParseIP(ipStr)
		if parsed == nil {
			log.Debugf("  -> failed to parse IP: %q", ipStr)
			continue
		}
		if parsed.To4() != nil {
			if ipv4 == nil {
				ipv4 = parsed.To4()
			}
		} else {
			if ipv6 == nil {
				ipv6 = parsed
			}
		}
	}

	// Skip members with no IP addresses
	if ipv4 == nil && ipv6 == nil {
		log.Debugf("  -> skipped: no IP addresses assigned")
		return nil
	}

	name := am.Name
	if name == "" {
		name = am.NodeID
	}

	return &Member{
		NodeID:      am.NodeID,
		Name:        name,
		Description: am.Description,
		IPv4:        ipv4,
		IPv6:        ipv6,
		Online:      am.Online,
		Authorized:  am.Config.Authorized,
	}
}

// sanitizeHostname converts a name to a valid DNS hostname.
func sanitizeHostname(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase
	hostname := strings.ToLower(name)

	// Replace spaces and underscores with hyphens
	hostname = strings.ReplaceAll(hostname, " ", "-")
	hostname = strings.ReplaceAll(hostname, "_", "-")

	// Remove any characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	hostname = reg.ReplaceAllString(hostname, "")

	// Remove leading/trailing hyphens
	hostname = strings.Trim(hostname, "-")

	// Collapse multiple hyphens
	for strings.Contains(hostname, "--") {
		hostname = strings.ReplaceAll(hostname, "--", "-")
	}

	// DNS labels must be 63 chars or less
	if len(hostname) > 63 {
		hostname = hostname[:63]
		hostname = strings.TrimRight(hostname, "-")
	}

	return hostname
}

