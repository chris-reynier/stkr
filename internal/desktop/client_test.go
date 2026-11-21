package desktop

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestClientPassesRealGuards runs the client against the real server, whose
// Host and Origin guards reject anything but loopback on its own port.
func TestClientPassesRealGuards(t *testing.T) {
	h, _ := testHost(t, "")
	hd, err := h.start(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hd.Shutdown(context.Background()) }()
	c := NewClient(hd.Port())
	ctx := context.Background()
	if err := c.Probe(ctx); err != nil {
		t.Fatalf("probe: %v", err)
	}
	st, err := c.Status(ctx)
	if err != nil || st.HostedBy != "desktop" || len(st.Contexts) == 0 {
		t.Fatalf("status %+v %v", st, err)
	}
	// POST carries an Origin; the Origin guard must accept it.
	res, err := c.Post(ctx, "/api/switch/other")
	if err != nil || res["created"] != true {
		t.Fatalf("switch %v %v", res, err)
	}
	_, err = c.Post(ctx, "/api/switch/nope")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Code != "not_found" || ae.Error() == "" {
		t.Fatalf("err = %v", err)
	}
	status, body, err := c.do(ctx, "GET", "/healthz")
	if err != nil || status != 200 || len(body) == 0 {
		t.Fatalf("healthz %d %v", status, err)
	}
}

func TestClientWrongHostRejected(t *testing.T) {
	h, _ := testHost(t, "")
	hd, err := h.start(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hd.Shutdown(context.Background()) }()
	// Build a client for a different port but dial the real one: the Host
	// header then names the wrong port.
	c := NewClient(hd.Port())
	c.port = hd.Port() + 1
	tr := &dialTo{addr: "127.0.0.1:" + strconv.Itoa(hd.Port())}
	c.http.Transport = tr.transport()
	_, err = c.Status(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 421 || ae.Code != "invalid_host" {
		t.Fatalf("err = %v", err)
	}
	if err := c.Probe(context.Background()); !errors.Is(err, errNotSTKR) {
		t.Fatalf("probe = %v", err)
	}
}

func TestAPIErrorWithoutMessage(t *testing.T) {
	if e := (&APIError{Status: 500}); e.Error() != "server answered 500" {
		t.Fatal(e.Error())
	}
	if err := apiError(400, []byte("not json")); err.Error() != "server answered 400" {
		t.Fatal(err)
	}
}

// dialTo is a transport that always dials addr, whatever the URL says.
type dialTo struct{ addr string }

func (d *dialTo) transport() *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dl net.Dialer
		return dl.DialContext(ctx, network, d.addr)
	}}
}
