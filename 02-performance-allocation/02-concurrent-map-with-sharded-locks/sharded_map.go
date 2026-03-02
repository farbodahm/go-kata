package shardedmap

import (
	"encoding/binary"
	"hash/fnv"
	"sync"
)

// ShardedMap is a generic, thread-safe map that distributes keys across
// multiple shards to reduce lock contention under high concurrency.
// Each shard has its own RWMutex and underlying map.
type ShardedMap[K comparable, V any] struct {
	shards []map[K]V
	locks  []sync.RWMutex
	count  int
}

// NewShardedMap creates a new ShardedMap with the given number of shards.
// Each shard is initialized with its own map and RWMutex.
func NewShardedMap[K comparable, V any](shardCount int) *ShardedMap[K, V] {
	locks := make([]sync.RWMutex, shardCount)
	shards := make([]map[K]V, shardCount)
	for i := range shards {
		shards[i] = make(map[K]V)
	}

	return &ShardedMap[K, V]{
		shards: shards,
		locks:  locks,
		count:  shardCount,
	}
}

// Get retrieves a value by key. Returns the value and whether it was found.
func (m *ShardedMap[K, V]) Get(key K) (V, bool) {
	index := m.shardIndex(key)
	m.locks[index].RLock()
	defer m.locks[index].RUnlock()

	value, found := m.shards[index][key]
	return value, found
}

// Set inserts or updates a key-value pair.
func (m *ShardedMap[K, V]) Set(key K, value V) {
	index := m.shardIndex(key)
	m.locks[index].Lock()
	defer m.locks[index].Unlock()

	m.shards[index][key] = value
}

// Delete removes a key from the map.
func (m *ShardedMap[K, V]) Delete(key K) {
	index := m.shardIndex(key)
	m.locks[index].Lock()
	defer m.locks[index].Unlock()

	delete(m.shards[index], key)
}

// Keys returns all keys across all shards. Order is not guaranteed.
func (m *ShardedMap[K, V]) Keys() []K {
	var keys []K
	for i := range m.shards {
		// TODO: This is still not safe, as by the time of iteration, the shard could have been modified.
		m.locks[i].RLock()
		for key := range m.shards[i] {
			keys = append(keys, key)
		}
		m.locks[i].RUnlock()
	}

	return keys
}

// shardIndex returns the shard index for the given key using FNV-64a hashing.
func (m *ShardedMap[K, V]) shardIndex(key K) int {
	h := fnv.New64a()
	switch v := any(key).(type) {
	case int:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	case string:
		h.Write([]byte(v))
	default:
		panic("unsupported key type")
	}

	return int(h.Sum64() % uint64(m.count))
}
