package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// EngineError carries the structured error code from the engine (04 §2.4).
type EngineError struct {
	Code    string
	Message string
}

func (e *EngineError) Error() string { return fmt.Sprintf("[%s] %s", e.Code, e.Message) }

type pending struct {
	ch chan rpcResponse
}

type rpcResponse struct {
	result json.RawMessage
	err    *rpcError
}

// rpcClient speaks JSON-RPC 2.0 with LSP-style Content-Length framing over the
// sidecar's stdin/stdout.
type rpcClient struct {
	w      io.Writer
	r      *bufio.Reader
	writeM sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]*pending
	closed  bool

	onNotify func(Notification)
}

func newRPCClient(w io.Writer, r io.Reader, onNotify func(Notification)) *rpcClient {
	c := &rpcClient{
		w:        w,
		r:        bufio.NewReaderSize(r, 1<<20),
		nextID:   1,
		pending:  make(map[int]*pending),
		onNotify: onNotify,
	}
	go c.readLoop()
	return c
}

// call issues a request and blocks until the matching response arrives, ctx is
// cancelled, or the pipe closes.
func (c *rpcClient) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return &EngineError{Code: "engine_down", Message: "engine not running"}
	}
	id := c.nextID
	c.nextID++
	p := &pending{ch: make(chan rpcResponse, 1)}
	c.pending[id] = p
	c.mu.Unlock()

	if err := c.writeMessage(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case resp := <-p.ch:
		if resp.err != nil {
			return &EngineError{Code: resp.err.Data.Code, Message: resp.err.Message}
		}
		if out != nil && len(resp.result) > 0 {
			return json.Unmarshal(resp.result, out)
		}
		return nil
	}
}

// notify sends a fire-and-forget notification (e.g. job.cancel).
func (c *rpcClient) notify(method string, params any) error {
	return c.writeMessage(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *rpcClient) writeMessage(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeM.Lock()
	defer c.writeM.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = c.w.Write(data)
	return err
}

func (c *rpcClient) readLoop() {
	for {
		msg, err := c.readMessage()
		if err != nil {
			c.shutdownPending(err)
			return
		}
		// A response has an id; a notification has a method and no id.
		var envelope struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}
		if envelope.ID == nil {
			if envelope.Method != "" && c.onNotify != nil {
				c.onNotify(Notification{Method: envelope.Method, Params: envelope.Params})
			}
			continue
		}
		c.mu.Lock()
		p := c.pending[*envelope.ID]
		delete(c.pending, *envelope.ID)
		c.mu.Unlock()
		if p != nil {
			p.ch <- rpcResponse{result: envelope.Result, err: envelope.Error}
		}
	}
}

func (c *rpcClient) readMessage() ([]byte, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			if strings.EqualFold(strings.TrimSpace(k), "content-length") {
				length, err = strconv.Atoi(strings.TrimSpace(v))
				if err != nil {
					return nil, fmt.Errorf("bad Content-Length: %w", err)
				}
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (c *rpcClient) shutdownPending(err error) {
	c.mu.Lock()
	c.closed = true
	pend := c.pending
	c.pending = make(map[int]*pending)
	c.mu.Unlock()
	for _, p := range pend {
		p.ch <- rpcResponse{err: &rpcError{Message: err.Error()}}
	}
}
