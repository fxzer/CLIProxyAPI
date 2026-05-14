package management

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type authRefreshQueueItem struct { // NOLINT
	ID            string  `json:"id"`
	AuthIndex     string  `json:"auth_index"`
	Name          string  `json:"name"`
	Provider      string  `json:"provider"`
	Status        string  `json:"status"`
	Unavailable   bool    `json:"unavailable"`
	Disabled      bool    `json:"disabled"`
	NextRefreshAt string  `json:"next_refresh_at"`
	AccountType   *string `json:"account_type,omitempty"`
	Account       *string `json:"account,omitempty"`
	Email         *string `json:"email,omitempty"`
}

type authRefreshQueueResponse struct {
	Queue       []authRefreshQueueItem `json:"queue"`
	Count       int                  `json:"count"`
	GeneratedAt string                `json:"generated_at"`
}

// GetAuthRefreshQueue returns the auth refresh queue status.
func (h *Handler) GetAuthRefreshQueue(c *gin.Context) { // NOLINT
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()

	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager unavailable"})
		return
	}

	auths := manager.List()
	items := make([]authRefreshQueueItem, 0, len(auths))
	now := time.Now()

	for _, auth := range auths {
		if auth == nil {
			continue
		}

		entry := buildRefreshQueueItem(auth, now)
		if entry != nil {
			items = append(items, *entry)
		}
	}

	response := authRefreshQueueResponse{
		Queue:       items,
		Count:       len(items),
		GeneratedAt: now.UTC().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, response)
}

func buildRefreshQueueItem(auth *coreauth.Auth, now time.Time) *authRefreshQueueItem { // NOLINT
	if auth == nil {
		return nil
	}

	auth.EnsureIndex()

	runtimeOnly := isRuntimeOnlyAuthInternal(auth)
	if runtimeOnly && (auth.Disabled || auth.Status == coreauth.StatusDisabled) {
		return nil
	}

	item := &authRefreshQueueItem{
		ID:           auth.ID,
		AuthIndex:    auth.Index,
		Name:         strings.TrimSpace(auth.FileName),
		Provider:     strings.TrimSpace(auth.Provider),
		Status:       string(auth.Status),
		Unavailable:  auth.Unavailable,
		Disabled:     auth.Disabled,
		NextRefreshAt: "",
	}

	if !auth.NextRefreshAfter.IsZero() {
		item.NextRefreshAt = auth.NextRefreshAfter.UTC().Format(time.RFC3339)
	} else if !auth.NextRetryAfter.IsZero() {
		item.NextRefreshAt = auth.NextRetryAfter.UTC().Format(time.RFC3339)
	} else {
		defaultRefresh := now.Add(1 * time.Hour)
		item.NextRefreshAt = defaultRefresh.UTC().Format(time.RFC3339)
	}

	if accountType, account := auth.AccountInfo(); accountType != "" || account != "" {
		if accountType != "" {
			item.AccountType = &accountType
		}
		if account != "" {
			item.Account = &account
		}
	}

	if email := authEmailInternal(auth); email != "" {
		item.Email = &email
	}

	return item
}

func isRuntimeOnlyAuthInternal(auth *coreauth.Auth) bool {
	if auth == nil || len(auth.Attributes) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true")
}

func authEmailInternal(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if email, ok := auth.Metadata["email"]; ok {
			if emailStr, isStr := email.(string); isStr {
				return strings.TrimSpace(emailStr)
			}
		}
	}
	return ""
}
