package middleware

import (
	"fmt"
	"sync"
)

var scache *sessionCache

type sessionCache struct {
	instances map[string]*SessionsConfig
	mu        sync.RWMutex
}

func SessionCache() *sessionCache {
	if scache == nil {
		scache = &sessionCache{
			instances: make(map[string]*SessionsConfig),
		}
	}
	return scache
}

func (sc *sessionCache) Register(key string, instance *SessionsConfig) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if instance == nil {
		return
	}

	sc.instances[key] = instance
}

func (sc *sessionCache) RegisterWithErrorChecks(key string, instance *SessionsConfig) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if instance == nil {
		return fmt.Errorf("cannot register cache because SessionsConfig instance is nil where session key is %s", key)
	}

	if _, ok := sc.instances[key]; ok {
		return fmt.Errorf("cannot register cache because SessionsConfig instance already found in cache where session key is %s", key)
	}

	sc.instances[key] = instance

	return nil
}

func (sc *sessionCache) Get(key string) *SessionsConfig {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	instance, ok := sc.instances[key]

	if !ok {
		return nil
	}

	return instance
}

func (sc *sessionCache) Length() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.instances)
}

func (sc *sessionCache) Remove(key string) error {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if _, ok := sc.instances[key]; !ok {
		return nil
	}

	delete(sc.instances, key)

	return nil
}
