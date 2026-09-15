package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/audit"
)

type AuditService interface {
	List(context.Context, string, string, string, int) ([]audit.Event, error)
}

type auditHandler struct{ service AuditService }

func (h auditHandler) list(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeAuditError(c, &audit.Error{Code: "audit_unavailable", Status: http.StatusServiceUnavailable})
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			writeAuditError(c, &audit.Error{Code: "invalid_limit", Status: http.StatusBadRequest})
			return
		}
	}
	events, err := h.service.List(c.Request.Context(), token, c.Param("workspaceId"), c.Query("before"), limit)
	if err != nil {
		writeAuditError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": events})
}

func writeAuditError(c *gin.Context, err error) {
	var safe *audit.Error
	if !errors.As(err, &safe) {
		safe = &audit.Error{Code: "audit_unavailable", Status: http.StatusServiceUnavailable}
	}
	messages := map[string]string{
		"invalid_cursor":      "活动游标无效",
		"invalid_limit":       "活动数量无效",
		"workspace_forbidden": "无权访问该工作区",
		"unauthorized":        "登录已失效，请重新登录",
		"audit_unavailable":   "工作区活动暂时不可用",
	}
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": messages[safe.Code]}})
}
