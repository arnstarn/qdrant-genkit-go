package client

import (
	"errors"
	"strings"
	"testing"

	qclient "github.com/qdrant/go-client/qdrant"
)

// TestNew_AppliesDefaults verifies that a zero-value Params struct produces a
// client whose underlying gRPC connection targets the documented defaults
// (localhost:6334).
func TestNew_AppliesDefaults(t *testing.T) {
	c, err := New(Params{})
	if err != nil {
		t.Fatalf("New(empty): %v", err)
	}
	if c == nil {
		t.Fatal("New(empty) returned nil client")
	}
	defer c.Close()

	target := c.GetGrpcClient().Conn().Target()
	if !strings.Contains(target, "localhost") {
		t.Errorf("target = %q, want host=localhost", target)
	}
	if !strings.Contains(target, "6334") {
		t.Errorf("target = %q, want default gRPC port 6334", target)
	}
}

// TestNew_HostOnly verifies that the documented default port (6334) is applied
// when only Host is provided.
func TestNew_HostOnly(t *testing.T) {
	c, err := New(Params{Host: "example.invalid"})
	if err != nil {
		t.Fatalf("New(host-only): %v", err)
	}
	defer c.Close()

	target := c.GetGrpcClient().Conn().Target()
	if !strings.Contains(target, "example.invalid") {
		t.Errorf("target = %q, want host=example.invalid", target)
	}
	if !strings.Contains(target, "6334") {
		t.Errorf("target = %q, want default gRPC port 6334", target)
	}
}

// TestNew_PortOnly verifies that a Host-less Params struct still gets the
// default host applied while preserving the user-supplied port.
func TestNew_PortOnly(t *testing.T) {
	c, err := New(Params{Port: 9999})
	if err != nil {
		t.Fatalf("New(port-only): %v", err)
	}
	defer c.Close()

	target := c.GetGrpcClient().Conn().Target()
	if !strings.Contains(target, "localhost") {
		t.Errorf("target = %q, want default host localhost", target)
	}
	if !strings.Contains(target, "9999") {
		t.Errorf("target = %q, want port 9999", target)
	}
}

// TestNew_FullParams verifies that an explicit host, port, API key, and TLS
// flag are all accepted. We can't easily peek at the API key (it's set up as a
// gRPC interceptor) but we can confirm host:port end up on the dial target and
// that no error is returned.
func TestNew_FullParams(t *testing.T) {
	c, err := New(Params{
		Host:   "qdrant.example.test",
		Port:   1234,
		APIKey: "fake-api-key",
		UseTLS: true,
	})
	if err != nil {
		t.Fatalf("New(full): %v", err)
	}
	defer c.Close()

	target := c.GetGrpcClient().Conn().Target()
	if !strings.Contains(target, "qdrant.example.test") {
		t.Errorf("target = %q, want host=qdrant.example.test", target)
	}
	if !strings.Contains(target, "1234") {
		t.Errorf("target = %q, want port 1234", target)
	}
}

// TestNew_TLSToggle exercises the UseTLS=true branch independently to make
// sure the TLS-credentials path in the underlying client constructor doesn't
// blow up.
func TestNew_TLSToggle(t *testing.T) {
	for _, tls := range []bool{false, true} {
		tls := tls
		t.Run("", func(t *testing.T) {
			c, err := New(Params{Host: "tls.example.test", Port: 4443, UseTLS: tls})
			if err != nil {
				t.Fatalf("New(UseTLS=%v): %v", tls, err)
			}
			c.Close()
		})
	}
}

// TestNew_APIKeyOnly confirms that providing only an API key still produces a
// usable client with the default endpoint (the underlying client adds the API
// key as an outbound metadata interceptor; we just want to make sure the
// constructor accepts it cleanly).
func TestNew_APIKeyOnly(t *testing.T) {
	c, err := New(Params{APIKey: "secret-token"})
	if err != nil {
		t.Fatalf("New(api-key-only): %v", err)
	}
	defer c.Close()

	target := c.GetGrpcClient().Conn().Target()
	if !strings.Contains(target, "localhost") || !strings.Contains(target, "6334") {
		t.Errorf("target = %q, want defaults applied", target)
	}
}

// TestNew_DistinctClients verifies that consecutive calls return distinct
// client instances. The plugin layer is responsible for de-duplication via
// Qdrant.clientFor; client.New itself should not memoize.
func TestNew_DistinctClients(t *testing.T) {
	c1, err := New(Params{Host: "a.example.test", Port: 6334})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c1.Close()
	c2, err := New(Params{Host: "a.example.test", Port: 6334})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c2.Close()

	if c1 == c2 {
		t.Errorf("expected distinct client instances on each New() call")
	}
}

// TestNew_ConstructorErrorIsWrapped covers the error-wrap branch in New: when
// the underlying constructor fails, New must wrap with the host:port context
// and propagate the original via errors.Is.
func TestNew_ConstructorErrorIsWrapped(t *testing.T) {
	wantErr := errors.New("simulated connect failure")

	prev := newClient
	newClient = func(*qclient.Config) (*qclient.Client, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { newClient = prev })

	_, err := New(Params{Host: "boom.example.test", Port: 6334})
	if err == nil {
		t.Fatal("expected error from New when underlying constructor fails")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
	if !strings.Contains(err.Error(), "boom.example.test:6334") {
		t.Errorf("error %q should mention host:port", err)
	}
}
