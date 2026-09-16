package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/usage"
)

const maxAgentRequestBytes = 2 << 20
const agentHeartbeatInterval = 15 * time.Second

type AgentRunner interface {
	Run(context.Context, domain.AgentRequest) (<-chan domain.AgentEvent, error)
}

type WorkspaceAgent interface {
	Run(context.Context, string, string, domain.AgentRequest) (<-chan domain.AgentEvent, error)
}

type agentHandler struct {
	runner WorkspaceAgent
}

func (h agentHandler) run(c *gin.Context) {
	if h.runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "agent_unavailable", "message": "Agent 服务尚未准备好",
		})
		return
	}
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAgentRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	var request domain.AgentRequest
	if err := decoder.Decode(&request); err != nil {
		code := "invalid_json"
		status := http.StatusBadRequest
		if _, ok := err.(*http.MaxBytesError); ok {
			code = "request_too_large"
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": code, "message": "Agent 请求无法解析"})
		return
	}
	if err := ensureJSONEnded(decoder); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_json", "message": "请求只能包含一个 JSON 对象"})
		return
	}

	events, err := h.runner.Run(c.Request.Context(), token, c.Param("workspaceId"), request)
	if err != nil {
		writeAgentError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := io.WriteString(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	heartbeat := time.NewTicker(agentHeartbeatInterval)
	defer heartbeat.Stop()
	streamAgentEvents(c.Request.Context(), c.Writer, c.Writer.Flush, events, heartbeat.C)
}

func streamAgentEvents(ctx context.Context, writer io.Writer, flush func(), events <-chan domain.AgentEvent, heartbeats <-chan time.Time) {
	sawDone := false
	writeEvent := func(event domain.AgentEvent) bool {
		payload, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
			return false
		}
		flush()
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeats:
			if _, err := io.WriteString(writer, ": keepalive\n\n"); err != nil {
				return
			}
			flush()
		case event, ok := <-events:
			if !ok {
				if !sawDone && ctx.Err() == nil {
					if !writeEvent(domain.AgentEvent{Type: "error", Code: "agent_stream_incomplete", Message: "Agent 事件流意外中断", Retryable: true}) {
						return
					}
					writeEvent(domain.AgentEvent{Type: "done"})
				}
				return
			}
			if event.Type == "done" {
				sawDone = true
			}
			if !writeEvent(event) {
				return
			}
		}
	}
}

func writeAgentError(c *gin.Context, err error) {
	status, code, message := http.StatusBadRequest, "invalid_request", "Agent 请求不符合协议"
	var safe *usage.Error
	if errors.As(err, &safe) {
		status, code = safe.Status, safe.Code
		messages := map[string]string{"unauthorized": "登录已失效，请重新登录", "workspace_forbidden": "无权访问该工作区", "quota_exhausted": "工作区生成额度已用完", "approval_invalid": "审批凭证无效或已使用", "usage_unavailable": "用量服务暂时不可用", "invalid_request": "Agent 请求不符合协议"}
		message = messages[code]
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func ensureJSONEnded(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected second value")
	}
	return err
}
