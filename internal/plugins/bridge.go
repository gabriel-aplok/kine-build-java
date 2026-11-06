package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

const (
	rpcVersion    = "2.0"
	maxRPCMessage = 16 << 20
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// Bridge is a JSON-RPC client over newline-delimited messages.
type Bridge struct {
	reader io.ReadCloser
	writer io.WriteCloser
	notify func(method string, params json.RawMessage)

	writeMu sync.Mutex

	mu        sync.Mutex
	nextID    int64
	pending   map[string]chan rpcResult
	cancelled map[string]struct{}
	closed    bool
	err       error

	closeOnce sync.Once
}

// NewBridge starts reading messages from reader and writes to writer.
func NewBridge(reader io.ReadCloser, writer io.WriteCloser, notify func(string, json.RawMessage)) *Bridge {
	bridge := &Bridge{
		reader:  reader,
		writer:  writer,
		notify:  notify,
		pending: make(map[string]chan rpcResult),
	}
	go bridge.readLoop()
	return bridge
}

// Call sends one request and decodes its result.
func (b *Bridge) Call(ctx context.Context, method string, params, result any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s params: %w", method, err)
	}

	b.mu.Lock()
	if b.closed {
		err := b.err
		b.mu.Unlock()
		return err
	}
	b.nextID++
	id := strconv.FormatInt(b.nextID, 10)
	channel := make(chan rpcResult, 1)
	b.pending[id] = channel
	b.mu.Unlock()

	message := rpcMessage{JSONRPC: rpcVersion, ID: id, Method: method, Params: payload}
	data, err := json.Marshal(message)
	if err != nil {
		b.removePending(id)
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	data = append(data, '\n')

	b.writeMu.Lock()
	_, err = b.writer.Write(data)
	b.writeMu.Unlock()
	if err != nil {
		b.removePending(id)
		b.closeWithError(fmt.Errorf("write %s request: %w", method, err))
		return fmt.Errorf("write %s request: %w", method, err)
	}

	select {
	case response := <-channel:
		if response.err != nil {
			return response.err
		}
		if result != nil && len(response.result) > 0 {
			if err := json.Unmarshal(response.result, result); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		b.cancelPending(id)
		return ctx.Err()
	}
}

// Close stops the bridge and fails pending calls.
func (b *Bridge) Close() error {
	var closeErr error
	b.closeOnce.Do(func() {
		b.closeWithError(errors.New("bridge is closed"))
		if err := b.writer.Close(); err != nil {
			closeErr = err
		}
		if err := b.reader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	})
	return closeErr
}

func (b *Bridge) readLoop() {
	scanner := bufio.NewScanner(b.reader)
	scanner.Buffer(make([]byte, 0, 64<<10), maxRPCMessage)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			b.closeWithError(fmt.Errorf("decode host message: %w", err))
			return
		}
		if message.JSONRPC != rpcVersion {
			b.closeWithError(fmt.Errorf("host message: jsonrpc %q, want %q", message.JSONRPC, rpcVersion))
			return
		}
		if message.ID == "" {
			if b.notify != nil && message.Method != "" {
				b.notify(message.Method, message.Params)
			}
			continue
		}
		b.deliver(message)
	}

	if err := scanner.Err(); err != nil {
		b.closeWithError(fmt.Errorf("read host message: %w", err))
		return
	}
	b.closeWithError(errors.New("host closed the bridge"))
}

func (b *Bridge) deliver(message rpcMessage) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	channel, ok := b.pending[message.ID]
	if ok {
		delete(b.pending, message.ID)
	}
	_, cancelled := b.cancelled[message.ID]
	if cancelled {
		delete(b.cancelled, message.ID)
	}
	b.mu.Unlock()

	if !ok {
		if cancelled {
			return
		}
		b.closeWithError(fmt.Errorf("unexpected response id %q", message.ID))
		return
	}
	if message.Error != nil {
		channel <- rpcResult{err: message.Error}
		return
	}
	channel <- rpcResult{result: message.Result}
}

func (b *Bridge) removePending(id string) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

func (b *Bridge) cancelPending(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.pending[id]; !ok {
		return
	}
	delete(b.pending, id)
	if b.cancelled == nil {
		b.cancelled = make(map[string]struct{})
	}
	b.cancelled[id] = struct{}{}
}

func (b *Bridge) closeWithError(err error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.err = err
	pending := b.pending
	b.pending = nil
	b.cancelled = nil
	b.mu.Unlock()

	for _, channel := range pending {
		channel <- rpcResult{err: err}
	}
}
