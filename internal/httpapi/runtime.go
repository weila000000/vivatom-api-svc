package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/domain"
	runtimeservice "vivatom-api-svc/internal/runtime"
)

const maxRuntimeBodyBytes = 64 * 1024

type RuntimeService interface {
	Provision(context.Context, runtimeservice.ProvisionInput) (int, error)
	Inspect(context.Context, string, string, string) (runtimeservice.Inspection, error)
	Register(context.Context, string, string, string, string) (runtimeservice.Authentication, error)
	Login(context.Context, string, string, string, string) (runtimeservice.Authentication, error)
	Me(context.Context, string, string, string) (runtimeservice.User, error)
	Logout(context.Context, string, string, string) error
	CreateRecord(context.Context, string, string, string, string, map[string]any) (runtimeservice.Record, error)
	ListRecords(context.Context, string, string, string, string) ([]runtimeservice.Record, error)
	UpdateRecord(context.Context, string, string, string, string, string, map[string]any) (runtimeservice.Record, error)
	DeleteRecord(context.Context, string, string, string, string, string) error
}

type runtimeHandler struct {
	service RuntimeService
}

type provisionBody struct {
	Title   string             `json:"title"`
	Backend domain.BackendSpec `json:"backend"`
}

type credentialsBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h runtimeHandler) provision(c *gin.Context) {
	if h.service == nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "runtime_unavailable", Status: 503})
		return
	}
	publicKey, adminToken, ok := runtimeCredentials(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRuntimeBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body provisionBody
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeRuntimeError(c, &runtimeservice.Error{Code: "payload_too_large", Status: 413})
			return
		}
		writeRuntimeError(c, &runtimeservice.Error{Code: "invalid_request", Status: 400})
		return
	}
	if ensureJSONEnded(decoder) != nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "invalid_request", Status: 400})
		return
	}
	version, err := h.service.Provision(c.Request.Context(), runtimeservice.ProvisionInput{
		ProjectID: c.Param("projectId"), Title: body.Title, Backend: body.Backend,
		PublicKey: publicKey, AdminToken: adminToken,
	})
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"schemaVersion": version}})
}

func (h runtimeHandler) inspect(c *gin.Context) {
	if h.service == nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "runtime_unavailable", Status: 503})
		return
	}
	publicKey, adminToken, ok := runtimeCredentials(c)
	if !ok {
		return
	}
	inspection, err := h.service.Inspect(
		c.Request.Context(), c.Param("projectId"), publicKey, adminToken,
	)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": inspection})
}

func (h runtimeHandler) register(c *gin.Context) { h.authenticate(c, h.service.Register) }

func (h runtimeHandler) login(c *gin.Context) { h.authenticate(c, h.service.Login) }

func (h runtimeHandler) authenticate(c *gin.Context, action func(context.Context, string, string, string, string) (runtimeservice.Authentication, error)) {
	if h.service == nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "runtime_unavailable", Status: 503})
		return
	}
	publicKey, ok := publicCredential(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRuntimeBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body credentialsBody
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "invalid_request", Status: 400})
		return
	}
	authentication, err := action(c.Request.Context(), c.Param("projectId"), publicKey, body.Email, body.Password)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": authentication})
}

func (h runtimeHandler) me(c *gin.Context) {
	publicKey, ok := publicCredential(c)
	if !ok {
		return
	}
	token, ok := bearerToken(c)
	if !ok {
		return
	}
	user, err := h.service.Me(c.Request.Context(), c.Param("projectId"), publicKey, token)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": user}})
}

