package ztdns

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"myhost", "myhost"},
		{"My Host", "my-host"},
		{"my_host", "my-host"},
		{"My_Host Name", "my-host-name"},
		{"UPPERCASE", "uppercase"},
		{"host!@#$%name", "hostname"},
		{"--leading-trailing--", "leading-trailing"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"", ""},
		{"a", "a"},
		{"123", "123"},
		{"host.name", "hostname"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeHostname(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeHostname(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestClientLookup(t *testing.T) {
	members := []apiMember{
		{
			NodeID: "abc123",
			Name:   "my-server",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    true,
				IPAssignments: []string{"10.147.17.1"},
			},
			Online: true,
		},
		{
			NodeID: "def456",
			Name:   "My Desktop",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    true,
				IPAssignments: []string{"10.147.17.2"},
			},
			Online: false,
		},
		{
			NodeID: "dualstack",
			Name:   "dual-stack-host",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    true,
				IPAssignments: []string{"10.147.17.3", "fd80:56c2:e21c:123::1"},
			},
			Online: true,
		},
		{
			NodeID: "ipv6only",
			Name:   "ipv6-only-host",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    true,
				IPAssignments: []string{"fd80:56c2:e21c:456::1"},
			},
			Online: true,
		},
		{
			NodeID: "unauthorized",
			Name:   "unauthorized-host",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    false,
				IPAssignments: []string{"10.147.17.99"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(members)
	}))
	defer server.Close()

	client := NewClient("test-token", "network123", time.Minute)
	client.apiURL = server.URL

	ctx := context.Background()

	// Test lookup of existing host (IPv4 only)
	member, err := client.Lookup(ctx, "my-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member == nil {
		t.Fatal("expected member, got nil")
	}
	if member.Name != "my-server" {
		t.Errorf("expected name 'my-server', got %q", member.Name)
	}
	if !member.IPv4.Equal(net.ParseIP("10.147.17.1")) {
		t.Errorf("expected IPv4 10.147.17.1, got %v", member.IPv4)
	}
	if member.IPv6 != nil {
		t.Errorf("expected no IPv6, got %v", member.IPv6)
	}

	// Test lookup with sanitized name
	member, err = client.Lookup(ctx, "my-desktop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member == nil {
		t.Fatal("expected member for 'my-desktop', got nil")
	}

	// Test dual-stack host
	member, err = client.Lookup(ctx, "dual-stack-host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member == nil {
		t.Fatal("expected member for 'dual-stack-host', got nil")
	}
	if !member.IPv4.Equal(net.ParseIP("10.147.17.3")) {
		t.Errorf("expected IPv4 10.147.17.3, got %v", member.IPv4)
	}
	if !member.IPv6.Equal(net.ParseIP("fd80:56c2:e21c:123::1")) {
		t.Errorf("expected IPv6 fd80:56c2:e21c:123::1, got %v", member.IPv6)
	}

	// Test IPv6-only host
	member, err = client.Lookup(ctx, "ipv6-only-host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member == nil {
		t.Fatal("expected member for 'ipv6-only-host', got nil")
	}
	if member.IPv4 != nil {
		t.Errorf("expected no IPv4, got %v", member.IPv4)
	}
	if !member.IPv6.Equal(net.ParseIP("fd80:56c2:e21c:456::1")) {
		t.Errorf("expected IPv6 fd80:56c2:e21c:456::1, got %v", member.IPv6)
	}

	// Test lookup of non-existent host
	member, err = client.Lookup(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member != nil {
		t.Errorf("expected nil for nonexistent host, got %v", member)
	}

	// Test that unauthorized hosts are not returned
	member, err = client.Lookup(ctx, "unauthorized-host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member != nil {
		t.Errorf("expected nil for unauthorized host, got %v", member)
	}
}

func TestClientCaching(t *testing.T) {
	callCount := 0
	members := []apiMember{
		{
			NodeID: "abc123",
			Name:   "test-host",
			Config: struct {
				Authorized    bool     `json:"authorized"`
				IPAssignments []string `json:"ipAssignments"`
			}{
				Authorized:    true,
				IPAssignments: []string{"10.147.17.1"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(members)
	}))
	defer server.Close()

	client := NewClient("test-token", "network123", time.Hour)
	client.apiURL = server.URL

	ctx := context.Background()

	// First lookup should trigger API call
	_, err := client.Lookup(ctx, "test-host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second lookup should use cache
	_, err = client.Lookup(ctx, "test-host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 API call (cached), got %d", callCount)
	}
}
