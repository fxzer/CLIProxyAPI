package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
)

type usageQueueRecord []byte

func (r usageQueueRecord) MarshalJSON() ([]byte, error) {
	if json.Valid(r) {
		return append([]byte(nil), r...), nil
	}
	return json.Marshal(string(r))
}

// GetUsageQueue pops queued usage records from the usage queue.
func (h *Handler) GetUsageQueue(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	count, errCount := parseUsageQueueCount(c.Query("count"))
	if errCount != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCount.Error()})
		return
	}

	items := redisqueue.PopOldest(count)
	records := make([]usageQueueRecord, 0, len(items))
	for _, item := range items {
		records = append(records, usageQueueRecord(append([]byte(nil), item...)))
	}

	c.JSON(http.StatusOK, records)
}

func parseUsageQueueCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	count, errCount := strconv.Atoi(value)
	if errCount != nil || count <= 0 {
		return 0, errors.New("count must be a positive integer")
	}
	return count, nil
}

// GetUsage returns aggregated usage statistics from the usage queue.
// Supports time range filtering: 7h, 24h, 7d, 30d, all
func (h *Handler) GetUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	timeRange := strings.TrimSpace(c.Query("time_range"))
	startTime := strings.TrimSpace(c.Query("start_time"))
	endTime := strings.TrimSpace(c.Query("end_time"))
	limitStr := strings.TrimSpace(c.Query("limit"))
	var limit int
	var err error

	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit parameter"})
			return
		}
	}

	allItems := redisqueue.PeekAll()
	if len(allItems) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"requests": []interface{}{},
			"stats": gin.H{
				"total_tokens":   0,
				"total_requests": 0,
				"total_cost":    0.0,
			},
		})
		return
	}

	var filteredItems [][]byte
	now := time.Now()

	for _, item := range allItems {
		var record map[string]interface{}
		if err := json.Unmarshal(item, &record); err != nil {
			continue
		}

		timestampStr, _ := record["timestamp"].(string)
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			continue
		}

		include := true

		if timeRange != "" {
			var duration time.Duration
			switch timeRange {
			case "7h":
				duration = 7 * time.Hour
			case "24h":
				duration = 24 * time.Hour
			case "7d":
				duration = 7 * 24 * time.Hour
			case "30d":
				duration = 30 * 24 * time.Hour
			case "all":
				duration = 365 * 24 * time.Hour
			default:
				duration = 24 * time.Hour
			}
			cutoff := now.Add(-duration)
			include = timestamp.After(cutoff)
		}

		if startTime != "" {
			start, err := time.Parse(time.RFC3339, startTime)
			if err == nil {
				include = include && timestamp.After(start)
			} else {
				include = false
			}
		}

		if endTime != "" {
			end, err := time.Parse(time.RFC3339, endTime)
			if err == nil {
				include = include && timestamp.Before(end)
			} else {
				include = false
			}
		}

		if include {
			filteredItems = append(filteredItems, item)
		}
		if limit > 0 && len(filteredItems) >= limit {
			break
		}
	}

	stats := calculateUsageStats(filteredItems)

	c.JSON(http.StatusOK, gin.H{
		"requests": filteredItems,
		"stats":    stats,
	})
}

type usageStats struct {
	TotalTokens   int64   `json:"total_tokens"`
	TotalRequests int64   `json:"total_requests"`
	TotalCost    float64 `json:"total_cost"`
}

func calculateUsageStats(items [][]byte) usageStats {
	stats := usageStats{}

	for _, item := range items {
		var record map[string]interface{}
		if err := json.Unmarshal(item, &record); err != nil {
			continue
		}

		tokens := int64(0)
		if tokensMap, ok := record["tokens"].(map[string]interface{}); ok {
			if totalTokens, ok := tokensMap["total_tokens"].(float64); ok {
				tokens = int64(totalTokens)
			} else {
				if inputTokens, ok := tokensMap["input_tokens"].(float64); ok {
					tokens += int64(inputTokens)
				}
				if outputTokens, ok := tokensMap["output_tokens"].(float64); ok {
					tokens += int64(outputTokens)
				}
			}
		}

		stats.TotalTokens += tokens
		stats.TotalRequests++

		if !record["failed"].(bool) {
			stats.TotalRequests++
		}

		if cost, ok := record["cost"].(float64); ok {
			stats.TotalCost += cost
		}
	}

	return stats
}
