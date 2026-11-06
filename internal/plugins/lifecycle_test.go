package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestHostLazyStart(t *testing.T) {
	bridge, server := newLifecycleBridge(t)
	closer := &fakeCloser{}
	host := testHost()
	starts := 0
	host.startFn = func(ctx context.Context) (*Bridge, io.Closer, error) {
		starts++
		return bridge, closer, nil
	}

	ctx := context.Background()
	if err := host.Call(ctx, "handshake", struct{}{}, nil); err != nil {
		t.Fatalf("handshake error = %v", err)
	}
	if err := host.Call(ctx, "tasks", struct{}{}, nil); err != nil {
		t.Fatalf("tasks error = %v", err)
	}
	if starts != 1 {
		t.Errorf("starts = %d, want 1", starts)
	}
	if got := server.methods(); !reflect.DeepEqual(got, []string{"handshake", "tasks"}) {
		t.Errorf("methods = %v", got)
	}
	if err := host.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
	if closer.closed != 1 {
		t.Errorf("closer closed %d times, want 1", closer.closed)
	}
	if got := server.methods(); !reflect.DeepEqual(got, []string{"handshake", "tasks", "shutdown"}) {
		t.Errorf("methods = %v", got)
	}
}

func TestHostStartError(t *testing.T) {
	host := testHost()
	starts := 0
	host.startFn = func(ctx context.Context) (*Bridge, io.Closer, error) {
		starts++
		return nil, nil, errors.New("no jdk")
	}

	for i := 0; i < 2; i++ {
		err := host.Call(context.Background(), "handshake", struct{}{}, nil)
		if err == nil {
			t.Fatal("Call() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "start plugin host") || !strings.Contains(err.Error(), "no jdk") {
			t.Errorf("error %q does not carry the start failure", err)
		}
	}
	if starts != 1 {
		t.Errorf("starts = %d, want 1", starts)
	}
	if err := host.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestHostClosed(t *testing.T) {
	host := testHost()
	starts := 0
	host.startFn = func(ctx context.Context) (*Bridge, io.Closer, error) {
		starts++
		return nil, nil, nil
	}

	if err := host.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	err := host.Call(context.Background(), "handshake", struct{}{}, nil)
	if err == nil {
		t.Fatal("Call() error = nil after close")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error %q does not mention the closed host", err)
	}
	if starts != 0 {
		t.Errorf("starts = %d, want 0", starts)
	}
}

func TestHostProviders(t *testing.T) {
	host := testHost()
	host.config.Plugins = []Plugin{
		{
			Jar: "/plugins/a.jar",
			Manifest: Manifest{
				API:       APIVersion,
				ID:        "a",
				Version:   "1.0",
				Main:      "a.APlugin",
				Providers: []string{"alpha", "beta"},
			},
		},
	}

	providers, err := host.Providers()
	if err != nil {
		t.Fatalf("Providers() error = %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(providers))
	}
	names := []string{providers[0].Name(), providers[1].Name()}
	if !reflect.DeepEqual(names, []string{"alpha", "beta"}) {
		t.Errorf("names = %v", names)
	}
	for _, provider := range providers {
		if _, ok := provider.(core.Provider); !ok {
			t.Errorf("provider %T does not implement core.Provider", provider)
		}
	}
}

func testHost() *Host {
	return &Host{
		config: HostConfig{WorkspaceRoot: "/tmp/workspace"},
		logger: discardTestLogger(),
	}
}

type fakeCloser struct {
	closed int
}

func (c *fakeCloser) Close() error {
	c.closed++
	return nil
}

type lifecycleServer struct {
	mu     sync.Mutex
	record []string
}

func (s *lifecycleServer) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.record...)
}

func (s *lifecycleServer) serve(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return
		}
		s.mu.Lock()
		s.record = append(s.record, message.Method)
		s.mu.Unlock()

		response := `{"jsonrpc":"2.0","id":"` + message.ID + `","result":{}}` + "\n"
		if _, err := conn.Write([]byte(response)); err != nil {
			return
		}
	}
}

func newLifecycleBridge(t *testing.T) (*Bridge, *lifecycleServer) {
	t.Helper()

	client, server := net.Pipe()
	state := &lifecycleServer{}
	bridge := NewBridge(client, client, nil)
	go state.serve(server)
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	return bridge, state
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
