package main

import (
	"fmt"
	"log/slog"
	"sync"
)

// InMemoryCache is a thread safe in memory structure.
type InMemoryCache struct {
	mu    sync.RWMutex
	store map[string]string
}

func (c *InMemoryCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.store[key]
	return val, ok
}

func (c *InMemoryCache) Set(key string, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if val == "" {
		if _, ok := c.store[key]; ok {
			delete(c.store, key)
			slog.Info(fmt.Sprintf("Deleted cached value for vectorId '%s'", key))
		}
	} else {
		c.store[key] = val
	}
}
