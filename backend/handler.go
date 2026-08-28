package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type App struct {
	Client *http.Client
}

func (a *App) handleGetGallery(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return
	}

	galleryURL := makeGalleryURL(id, token)

	html, err := makeRequest(a.Client, galleryURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai request failed: %v", err)})
		return
	}

	gallery, err := ParseGallery(html, id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("gallery parsing failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gallery)
}
