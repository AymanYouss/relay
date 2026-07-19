package usage

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Retention windows for the Redis rollups.
const (
	monthTTL  = 45 * 24 * time.Hour
	dailyTTL  = 100 * 24 * time.Hour
	latSample = 10_000 // per-day latency samples retained for percentiles
	recentCap = 100
)

// RedisStore persists accounting rollups in Redis so budgets and dashboards are
// consistent across gateway replicas and survive restarts. Writes are batched
// into a single pipeline per event to keep the hot path cheap.
type RedisStore struct {
	rdb *redis.Client
	now func() time.Time
}

// NewRedisStore builds a Redis-backed usage store.
func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb, now: time.Now}
}

func modelField(prefix, model, provider string) string {
	return prefix + ":" + model + "|" + provider
}

// Record writes all rollups for one event in a single pipeline.
func (s *RedisStore) Record(ctx context.Context, e Event) error {
	if e.Time.IsZero() {
		e.Time = s.now()
	}
	day := dayKey(e.Time)
	month := monthKey(e.Time)

	dayHash := "relay:stats:day:" + day
	modelHash := "relay:stats:model:" + day
	routeHash := "relay:stats:route:" + day
	latList := "relay:stats:lat:" + day
	monthHash := "relay:usage:" + e.KeyID + ":" + month
	keySet := "relay:usage:keys:" + month
	recentList := "relay:stats:recent"

	pipe := s.rdb.Pipeline()

	pipe.HIncrBy(ctx, dayHash, "requests", 1)
	pipe.HIncrByFloat(ctx, dayHash, "cost", e.CostUSD)
	pipe.HIncrByFloat(ctx, dayHash, "saved", e.SavedUSD)
	pipe.HIncrBy(ctx, dayHash, "prompt_tokens", int64(e.PromptTokens))
	pipe.HIncrBy(ctx, dayHash, "completion_tokens", int64(e.CompletionTokens))
	if e.CacheHit {
		pipe.HIncrBy(ctx, dayHash, "cache_hits", 1)
	}
	if e.Status == "error" {
		pipe.HIncrBy(ctx, dayHash, "errors", 1)
	}
	pipe.Expire(ctx, dayHash, dailyTTL)

	if e.Model != "" {
		pipe.HIncrBy(ctx, modelHash, modelField("req", e.Model, e.Provider), 1)
		pipe.HIncrByFloat(ctx, modelHash, modelField("cost", e.Model, e.Provider), e.CostUSD)
		pipe.HIncrBy(ctx, modelHash, modelField("tok", e.Model, e.Provider), int64(e.PromptTokens+e.CompletionTokens))
		pipe.Expire(ctx, modelHash, dailyTTL)
	}
	if e.Strategy != "" {
		pipe.HIncrBy(ctx, routeHash, e.Strategy, 1)
		pipe.Expire(ctx, routeHash, dailyTTL)
	}
	if e.LatencyMS > 0 {
		pipe.LPush(ctx, latList, e.LatencyMS)
		pipe.LTrim(ctx, latList, 0, latSample-1)
		pipe.Expire(ctx, latList, dailyTTL)
	}

	if e.KeyID != "" {
		pipe.HIncrByFloat(ctx, monthHash, "cost", e.CostUSD)
		pipe.HIncrBy(ctx, monthHash, "requests", 1)
		pipe.HIncrBy(ctx, monthHash, "tokens", int64(e.PromptTokens+e.CompletionTokens))
		if e.KeyName != "" {
			pipe.HSet(ctx, monthHash, "name", e.KeyName)
		}
		pipe.Expire(ctx, monthHash, monthTTL)
		pipe.SAdd(ctx, keySet, e.KeyID)
		pipe.Expire(ctx, keySet, monthTTL)
	}

	if payload, err := json.Marshal(e); err == nil {
		pipe.LPush(ctx, recentList, payload)
		pipe.LTrim(ctx, recentList, 0, recentCap-1)
		pipe.Expire(ctx, recentList, dailyTTL)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// MonthSpend reads a key's month-to-date cost.
func (s *RedisStore) MonthSpend(ctx context.Context, keyID string) (float64, error) {
	month := monthKey(s.now())
	v, err := s.rdb.HGet(ctx, "relay:usage:"+keyID+":"+month, "cost").Float64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

// Query aggregates the last windowDays days from the daily rollups.
func (s *RedisStore) Query(ctx context.Context, windowDays int) (Dashboard, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	now := s.now().UTC()
	var dash Dashboard
	summary := Summary{WindowDays: windowDays}

	var allLatencies []float64
	modelAgg := map[string]*ModelUsage{}
	routeAgg := map[string]int64{}

	for i := windowDays - 1; i >= 0; i-- {
		day := dayKey(now.AddDate(0, 0, -i))
		dh, err := s.rdb.HGetAll(ctx, "relay:stats:day:"+day).Result()
		if err != nil && err != redis.Nil {
			return dash, err
		}
		point := DailyPoint{Date: day}
		point.Requests = parseInt(dh["requests"])
		point.CacheHits = parseInt(dh["cache_hits"])
		point.CostUSD = parseFloat(dh["cost"])
		point.CostSavedUSD = parseFloat(dh["saved"])
		if point.Requests > 0 {
			point.CacheHitRate = float64(point.CacheHits) / float64(point.Requests)
		}

		lat, err := s.rdb.LRange(ctx, "relay:stats:lat:"+day, 0, -1).Result()
		if err != nil && err != redis.Nil {
			return dash, err
		}
		dayLat := make([]float64, 0, len(lat))
		for _, l := range lat {
			if v, e := strconv.ParseFloat(l, 64); e == nil {
				dayLat = append(dayLat, v)
				allLatencies = append(allLatencies, v)
			}
		}
		if len(dayLat) > 0 {
			sort.Float64s(dayLat)
			point.LatencyP50 = percentile(dayLat, 50)
			point.LatencyP95 = percentile(dayLat, 95)
			point.LatencyP99 = percentile(dayLat, 99)
		}
		dash.Series = append(dash.Series, point)

		summary.Requests += point.Requests
		summary.CacheHits += point.CacheHits
		summary.Errors += parseInt(dh["errors"])
		summary.TotalCostUSD += point.CostUSD
		summary.CostSavedUSD += point.CostSavedUSD
		summary.PromptTokens += parseInt(dh["prompt_tokens"])
		summary.CompletionTokens += parseInt(dh["completion_tokens"])

		mh, err := s.rdb.HGetAll(ctx, "relay:stats:model:"+day).Result()
		if err != nil && err != redis.Nil {
			return dash, err
		}
		accumulateModels(modelAgg, mh)

		rh, err := s.rdb.HGetAll(ctx, "relay:stats:route:"+day).Result()
		if err != nil && err != redis.Nil {
			return dash, err
		}
		for strat, n := range rh {
			routeAgg[strat] += parseInt(n)
		}
	}

	if summary.Requests > 0 {
		summary.CacheHitRate = float64(summary.CacheHits) / float64(summary.Requests)
		summary.ErrorRate = float64(summary.Errors) / float64(summary.Requests)
	}
	if len(allLatencies) > 0 {
		sort.Float64s(allLatencies)
		summary.LatencyP50 = percentile(allLatencies, 50)
		summary.LatencyP95 = percentile(allLatencies, 95)
		summary.LatencyP99 = percentile(allLatencies, 99)
		var sum float64
		for _, l := range allLatencies {
			sum += l
		}
		summary.LatencyAvg = sum / float64(len(allLatencies))
	}
	dash.Summary = summary

	for _, mu := range modelAgg {
		if summary.Requests > 0 {
			mu.Share = float64(mu.Requests) / float64(summary.Requests)
		}
		dash.Models = append(dash.Models, *mu)
	}
	sort.Slice(dash.Models, func(i, j int) bool { return dash.Models[i].Requests > dash.Models[j].Requests })

	for strat, n := range routeAgg {
		dash.Routes = append(dash.Routes, RouteBreakdown{Strategy: strat, Requests: n})
	}
	sort.Slice(dash.Routes, func(i, j int) bool { return dash.Routes[i].Requests > dash.Routes[j].Requests })

	// Per-key month-to-date usage.
	month := monthKey(now)
	keyIDs, err := s.rdb.SMembers(ctx, "relay:usage:keys:"+month).Result()
	if err != nil && err != redis.Nil {
		return dash, err
	}
	for _, kid := range keyIDs {
		h, err := s.rdb.HGetAll(ctx, "relay:usage:"+kid+":"+month).Result()
		if err != nil {
			continue
		}
		dash.Keys = append(dash.Keys, KeyUsage{
			KeyID:    kid,
			Name:     h["name"],
			Requests: parseInt(h["requests"]),
			CostUSD:  parseFloat(h["cost"]),
			Tokens:   parseInt(h["tokens"]),
		})
	}
	sort.Slice(dash.Keys, func(i, j int) bool { return dash.Keys[i].CostUSD > dash.Keys[j].CostUSD })

	// Recent events feed.
	recent, err := s.rdb.LRange(ctx, "relay:stats:recent", 0, 49).Result()
	if err != nil && err != redis.Nil {
		return dash, err
	}
	for _, r := range recent {
		var e Event
		if json.Unmarshal([]byte(r), &e) == nil {
			dash.Recent = append(dash.Recent, e)
		}
	}

	dash.ensureNonNil()
	return dash, nil
}

func accumulateModels(agg map[string]*ModelUsage, h map[string]string) {
	for field, val := range h {
		// field is "<prefix>:<model>|<provider>"
		prefix, rest, ok := cut(field, ":")
		if !ok {
			continue
		}
		model, provider, _ := cut(rest, "|")
		mk := model + "|" + provider
		mu := agg[mk]
		if mu == nil {
			mu = &ModelUsage{Model: model, Provider: provider}
			agg[mk] = mu
		}
		switch prefix {
		case "req":
			mu.Requests += parseInt(val)
		case "cost":
			mu.CostUSD += parseFloat(val)
		case "tok":
			mu.Tokens += parseInt(val)
		}
	}
}

// Close is a no-op; the Redis client lifecycle is owned by the caller.
func (s *RedisStore) Close() error { return nil }

func parseInt(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func cut(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}
