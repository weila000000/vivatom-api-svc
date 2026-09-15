package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/team"
)

type TeamService interface {
	List(context.Context, string, string) ([]team.Member, error)
	Invite(context.Context, string, string, string, string) (team.Invitation, error)
	Accept(context.Context, string, string) (team.Member, error)
	ChangeRole(context.Context, string, string, string, string) error
	Remove(context.Context, string, string, string) error
}
type teamHandler struct{ service TeamService }
type teamBody struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h teamHandler) list(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	members, err := h.service.List(c.Request.Context(), token, c.Param("workspaceId"))
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": members})
}
func (h teamHandler) invite(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	var body teamBody
	if !decodeTeamBody(c, &body) {
		return
	}
	invitation, err := h.service.Invite(c.Request.Context(), token, c.Param("workspaceId"), body.Email, body.Role)
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"data": invitation})
}
func (h teamHandler) accept(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	member, err := h.service.Accept(c.Request.Context(), token, c.Param("token"))
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": member})
}
func (h teamHandler) role(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	var body teamBody
	if !decodeTeamBody(c, &body) {
		return
	}
	if err := h.service.ChangeRole(c.Request.Context(), token, c.Param("workspaceId"), c.Param("accountId"), body.Role); err != nil {
		writeTeamError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h teamHandler) remove(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if err := h.service.Remove(c.Request.Context(), token, c.Param("workspaceId"), c.Param("accountId")); err != nil {
		writeTeamError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func decodeTeamBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || ensureJSONEnded(decoder) != nil {
		writeTeamError(c, &team.Error{Code: "invalid_request", Status: 400})
		return false
	}
	return true
}
func writeTeamError(c *gin.Context, err error) {
	var safe *team.Error
	if !errors.As(err, &safe) {
		safe = &team.Error{Code: "team_unavailable", Status: 503}
	}
	messages := map[string]string{"invalid_request": "成员请求不符合协议", "invalid_invitation": "邀请信息不正确", "invitation_invalid": "邀请已失效或已使用", "invitation_email_mismatch": "请使用受邀邮箱登录", "invalid_role": "成员角色不正确", "owner_required": "只有工作区所有者可以执行此操作", "last_owner": "工作区必须保留至少一位所有者", "member_not_found": "成员不存在", "workspace_forbidden": "无权访问该工作区", "unauthorized": "登录已失效，请重新登录", "team_unavailable": "成员服务暂时不可用"}
	c.Header("Cache-Control", "no-store")
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": messages[safe.Code]}})
}
