package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/catalog"
)

type CatalogService interface {
	Sync(context.Context, string, string, catalog.SyncInput) (catalog.Project, error)
	List(context.Context, string, string) ([]catalog.Project, error)
	SaveDocument(context.Context, string, string, string, int, catalog.DocumentPayload) (catalog.Document, error)
	GetDocument(context.Context, string, string, string) (catalog.Document, error)
	ResolveConflict(context.Context, string, string, string, string) error
}

type documentBody struct {
	ExpectedRevision int                     `json:"expectedRevision"`
	Payload          catalog.DocumentPayload `json:"payload"`
}

type catalogHandler struct{ service CatalogService }

type catalogBody struct {
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	ActiveVersionID *string `json:"activeVersionId"`
}

type conflictResolutionBody struct {
	Choice string `json:"choice"`
}

func (h catalogHandler) resolveConflict(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeCatalogError(c, &catalog.Error{Code: "catalog_unavailable", Status: 503})
		return
	}
	var body conflictResolutionBody
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeCatalogError(c, &catalog.Error{Code: "invalid_conflict_resolution", Status: http.StatusBadRequest})
		return
	}
	if err := h.service.ResolveConflict(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"), body.Choice); err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h catalogHandler) getDocument(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeCatalogError(c, &catalog.Error{Code: "catalog_unavailable", Status: 503})
		return
	}
	document, err := h.service.GetDocument(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"))
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": document})
}

func (h catalogHandler) saveDocument(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeCatalogError(c, &catalog.Error{Code: "catalog_unavailable", Status: 503})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024+64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body documentBody
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeCatalogError(c, &catalog.Error{Code: "invalid_document", Status: 400})
		return
	}
	document, err := h.service.SaveDocument(c.Request.Context(), token, c.Param("workspaceId"), c.Param("projectId"), body.ExpectedRevision, body.Payload)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": document})
}

func (h catalogHandler) list(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeCatalogError(c, &catalog.Error{Code: "catalog_unavailable", Status: http.StatusServiceUnavailable})
		return
	}
	projects, err := h.service.List(c.Request.Context(), token, c.Param("workspaceId"))
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": projects})
}

func (h catalogHandler) sync(c *gin.Context) {
	token, ok := identityBearerToken(c)
	if !ok {
		return
	}
	if h.service == nil {
		writeCatalogError(c, &catalog.Error{Code: "catalog_unavailable", Status: http.StatusServiceUnavailable})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body catalogBody
	if err := decoder.Decode(&body); err != nil || ensureJSONEnded(decoder) != nil {
		writeCatalogError(c, &catalog.Error{Code: "invalid_project", Status: http.StatusBadRequest})
		return
	}
	project, err := h.service.Sync(c.Request.Context(), token, c.Param("workspaceId"), catalog.SyncInput{
		ID: c.Param("projectId"), Title: body.Title, Status: body.Status, ActiveVersionID: body.ActiveVersionID,
	})
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": project})
}

func writeCatalogError(c *gin.Context, err error) {
	var safe *catalog.Error
	if !errors.As(err, &safe) {
		safe = &catalog.Error{Code: "catalog_unavailable", Status: http.StatusServiceUnavailable}
	}
	messages := map[string]string{
		"invalid_project":             "项目目录信息不符合协议",
		"workspace_forbidden":         "无权访问该工作区",
		"unauthorized":                "登录已失效，请重新登录",
		"catalog_unavailable":         "项目目录暂时不可用",
		"invalid_document":            "项目内容不符合同步协议",
		"document_too_large":          "项目内容超过同步大小限制",
		"document_conflict":           "项目已在其他设备更新，请先同步最新内容",
		"document_not_found":          "项目还没有可恢复的云端内容",
		"invalid_conflict_resolution": "冲突处理方式无效",
	}
	c.JSON(safe.Status, gin.H{"error": gin.H{"code": safe.Code, "message": messages[safe.Code]}})
}
