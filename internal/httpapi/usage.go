package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/usage"
)

type UsageService interface {
	Summary(context.Context, string, string) (usage.Summary, error)
	Approve(context.Context, string, string, string, string) error
}
type usageHandler struct{ service UsageService }

func (h usageHandler) approve(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeUsageError(c, &usage.Error{Code: "usage_unavailable", Status: 503})
		return
	}
	if err := h.service.Approve(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"), c.Param("approvalId")); err != nil {
		writeUsageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h usageHandler) summary(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeUsageError(c, &usage.Error{Code: "usage_unavailable", Status: 503})
		return
	}
	summary, err := h.service.Summary(c.Request.Context(), token, c.Param("workspaceId"))
	if err != nil {
		writeUsageError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": summary})
}

func writeUsageError(c *gin.Context, err error) {
	var safe *usage.Error
	if !errors.As(err, &safe) {
		safe = &usage.Error{Code: "usage_unavailable", Status: 503}
	}
	messages := map[string]string{"unauthorized": "登录已失效，请重新登录", "workspace_forbidden": "无权访问该工作区", "approval_invalid": "审批凭证无效或已使用", "usage_unavailable": "用量服务暂时不可用"}
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": messages[safe.Code]}})
}