func (h runtimeHandler) logout(c *gin.Context) {
	publicKey, ok := publicCredential(c)
	if !ok {
		return
	}
	token, ok := bearerToken(c)
	if !ok {
		return
	}
	if err := h.service.Logout(c.Request.Context(), c.Param("projectId"), publicKey, token); err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func (h runtimeHandler) collection(c *gin.Context) {
	if h.service == nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "runtime_unavailable", Status: 503})
		return
	}
	publicKey, ok := publicCredential(c)
	if !ok {
		return
	}
	token := optionalBearerToken(c)
	if c.Request.Method == http.MethodGet {
		records, err := h.service.ListRecords(c.Request.Context(), c.Param("projectId"), publicKey, token, c.Param("collection"))
		if err != nil {
			writeRuntimeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"data": records})
		return
	}
	body, ok := recordBody(c)
	if !ok {
		return
	}
	record, err := h.service.CreateRecord(c.Request.Context(), c.Param("projectId"), publicKey, token, c.Param("collection"), body)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"data": record})
}

func (h runtimeHandler) record(c *gin.Context) {
	if h.service == nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "runtime_unavailable", Status: 503})
		return
	}
	publicKey, ok := publicCredential(c)
	if !ok {
		return
	}
	token := optionalBearerToken(c)
	if c.Request.Method == http.MethodDelete {
		err := h.service.DeleteRecord(c.Request.Context(), c.Param("projectId"), publicKey, token, c.Param("collection"), c.Param("recordId"))
		if err != nil {
			writeRuntimeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
		return
	}
	body, ok := recordBody(c)
	if !ok {
		return
	}
	record, err := h.service.UpdateRecord(c.Request.Context(), c.Param("projectId"), publicKey, token, c.Param("collection"), c.Param("recordId"), body)
	if err != nil {
		writeRuntimeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": record})
}

func recordBody(c *gin.Context) (map[string]any, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRuntimeBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeRuntimeError(c, &runtimeservice.Error{Code: "invalid_record", Status: 400})
		return nil, false
	}
	return body, true
}

func optionalBearerToken(c *gin.Context) string {
	const prefix = "Bearer "
	value := c.GetHeader("Authorization")
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(value[len(prefix):])
}

func publicCredential(c *gin.Context) (string, bool) {
	publicKey := c.GetHeader("X-Vivatom-App-Key")
	if publicKey == "" {
		writeRuntimeError(c, &runtimeservice.Error{Code: "forbidden", Status: 403})
		return "", false
	}
	return publicKey, true
}

func bearerToken(c *gin.Context) (string, bool) {
	const prefix = "Bearer "
	value := c.GetHeader("Authorization")
	if !strings.HasPrefix(value, prefix) || strings.TrimSpace(value[len(prefix):]) == "" {
		writeRuntimeError(c, &runtimeservice.Error{Code: "unauthorized", Status: 401})
		return "", false
	}
	return strings.TrimSpace(value[len(prefix):]), true
}

func runtimeCredentials(c *gin.Context) (string, string, bool) {
	publicKey := c.GetHeader("X-Vivatom-App-Key")
	adminToken := c.GetHeader("X-Vivatom-Admin-Token")
	if publicKey == "" || adminToken == "" {
		writeRuntimeError(c, &runtimeservice.Error{Code: "forbidden", Status: 403})
		return "", "", false
	}
	return publicKey, adminToken, true
}

func writeRuntimeError(c *gin.Context, err error) {
	safe, ok := err.(*runtimeservice.Error)
	if !ok {
		safe = &runtimeservice.Error{Code: "runtime_unavailable", Status: 503}
	}
	messages := map[string]string{
		"invalid_request":     "Runtime 请求不符合协议",
		"payload_too_large":   "Runtime 请求内容过大",
		"forbidden":           "Runtime 管理凭据无效",
		"unauthorized":        "登录会话无效或已过期",
		"invalid_credentials": "邮箱或密码错误",
		"email_taken":         "该邮箱已经注册",
		"auth_disabled":       "当前项目未启用邮箱密码登录",
		"invalid_record":      "记录字段不符合集合定义",
		"not_found":           "集合或记录不存在",
		"resource_limit":      "当前集合已达到记录数量上限",
		"schema_conflict":     "新的数据结构与现有数据空间不兼容",
		"provision_conflict":  "数据空间正在被其他请求更新",
		"runtime_unavailable": "Runtime 服务暂时不可用",
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(safe.Status, gin.H{
		"error": gin.H{"code": safe.Code, "message": messages[safe.Code]},
	})
}
