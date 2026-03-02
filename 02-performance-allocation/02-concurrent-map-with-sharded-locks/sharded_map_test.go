package shardedmap

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestGetSet(t *testing.T) {
	m := NewShardedMap[string, int](16)

	m.Set("alice", 100)
	m.Set("bob", 200)

	val, ok := m.Get("alice")
	if !ok || val != 100 {
		t.Errorf("Get(alice) = (%d, %v), want (100, true)", val, ok)
	}

	val, ok = m.Get("bob")
	if !ok || val != 200 {
		t.Errorf("Get(bob) = (%d, %v), want (200, true)", val, ok)
	}

	_, ok = m.Get("charlie")
	if ok {
		t.Error("Get(charlie) should return false for missing key")
	}
}

func TestSetOverwrite(t *testing.T) {
	m := NewShardedMap[string, string](8)

	m.Set("key", "first")
	m.Set("key", "second")

	val, ok := m.Get("key")
	if !ok || val != "second" {
		t.Errorf("Get(key) = (%q, %v), want (\"second\", true)", val, ok)
	}
}

func TestDelete(t *testing.T) {
	m := NewShardedMap[string, int](8)

	m.Set("key", 42)
	m.Delete("key")

	_, ok := m.Get("key")
	if ok {
		t.Error("key should not exist after Delete")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	m := NewShardedMap[string, int](8)

	// Should not panic when deleting a key that doesn't exist.
	m.Delete("nonexistent")
}

func TestKeys(t *testing.T) {
	m := NewShardedMap[string, int](4)

	expected := map[string]bool{
		"a": true,
		"b": true,
		"c": true,
	}
	for k := range expected {
		m.Set(k, 1)
	}

	keys := m.Keys()
	if len(keys) != len(expected) {
		t.Fatalf("Keys() returned %d keys, want %d", len(keys), len(expected))
	}

	for _, k := range keys {
		if !expected[k] {
			t.Errorf("unexpected key %q in Keys() result", k)
		}
	}
}

func TestIntKeys(t *testing.T) {
	m := NewShardedMap[int, string](16)

	m.Set(1, "one")
	m.Set(2, "two")
	m.Set(3, "three")

	val, ok := m.Get(2)
	if !ok || val != "two" {
		t.Errorf("Get(2) = (%q, %v), want (\"two\", true)", val, ok)
	}

	keys := m.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys() returned %d keys, want 3", len(keys))
	}
}

func TestSingleShard(t *testing.T) {
	m := NewShardedMap[string, int](1)

	m.Set("a", 1)
	m.Set("b", 2)

	val, ok := m.Get("a")
	if !ok || val != 1 {
		t.Errorf("Get(a) = (%d, %v), want (1, true)", val, ok)
	}

	keys := m.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() returned %d keys, want 2", len(keys))
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	m := NewShardedMap[int, int](32)
	const numGoroutines = 100
	const numOps = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // readers + writers + deleters

	// Writers
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				m.Set(i, id)
			}
		}(g)
	}

	// Readers
	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				m.Get(i)
			}
		}()
	}

	// Deleters
	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				m.Delete(i)
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentKeys(t *testing.T) {
	m := NewShardedMap[int, int](16)
	const numOps = 1000

	var wg sync.WaitGroup
	wg.Add(3)

	// Writer
	go func() {
		defer wg.Done()
		for i := 0; i < numOps; i++ {
			m.Set(i, i)
		}
	}()

	// Keys reader (should not race with writer)
	go func() {
		defer wg.Done()
		for i := 0; i < numOps; i++ {
			_ = m.Keys()
		}
	}()

	// Deleter
	go func() {
		defer wg.Done()
		for i := 0; i < numOps; i++ {
			m.Delete(i)
		}
	}()

	wg.Wait()
}

func benchmarkShardedSet(b *testing.B, shardCount int) {
	m := NewShardedMap[int, int](shardCount)
	numGoroutines := runtime.GOMAXPROCS(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Set(i%10000, i)
			i++
		}
	})
	b.ReportMetric(float64(numGoroutines), "goroutines")
}

