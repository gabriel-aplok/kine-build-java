package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBridgeCall(t *testing.T) {
	bridge, server := newTestBridge(t)
	go func() {
		reader := bufio.NewReader(server)
		request := readRequest(t, reader)
		if request.Method != "greet" {
			t.Errorf("method = %q, want greet", request.Method)
		}

		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Errorf("decode params: %v", err)
		}
		if params.Name != "kine" {
			t.Errorf("name = %q, want kine", params.Name)
		}
		writeResponse(t, server, request.ID, `{"message":"hello kine"}`)
	}()

	var result struct {
		Message string `json:"message"`
	}
	if err := bridge.Call(context.Background(), "greet", map[string]string{"name": "kine"}, &result); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Message != "hello kine" {
		t.Errorf("message = %q, want hello kine", result.Message)
	}
}

func TestBridgeConcurrentCalls(t *testing.T) {
	bridge, server := newTestBridge(t)
	go func() {
		reader := bufio.NewReader(server)
		for i := 0; i < 5; i++ {
			request := readRequest(t, reader)
			var params struct {
				N int `json:"n"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Errorf("decode params: %v", err)
				return
			}
			writeResponse(t, server, request.ID, `{"n":`+strconv.Itoa(params.N)+`}`)
		}
	}()

	var workers sync.WaitGroup
	for i := 0; i < 5; i++ {
		n := i
		workers.Add(1)
		go func() {
			defer workers.Done()
			var result struct {
				N int `json:"n"`
			}
			if err := bridge.Call(context.Background(), "echo", map[string]int{"n": n}, &result); err != nil {
				t.Errorf("Call(%d) error = %v", n, err)
				return
			}
			if result.N != n {
				t.Errorf("Call(%d) result = %d", n, result.N)
			}
		}()
	}
	workers.Wait()
}

func TestBridgeErrorResponse(t *testing.T) {
	bridge, server := newTestBridge(t)
	go func() {
		reader := bufio.NewReader(server)
		request := readRequest(t, reader)
		_, _ = server.Write([]byte(`{"jsonrpc":"2.0","id":"` + request.ID + `","error":{"code":42,"message":"boom"}}` + "\n"))
	}()

	err := bridge.Call(context.Background(), "fail", struct{}{}, nil)
	if err == nil {
		t.Fatal("Call() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "42") {
		t.Errorf("error %q does not carry the rpc error", err)
	}
}

func TestBridgeNotification(t *testing.T) {
	notifications := make(chan string, 1)
	client, server := net.Pipe()
	bridge := NewBridge(client, client, func(method string, params json.RawMessage) {
		var value struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &value); err != nil {
			return
		}
		notifications <- method + ":" + value.Message
	})
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})

	go func() {
		_, _ = server.Write([]byte(`{"jsonrpc":"2.0","method":"log","params":{"message":"hi"}}` + "\n"))
	}()

	select {
	case got := <-notifications:
		if got != "log:hi" {
			t.Errorf("notification = %q, want log:hi", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not delivered")
	}
}

func TestBridgeMalformedMessage(t *testing.T) {
	bridge, server := newTestBridge(t)
	go func() {
		reader := bufio.NewReader(server)
		readRequest(t, reader)
		_, _ = server.Write([]byte("not json\n"))
	}()

	err := bridge.Call(context.Background(), "call", struct{}{}, nil)
	if err == nil {
		t.Fatal("Call() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "decode host message") {
		t.Errorf("error %q does not mention the malformed message", err)
	}
}

func TestBridgeVersionMismatch(t *testing.T) {
	bridge, server := newTestBridge(t)
	go func() {
		reader := bufio.NewReader(server)
		request := readRequest(t, reader)
		_, _ = server.Write([]byte(`{"jsonrpc":"1.0","id":"` + request.ID + `","result":{}}` + "\n"))
	}()

	err := bridge.Call(context.Background(), "call", struct{}{}, nil)
	if err == nil {
		t.Fatal("Call() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "jsonrpc") {
		t.Errorf("error %q does not mention the version", err)
	}
}

func TestBridgeCloseFailsPending(t *testing.T) {
	bridge, server := newTestBridge(t)
	read := make(chan struct{})
	go func() {
		reader := bufio.NewReader(server)
		readRequest(t, reader)
		close(read)
	}()

	done := make(chan error, 1)
	go func() {
		done <- bridge.Call(context.Background(), "wait", struct{}{}, nil)
	}()
	<-read
	if err := bridge.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Call() error = nil after close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after close")
	}
}

func TestBridgeContextCancelKeepsUsable(t *testing.T) {
	bridge, server := newTestBridge(t)
	firstRead := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(func() {
		releaseFirst()
		<-handlerDone
	})

	go func() {
		defer close(handlerDone)
		reader := bufio.NewReader(server)
		first := readRequest(t, reader)
		close(firstRead)
		<-release
		writeResponse(t, server, first.ID, `{}`)

		second := readRequest(t, reader)
		writeResponse(t, server, second.ID, `{"ok":true}`)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := bridge.Call(ctx, "slow", struct{}{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want DeadlineExceeded", err)
	}
	<-firstRead
	releaseFirst()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := bridge.Call(context.Background(), "fast", struct{}{}, &result); err != nil {
		t.Fatalf("second Call() error = %v", err)
	}
	if !result.OK {
		t.Error("second Call result = false")
	}
}

func newTestBridge(t *testing.T) (*Bridge, net.Conn) {
	t.Helper()

	client, server := net.Pipe()
	bridge := NewBridge(client, client, nil)
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	return bridge, server
}

func readRequest(t *testing.T, reader *bufio.Reader) rpcMessage {
	t.Helper()

	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Errorf("read request: %v", err)
		return rpcMessage{}
	}
	var message rpcMessage
	if err := json.Unmarshal(line, &message); err != nil {
		t.Errorf("decode request: %v", err)
	}
	return message
}

func writeResponse(t *testing.T, conn net.Conn, id, result string) {
	t.Helper()

	payload := `{"jsonrpc":"2.0","id":"` + id + `","result":` + result + "}\n"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Errorf("write response: %v", err)
	}
}
