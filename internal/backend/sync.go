package backend

import (
	"context"
	"net/http"
	"sync"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
)

type syncCoordinator struct {
	mu      sync.Mutex
	name    string
	cancel  context.CancelFunc
	closing bool
	jobs    sync.WaitGroup
}

func (c *syncCoordinator) acquire(name string) (context.Context, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return nil, nil, httpjson.Error(http.StatusConflict, "OhneGuessr is stopping")
	}
	if c.name != "" {
		return nil, nil, httpjson.Error(http.StatusConflict, c.name+" synchronization is running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.name = name
	c.cancel = cancel
	c.jobs.Add(1)
	var once sync.Once
	release := func() {
		once.Do(func() {
			c.mu.Lock()
			c.name = ""
			c.cancel = nil
			c.mu.Unlock()
			c.jobs.Done()
		})
	}
	return ctx, release, nil
}

func (c *syncCoordinator) cancelJob(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.name != name || c.cancel == nil {
		return false
	}
	c.cancel()
	return true
}

func (c *syncCoordinator) shutdown(ctx context.Context) error {
	c.mu.Lock()
	c.closing = true
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.jobs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
