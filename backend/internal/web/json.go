package web

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const contentTypeJSON = "application/json; charset=utf-8"

// JSON writes obj as a JSON response using encoding/json/v2.
//
// It is the v2 counterpart of (*gin.Context).JSON, which still serializes
// through encoding/json (v1).
func JSON(c *gin.Context, code int, obj any) {
	data, err := json.Marshal(obj)
	if err != nil {
		// Never emit a partial document: report the error and fail the
		// response instead, mirroring gin's JSON renderer.
		_ = c.Error(err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(code, contentTypeJSON, data)
}

// AbortWithStatusJSON aborts the pending handlers and writes obj as JSON.
func AbortWithStatusJSON(c *gin.Context, code int, obj any) {
	c.Abort()
	JSON(c, code, obj)
}

// BindJSON decodes the request body into obj using encoding/json/v2.
//
// An empty body reports io.EOF, matching gin's binding behavior; a truncated
// body reports a decode error instead.
func BindJSON(c *gin.Context, obj any) error {
	if c.Request.Body == nil {
		return io.EOF
	}
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return io.EOF
	}
	return json.Unmarshal(data, obj)
}
