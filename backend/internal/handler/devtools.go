package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// upstreamRoutes are the routes that reach ExHentai. The simulated-outage
// middleware fails exactly these, leaving the local cache and database
// endpoints (/api/gallery-cache/*, /api/bookshelf*, /api/progress*,
// /api/recently-read, /api/image-cache/*) untouched.
var upstreamRoutes = map[string]struct{}{
	"/api/gallery/:id/:token":         {},
	"/api/gallery/:id/:token/details": {},
	"/api/gallery/:id/:token/pages":   {},
	"/api/galleries":                  {},
	"/api/search":                     {},
	"/api/watched":                    {},
	"/api/popular":                    {},
}

func isUpstreamRoute(fullPath string) bool {
	_, ok := upstreamRoutes[fullPath]
	return ok
}

// upstreamSimulationMiddleware returns 502 for upstream-backed endpoints while
// the dev outage switch is on. It is a no-op unless dev tools are enabled, so
// production (MANGA_READER_DEV_TOOLS unset) never registers it in effect.
func (s *Server) upstreamSimulationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.devTools || !s.simulateUpstreamDown.Load() {
			c.Next()
			return
		}
		if isUpstreamRoute(c.FullPath()) {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "simulated upstream outage"})
			return
		}
		c.Next()
	}
}

// handleDevUpstreamDownGet reports the current simulated-outage state.
func (s *Server) handleDevUpstreamDownGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"down": s.simulateUpstreamDown.Load()})
}

// handleDevUpstreamDownPut toggles the simulated-outage state at runtime.
func (s *Server) handleDevUpstreamDownPut(c *gin.Context) {
	var body struct {
		Down bool `json:"down"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `expected {"down": bool}`})
		return
	}
	s.simulateUpstreamDown.Store(body.Down)
	c.JSON(http.StatusOK, gin.H{"down": body.Down})
}
