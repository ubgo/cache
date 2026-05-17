package cache

// EvictionCause classifies why an entry left the cache. Adapters report it via
// the OnEvict hook and aggregate counts in Stats.
type EvictionCause string

// Eviction causes reported via OnEvict and aggregated in Stats.
const (
	EvictSize     EvictionCause = "size"     // capacity (max entries / max bytes)
	EvictExpired  EvictionCause = "expired"  // TTL elapsed
	EvictExplicit EvictionCause = "explicit" // Del / Flush / DeleteByPrefix
	EvictReplaced EvictionCause = "replaced" // overwritten by a new Set
)

// Stats is an adapter-reported snapshot. Fields an adapter does not track stay
// zero. Counters are cumulative since process start; gauges are instantaneous.
type Stats struct {
	Hits      int64
	Misses    int64
	Sets      int64
	Deletes   int64
	Evictions int64

	// EvictionsByCause is cumulative, keyed by EvictionCause. May be nil.
	EvictionsByCause map[EvictionCause]int64

	// Entries / Bytes are instantaneous gauges (adapter-permitting).
	Entries int64
	Bytes   int64
}

// HitRatio is hits / (hits + misses). Returns 0 when there has been no traffic.
func (s Stats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}
