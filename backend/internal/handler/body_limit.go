package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// limitRequestBody bounds the request body before it is decoded. JSON
// handlers that do not carry large payloads (sync pushes and prefill install
// their own, larger limit) use this so a single request cannot materialise an
// unbounded body in memory before validation rejects it.
func limitRequestBody(c *gin.Context, max int64) {
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
	}
}
