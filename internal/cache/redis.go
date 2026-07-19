package cache

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces all cache hashes so the RediSearch index can target them.
const keyPrefix = "relaycache:"

// RedisStore is a production vector store backed by Redis with the RediSearch
// module (redis-stack). It maintains an HNSW/FLAT vector index over cache hashes
// and answers KNN queries with a cosine distance metric.
//
// It speaks RESP2 explicitly so FT.SEARCH replies have a stable array shape that
// is parsed directly, avoiding protocol-dependent decoding.
type RedisStore struct {
	rdb   *redis.Client
	index string
	dims  int
}

// NewRedisStore connects to Redis and ensures the vector index exists.
func NewRedisStore(addr, password string, db, dims int) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		Protocol: 2,
	})
	s := &RedisStore{rdb: rdb, index: "relay_cache_idx", dims: dims}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	if err := s.ensureIndex(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// ensureIndex creates the RediSearch vector index if it does not already exist.
func (s *RedisStore) ensureIndex(ctx context.Context) error {
	// FT.INFO succeeds if the index exists; any error is treated as "missing".
	if err := s.rdb.Do(ctx, "FT.INFO", s.index).Err(); err == nil {
		return nil
	}
	args := []any{
		"FT.CREATE", s.index,
		"ON", "HASH",
		"PREFIX", "1", keyPrefix,
		"SCHEMA",
		"ns", "TAG",
		"model", "TAG",
		"created", "NUMERIC",
		"vector", "VECTOR", "HNSW", "8",
		"TYPE", "FLOAT32",
		"DIM", strconv.Itoa(s.dims),
		"DISTANCE_METRIC", "COSINE",
		"M", "16",
	}
	if err := s.rdb.Do(ctx, args...).Err(); err != nil {
		return fmt.Errorf("create vector index: %w", err)
	}
	return nil
}

// nsToken maps an arbitrary namespace string to a RediSearch-safe TAG token.
func nsToken(namespace string) string {
	sum := sha256.Sum256([]byte(namespace))
	return hex.EncodeToString(sum[:8])
}

// encodeVector serializes a float32 slice as little-endian bytes for RediSearch.
func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// Upsert stores an entry as a Redis hash and applies the TTL.
func (s *RedisStore) Upsert(ctx context.Context, e Entry, ttl time.Duration) error {
	key := keyPrefix + e.ID
	fields := map[string]any{
		"ns":       nsToken(e.Namespace),
		"ns_plain": e.Namespace,
		"model":    e.Model,
		"prompt":   e.Prompt,
		"response": e.Response,
		"created":  e.CreatedAt.Unix(),
		"vector":   encodeVector(e.Vector),
	}
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key, fields)
	if ttl > 0 {
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("cache upsert: %w", err)
	}
	return nil
}

// Search runs a KNN vector query filtered to the namespace tag.
func (s *RedisStore) Search(ctx context.Context, namespace string, vector []float32, k int) ([]Match, error) {
	query := fmt.Sprintf("(@ns:{%s})=>[KNN %d @vector $BLOB AS score]", nsToken(namespace), k)
	args := []any{
		"FT.SEARCH", s.index, query,
		"PARAMS", "2", "BLOB", encodeVector(vector),
		"SORTBY", "score", "ASC",
		"RETURN", "5", "score", "model", "prompt", "response", "ns_plain",
		"DIALECT", "2",
	}
	raw, err := s.rdb.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("cache search: %w", err)
	}
	return parseSearchReply(raw)
}

// parseSearchReply decodes the RESP2 FT.SEARCH array:
// [ total, key, [field, value, ...], key, [field, value, ...], ... ]
func parseSearchReply(raw any) ([]Match, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, nil
	}
	var matches []Match
	for i := 1; i+1 < len(arr); i += 2 {
		fieldsRaw, ok := arr[i+1].([]any)
		if !ok {
			continue
		}
		fields := map[string]string{}
		for j := 0; j+1 < len(fieldsRaw); j += 2 {
			key, _ := fieldsRaw[j].(string)
			val := toString(fieldsRaw[j+1])
			fields[key] = val
		}
		// COSINE distance = 1 - similarity.
		dist, _ := strconv.ParseFloat(fields["score"], 64)
		matches = append(matches, Match{
			Entry: Entry{
				Namespace: fields["ns_plain"],
				Model:     fields["model"],
				Prompt:    fields["prompt"],
				Response:  []byte(fields["response"]),
			},
			Score: 1 - dist,
		})
	}
	return matches, nil
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// Ping verifies connectivity.
func (s *RedisStore) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

// Close closes the underlying client.
func (s *RedisStore) Close() error { return s.rdb.Close() }

// Client exposes the underlying Redis client for reuse by other subsystems
// (rate limiting, accounting) that share the same connection.
func (s *RedisStore) Client() *redis.Client { return s.rdb }
