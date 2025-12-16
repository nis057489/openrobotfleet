package behavior

import "sync"

type Blackboard struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func NewBlackboard() *Blackboard {
	return &Blackboard{
		data: make(map[string]interface{}),
	}
}

func (b *Blackboard) Set(key string, value interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = value
}

func (b *Blackboard) Get(key string) interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.data[key]
}

func (b *Blackboard) GetString(key string) string {
	val := b.Get(key)
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}
