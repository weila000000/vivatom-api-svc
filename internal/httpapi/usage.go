package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/usage"
)

type UsageService interface {
	Summary(context.Context, string, string) (usage.Summary, error)
	Approve(context.Context, string, string, string, string) error
	CommitCandidate(context.Context, string, string, string, string, string, string, string) (usage.Version, error)
	RestageVersion(context.Context, string, string, string, string) (usage.Candidate, error)
}
type usageHandler struct{ service UsageService }

func (h usageHandler) restageVersion(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeUsageError(c, &usage.Error{Code: "usage_unavailable", Status: 503})
		return
	}
	candidate, err := h.service.RestageVersion(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"), c.Param("versionId"))
	if err != nil {
		writeUsageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": candidate})
}

type commitCandidateBody struct {
	SnapshotHash    string `json:"snapshotHash"`
	ParentVersionID string `json:"parentVersionId"`
	Prompt          string `json:"prompt"`
}

func (h usageHandler) commitCandidate(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeUsageError(c, &usage.Error{Code: "usage_unavailable", Status: 503})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024))
	decoder.DisallowUnknownFields()
	var body commitCandidateBody
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeUsageError(c, &usage.Error{Code: "invalid_request", Status: http.StatusBadRequest})
		return
	}
	version, err := h.service.CommitCandidate(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"), c.Param("candidateId"), body.SnapshotHash, body.ParentVersionID, body.Prompt)
	if err != nil {
		writeUsageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": version})
}

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
	messages := map[string]string{"unauthorized": "登录已失效，请重新登录", "workspace_forbidden": "无权访问该工作区", "approval_invalid": "审批凭证无效或已使用", "candidate_invalid": "候选源码凭证无效或已提交", "version_conflict": "项目已发布更新，请同步后重新构建", "version_untrusted": "该历史版本没有可信的服务端产物记录", "compile_in_progress": "候选源码正在隔离编译，请稍后重试", "compile_busy": "隔离编译任务已满，请稍后重试", "compile_failed": "候选源码未通过服务端隔离编译", "compiler_unavailable": "隔离编译服务暂时不可用", "artifact_unavailable": "构建产物缺失或完整性校验失败", "invalid_request": "提交版本的请求不符合协议", "usage_unavailable": "用量服务暂时不可用"}
	message := messages[safe.Code]
	if safe.Code == "compile_failed" && safe.Message != "" {
		message += "：" + safe.Message
	}
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": message}})
}
