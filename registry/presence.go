package registry

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	redisClient "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"github.com/redis/go-redis/v9"
)

const presenceBaseTTL = 24 * time.Hour
const presencePersistentTTL = 7 * 24 * time.Hour

func presenceKey(prefix, name string) string {
	return prefix + "presence:" + name
}

func presenceIndexKey(prefix string) string {
	return prefix + "presence:_index"
}

type PresenceScope struct {
	Name          string   `json:"name"`
	FrontendAreas []string `json:"frontend_areas"`
}

type PresenceRecord struct {
	Name          string         `json:"name"`
	Scope         *PresenceScope `json:"scope,omitempty"`
	SecurityLevel int            `json:"security_level"`
	InteractsWith []string       `json:"interacts_with,omitempty"`
	InternalKey   string         `json:"internal_key,omitempty"`
	FirstSeen     time.Time      `json:"first_seen"`
	LastSeen      time.Time      `json:"last_seen"`
	ExpiresAt     time.Time      `json:"expires_at"`
}

type PresenceRegistry struct {
	mu      sync.RWMutex
	records map[string]PresenceRecord
}

var GlobalRegistry *PresenceRegistry

func NewPresenceRegistry() *PresenceRegistry {
	return &PresenceRegistry{records: make(map[string]PresenceRecord)}
}

func (r *PresenceRegistry) Register(name string, ttlSeconds int, scope *PresenceScope, securityLevel int, interactsWith []string) PresenceRecord {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		record = PresenceRecord{Name: name, FirstSeen: now}
	}
	record.LastSeen = now
	if scope != nil {
		record.Scope = scope
	}
	record.SecurityLevel = securityLevel
	record.InteractsWith = interactsWith
	if ttlSeconds > 0 {
		record.ExpiresAt = now.Add(time.Duration(ttlSeconds) * time.Second)
	} else {
		record.ExpiresAt = time.Time{}
	}
	r.records[name] = record

	r.persist(name, record)
	return record
}

func (r *PresenceRegistry) RegisterAuthKey(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		return "", errors.New("service not found")
	}

	key := utils.GenerateRandomToken(64)
	record.InternalKey = key
	r.records[name] = record

	r.persist(name, record)
	return key, nil
}

func (r *PresenceRegistry) RevokeAuthKey(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		return
	}

	record.InternalKey = ""
	r.records[name] = record
	r.persist(name, record)
}

func (r *PresenceRegistry) GetAllRecords() []PresenceRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	records := make([]PresenceRecord, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	return records
}

func (r *PresenceRegistry) persist(name string, record PresenceRecord) {
	if redisClient.Client == nil {
		return
	}
	prefix := config.AppConfig.Redis.Prefix
	ctx := context.Background()

	payload, err := json.Marshal(record)
	if err != nil {
		return
	}

	ttl := presenceBaseTTL
	if record.ExpiresAt.IsZero() {
		ttl = presencePersistentTTL
	} else if d := time.Until(record.ExpiresAt); d > 0 && d < ttl {
		ttl = d
	}

	key := presenceKey(prefix, name)
	pipe := redisClient.Client.TxPipeline()
	pipe.Set(ctx, key, payload, ttl)
	pipe.SAdd(ctx, presenceIndexKey(prefix), name)
	pipe.Expire(ctx, presenceIndexKey(prefix), 30*24*time.Hour)
	_, _ = pipe.Exec(ctx)
}

func (r *PresenceRegistry) Load(ctx context.Context) error {
	if redisClient.Client == nil {
		return nil
	}
	prefix := config.AppConfig.Redis.Prefix

	names, err := redisClient.Client.SMembers(ctx, presenceIndexKey(prefix)).Result()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, name := range names {
		raw, err := redisClient.Client.Get(ctx, presenceKey(prefix, name)).Result()
		if err == redis.Nil {
			_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
			continue
		}
		if err != nil {
			continue
		}
		var record PresenceRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			continue
		}
		if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
			_ = redisClient.Client.Del(ctx, presenceKey(prefix, name)).Err()
			_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
			continue
		}
		r.records[name] = record
	}
	return nil
}

func (r *PresenceRegistry) Get(name string) (PresenceRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		return PresenceRecord{}, false
	}
	if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
		delete(r.records, name)
		r.evict(name)
		return PresenceRecord{}, false
	}
	return record, true
}

func (r *PresenceRegistry) evict(name string) {
	if redisClient.Client == nil {
		return
	}
	prefix := config.AppConfig.Redis.Prefix
	ctx := context.Background()
	_ = redisClient.Client.Del(ctx, presenceKey(prefix, name)).Err()
	_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
}
