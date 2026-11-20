package mcpserver

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// drainTimeout bounds how long the server waits for in-flight requests to be
// answered after stdin has ended.
const drainTimeout = 30 * time.Second

// drainTransport wraps a transport so that when the input ends (EOF) the
// connection stays open until every request already read has been answered.
// Without it, piping requests into `stkr mcp` and closing stdin would drop
// the responses.
type drainTransport struct{ inner mcp.Transport }

func (t drainTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	d := &drainConn{Connection: c, pending: map[jsonrpc.ID]struct{}{}}
	d.cond = sync.NewCond(&d.mu)
	return d, nil
}

type drainConn struct {
	mcp.Connection
	mu      sync.Mutex
	cond    *sync.Cond
	pending map[jsonrpc.ID]struct{}
}

func (d *drainConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := d.Connection.Read(ctx)
	if err != nil {
		if err == io.EOF {
			d.waitIdle(ctx)
		}
		return nil, err
	}
	if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
		d.mu.Lock()
		d.pending[req.ID] = struct{}{}
		d.mu.Unlock()
	}
	return msg, nil
}

func (d *drainConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := d.Connection.Write(ctx, msg)
	if resp, ok := msg.(*jsonrpc.Response); ok {
		d.mu.Lock()
		delete(d.pending, resp.ID)
		d.mu.Unlock()
		d.cond.Broadcast()
	}
	return err
}

func (d *drainConn) waitIdle(ctx context.Context) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-time.After(drainTimeout):
		case <-ctx.Done():
		case <-stop:
			return
		}
		d.mu.Lock()
		clear(d.pending)
		d.mu.Unlock()
		d.cond.Broadcast()
	}()
	d.mu.Lock()
	for len(d.pending) > 0 {
		d.cond.Wait()
	}
	d.mu.Unlock()
}
