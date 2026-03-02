package shardedmap

import (
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
	// TODO: Implement
	return nil
}

// Get retrieves a value by key. Returns the value and whether it was found.
// Hint: use RLock for read operations.
func (m *ShardedMap[K, V]) Get(key K) (V, bool) {
	// TODO: Implement
	var zero V
	return zero, false
}

// Set inserts or updates a key-value pair.
func (m *ShardedMap[K, V]) Set(key K, value V) {
	// TODO: Implement
}

// Delete removes a key from the map.
func (m *ShardedMap[K, V]) Delete(key K) {
	// TODO: Implement
}

// Keys returns all keys across all shards. Order is not guaranteed.
// Hint: must be safe to call concurrently with writes.
func (m *ShardedMap[K, V]) Keys() []K {
	// TODO: Implement
	return nil
}

// shardIndex returns the shard index for the given key using FNV-64a hashing.
// Hint: convert the key to bytes in a way that avoids allocations on the hot path.
// You can use hash/fnv and the encoding/binary package.
func (m *ShardedMap[K, V]) shardIndex(key K) int {
	// TODO: Implement
	return 0
}

// Ensure hash/fnv and sync are used (remove these once you use them in your implementation).
var _ = fnv.New64a
var _ sync.RWMutex