func BenchmarkSet_1Shard(b *testing.B) {
	benchmarkShardedSet(b, 1)
}

func BenchmarkSet_64Shards(b *testing.B) {
	benchmarkShardedSet(b, 64)
}

func benchmarkShardedGet(b *testing.B, shardCount int) {
	m := NewShardedMap[int, int](shardCount)
	// Pre-populate
	for i := 0; i < 10000; i++ {
		m.Set(i, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Get(i % 10000)
			i++
		}
	})
}

func BenchmarkGet_1Shard(b *testing.B) {
	benchmarkShardedGet(b, 1)
}

func BenchmarkGet_64Shards(b *testing.B) {
	benchmarkShardedGet(b, 64)
}

// Mixed read-heavy workload: 95% reads, 5% writes (matches the scenario)
func BenchmarkMixed95Read_64Shards(b *testing.B) {
	m := NewShardedMap[int, int](64)
	for i := 0; i < 10000; i++ {
		m.Set(i, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%20 == 0 {
				m.Set(i%10000, i)
			} else {
				m.Get(i % 10000)
			}
			i++
		}
	})
}

func TestMemoryOverhead(t *testing.T) {
	const numKeys = 1_000_000

	// Measure baseline: plain map
	runtime.GC()
	var baselineBefore runtime.MemStats
	runtime.ReadMemStats(&baselineBefore)

	plainMap := make(map[int]int, numKeys)
	for i := 0; i < numKeys; i++ {
		plainMap[i] = i
	}

	runtime.GC()
	var baselineAfter runtime.MemStats
	runtime.ReadMemStats(&baselineAfter)
	baselineAlloc := baselineAfter.Alloc - baselineBefore.Alloc

	// Clear
	plainMap = nil
	runtime.GC()

	// Measure sharded map
	var shardedBefore runtime.MemStats
	runtime.ReadMemStats(&shardedBefore)

	sm := NewShardedMap[int, int](64)
	for i := 0; i < numKeys; i++ {
		sm.Set(i, i)
	}

	runtime.GC()
	var shardedAfter runtime.MemStats
	runtime.ReadMemStats(&shardedAfter)
	shardedAlloc := shardedAfter.Alloc - shardedBefore.Alloc

	overhead := int64(shardedAlloc) - int64(baselineAlloc)
	maxOverhead := int64(50 * 1024 * 1024) // 50MB

	t.Logf("Baseline map alloc: %d MB", baselineAlloc/(1024*1024))
	t.Logf("Sharded map alloc:  %d MB", shardedAlloc/(1024*1024))
	t.Logf("Overhead:           %d MB", overhead/(1024*1024))

	if overhead > maxOverhead {
		t.Errorf("memory overhead %d MB exceeds 50MB limit", overhead/(1024*1024))
	}
}

func TestKeyDistribution(t *testing.T) {
	const shardCount = 16
	const numKeys = 10000
	m := NewShardedMap[string, int](shardCount)

	for i := 0; i < numKeys; i++ {
		m.Set(fmt.Sprintf("key-%d", i), i)
	}

	keys := m.Keys()
	if len(keys) != numKeys {
		t.Fatalf("expected %d keys, got %d", numKeys, len(keys))
	}

	// Count keys per shard to check distribution
	shardCounts := make([]int, shardCount)
	for i := range m.shards {
		m.locks[i].RLock()
		shardCounts[i] = len(m.shards[i])
		m.locks[i].RUnlock()
	}

	// Expect roughly even distribution: each shard should have at least 25%
	// of the average (numKeys/shardCount)
	avg := numKeys / shardCount
	minAcceptable := avg / 4

	for i, count := range shardCounts {
		t.Logf("Shard %2d: %d keys", i, count)
		if count < minAcceptable {
			t.Errorf("shard %d has only %d keys (min acceptable: %d) - poor distribution", i, count, minAcceptable)
		}
	}
}
