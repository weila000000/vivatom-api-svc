package httpapi

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	Ready           func() error
	AgentRunner     WorkspaceAgent
	Usage           UsageService
	Runtime         RuntimeService
	Identity        IdentityService
	Catalog         CatalogService
	Team            TeamService
	Audit           AuditService
	Logger          *slog.Logger
	AllowedOrigin   string
	AuthRateLimit   int
	AgentRateLimit  int
	RateLimitWindow time.Duration
	TrustedProxies  []string
}

func NewRouter(deps Dependencies) *gin.Engine {
	router := gin.New()
	if err := router.SetTrustedProxies(deps.TrustedProxies); err != nil {
		panic(err)
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if deps.AllowedOrigin == "" {
		deps.AllowedOrigin = "http://localhost:5173"
	}
	if deps.AuthRateLimit <= 0 {
		deps.AuthRateLimit = 20
	}
	if deps.AgentRateLimit <= 0 {
		deps.AgentRateLimit = 10
	}
	if deps.RateLimitWindow <= 0 {
		deps.RateLimitWindow = time.Minute
	}
	authLimiter := newRateLimiter(deps.AuthRateLimit, deps.RateLimitWindow)
	agentLimiter := newRateLimiter(deps.AgentRateLimit, deps.RateLimitWindow)
	router.Use(requestID(), accessLog(logger), gin.Recovery(), cors(deps.AllowedOrigin))

	api := router.Group("/api")
	api.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api.GET("/health/ready", func(c *gin.Context) {
		if deps.Ready != nil {
			if err := deps.Ready(); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status": "not_ready",
					"code":   "dependency_unavailable",
				})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	platformAuth := api.Group("/platform/auth")
	platformAuth.POST("/register", authLimiter.middleware("register"), identityHandler{service: deps.Identity}.register)
	platformAuth.POST("/login", authLimiter.middleware("login"), identityHandler{service: deps.Identity}.login)
	platformAuth.GET("/me", identityHandler{service: deps.Identity}.me)
	platformAuth.POST("/logout", identityHandler{service: deps.Identity}.logout)
	workspaceProjects := api.Group("/platform/workspaces/:workspaceId/projects")
	workspaceProjects.GET("", catalogHandler{service: deps.Catalog}.list)
	workspaceProjects.PUT("/:projectId", catalogHandler{service: deps.Catalog}.sync)
	workspaceProjects.GET("/:projectId/document", catalogHandler{service: deps.Catalog}.getDocument)
	workspaceProjects.PUT("/:projectId/document", catalogHandler{service: deps.Catalog}.saveDocument)
	workspaceProjects.POST("/:projectId/conflict-resolution", catalogHandler{service: deps.Catalog}.resolveConflict)
	workspaceProjects.POST("/:projectId/plans/:approvalId/approve", usageHandler{service: deps.Usage}.approve)
	workspaceProjects.POST("/:projectId/candidates/:candidateId/commit", usageHandler{service: deps.Usage}.commitCandidate)
	workspaceTeam := api.Group("/platform/workspaces/:workspaceId")
	workspaceTeam.GET("/members", teamHandler{service: deps.Team}.list)
	workspaceTeam.POST("/invitations", teamHandler{service: deps.Team}.invite)
	workspaceTeam.PATCH("/members/:accountId", teamHandler{service: deps.Team}.role)
	workspaceTeam.DELETE("/members/:accountId", teamHandler{service: deps.Team}.remove)
	workspaceTeam.GET("/audit", auditHandler{service: deps.Audit}.list)
	workspaceTeam.GET("/usage", usageHandler{service: deps.Usage}.summary)
	workspaceTeam.POST("/agent", agentLimiter.middleware("agent"), agentHandler{runner: deps.AgentRunner}.run)
	api.POST("/platform/invitations/:token/accept", teamHandler{service: deps.Team}.accept)
	runtimeProjects := api.Group("/runtime/projects/:projectId")
	runtimeProjects.PUT("/provision", runtimeHandler{service: deps.Runtime}.provision)
	runtimeProjects.GET("/inspect", runtimeHandler{service: deps.Runtime}.inspect)
	runtimeProjects.POST("/auth/register", runtimeHandler{service: deps.Runtime}.register)
	runtimeProjects.POST("/auth/login", runtimeHandler{service: deps.Runtime}.login)
	runtimeProjects.GET("/auth/me", runtimeHandler{service: deps.Runtime}.me)
	runtimeProjects.POST("/auth/logout", runtimeHandler{service: deps.Runtime}.logout)
	runtimeProjects.GET("/collections/:collection", runtimeHandler{service: deps.Runtime}.collection)
	runtimeProjects.POST("/collections/:collection", runtimeHandler{service: deps.Runtime}.collection)
	runtimeProjects.PATCH("/collections/:collection/:recordId", runtimeHandler{service: deps.Runtime}.record)
	runtimeProjects.DELETE("/collections/:collection/:recordId", runtimeHandler{service: deps.Runtime}.record)

	return router
}

func cors(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Vivatom-App-Key,X-Vivatom-Admin-Token")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
