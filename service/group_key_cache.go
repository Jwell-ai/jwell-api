package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/model"
	"github.com/go-redis/redis/v8"
)

const (
	// Redis key prefix for group key cache
	GroupKeyCachePrefix = "group_key:"
	// Default cache expiration (7 days)
	GroupKeyCacheExpiration = 7 * 24 * time.Hour
)

// GroupKeyCache represents a cached group key entry
type GroupKeyCache struct {
	Group       string `json:"group"`
	Key         string `json:"key"`
	UpdatedTime int64  `json:"updated_time"`
}

// InitGroupKeyCache initializes the group key cache at startup
// It loads all group keys from database and caches them in Redis
func InitGroupKeyCache() error {
	if !common.RedisEnabled {
		common.SysLog("Redis not enabled, skipping group key cache initialization")
		return nil
	}

	common.SysLog("Initializing group key cache...")

	// Get all tokens with their groups from database
	var tokens []model.Token
	err := model.DB.Find(&tokens).Error
	if err != nil {
		return fmt.Errorf("failed to load tokens for group key cache: %w", err)
	}

	// Build group -> key mapping
	// Note: In case of multiple tokens in same group, we use the first valid key
	groupKeyMap := make(map[string]string)
	for _, token := range tokens {
		if token.Status != 1 { // Skip disabled tokens
			continue
		}
		if token.Group == "" {
			continue
		}
		if _, exists := groupKeyMap[token.Group]; !exists {
			groupKeyMap[token.Group] = token.Key
		}
	}

	// Cache to Redis
	for group, key := range groupKeyMap {
		cache := GroupKeyCache{
			Group:       group,
			Key:         key,
			UpdatedTime: time.Now().Unix(),
		}

		cacheData, err := json.Marshal(cache)
		if err != nil {
			common.SysError(fmt.Sprintf("Failed to marshal group key cache for group %s: %v", group, err))
			continue
		}

		redisKey := GroupKeyCachePrefix + group
		err = common.RedisSet(redisKey, string(cacheData), GroupKeyCacheExpiration)
		if err != nil {
			common.SysError(fmt.Sprintf("Failed to cache group key for group %s: %v", group, err))
			continue
		}
	}

	common.SysLog(fmt.Sprintf("Group key cache initialized: %d groups cached", len(groupKeyMap)))
	return nil
}

// GetGroupKeyFromCache retrieves a group's key from Redis cache
// Returns empty string if not found or cache miss
func GetGroupKeyFromCache(group string) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}

	redisKey := GroupKeyCachePrefix + group
	cacheData, err := common.RedisGet(redisKey)
	if err != nil {
		if err == redis.Nil {
			return "", nil // Cache miss
		}
		return "", fmt.Errorf("failed to get group key from cache: %w", err)
	}

	var cache GroupKeyCache
	err = json.Unmarshal([]byte(cacheData), &cache)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal group key cache: %w", err)
	}

	return cache.Key, nil
}

// SetGroupKeyCache sets a group's key in Redis cache
func SetGroupKeyCache(group string, key string) error {
	if !common.RedisEnabled {
		return nil
	}

	cache := GroupKeyCache{
		Group:       group,
		Key:         key,
		UpdatedTime: time.Now().Unix(),
	}

	cacheData, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal group key cache: %w", err)
	}

	redisKey := GroupKeyCachePrefix + group
	return common.RedisSet(redisKey, string(cacheData), GroupKeyCacheExpiration)
}

// DeleteGroupKeyCache removes a group's key from Redis cache
func DeleteGroupKeyCache(group string) error {
	if !common.RedisEnabled {
		return nil
	}

	redisKey := GroupKeyCachePrefix + group
	return common.RedisDel(redisKey)
}

// RefreshGroupKeyCache refreshes the cache for a specific group from database
func RefreshGroupKeyCache(group string) error {
	if !common.RedisEnabled {
		return nil
	}

	// Get first valid token for this group
	var token model.Token
	err := model.DB.Where("`group` = ? AND status = 1", group).First(&token).Error
	if err != nil {
		if err == model.DB.Error {
			// No token found, delete cache
			return DeleteGroupKeyCache(group)
		}
		return fmt.Errorf("failed to query token for group %s: %w", group, err)
	}

	return SetGroupKeyCache(group, token.Key)
}

// StartGroupKeyCacheSync starts a background task to periodically sync group keys
func StartGroupKeyCacheSync(frequency int) {
	if !common.RedisEnabled {
		return
	}

	go func() {
		for {
			time.Sleep(time.Duration(frequency) * time.Second)
			common.SysLog("Syncing group key cache...")
			err := InitGroupKeyCache()
			if err != nil {
				common.SysError("Failed to sync group key cache: " + err.Error())
			}
		}
	}()
}
