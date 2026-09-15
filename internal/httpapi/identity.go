package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/identity"
)

type IdentityService interface {
	Register(context.Context, identity.Registration) (identity.Authentication, error)
	Login(context.Context, string, string) (identity.Authentication, error)
	Me(context.Context, string) (identity.Account, []identity.Workspace, error)
	Logout(context.Context, string) error
}

type identityHandler struct{ service IdentityService }
type identityBody struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	Name          string `json:"name"`
	WorkspaceName string `json:"workspaceName"`
}

func (h identityHandler) register(c *gin.Context) {
	body, ok := h.body(c)
	if !ok {
		return
	}
	auth, err := h.service.Register(c.Request.Context(), identity.Registration{Email: body.Email, Password: body.Password, Name: body.Name, WorkspaceName: body.WorkspaceName})
	if err != nil {
		writeIdentityError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"data": auth})
}

func (h identityHandler) login(c *gin.Context) {
	body, ok := h.body(c)
	if !ok {
		return
	}
	auth, err := h.service.Login(c.Request.Context(), body.Email, body.Password)
	if err != nil {
		writeIdentityError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": auth})
}

func (h identityHandler) me(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	account, workspaces, err := h.service.Me(c.Request.Context(), token)
	if err != nil {
		writeIdentityError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": account, "workspaces": workspaces}})
}

func (h identityHandler) logout(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if err := h.service.Logout(c.Request.Context(), token); err != nil {
		writeIdentityError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func identityBearerToken(c *gin.Context) (string, bool) {
	parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		writeIdentityError(c, &identity.Error{Code: "unauthorized", Status: http.StatusUnauthorized})
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
}

func (h identityHandler) body(c *gin.Context) (identityBody, bool) {
	if h.service == nil {
		writeIdentityError(c, &identity.Error{Code: "identity_unavailable", Status: 503})
		return identityBody{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body identityBody
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeIdentityError(c, &identity.Error{Code: "invalid_request", Status: 400})
		return identityBody{}, false
	}
	return body, true
}

func writeIdentityError(c *gin.Context, err error) {
	var safe *identity.Error
	if !errors.As(err, &safe) {
		safe = &identity.Error{Code: "identity_unavailable", Status: 503}
	}
	messages := map[string]string{"invalid_request": "请检查账户信息", "invalid_credentials": "邮箱或密码错误", "email_taken": "该邮箱已经注册", "unauthorized": "登录已失效，请重新登录", "identity_unavailable": "账户服务暂时不可用"}
	c.Header("Cache-Control", "no-store")
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": messages[safe.Code]}})
}
