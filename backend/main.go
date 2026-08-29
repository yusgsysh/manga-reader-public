package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	cookieCfg := LoadCookieConfig()
	if !cookieCfg.IsValid() {
		log.Fatal("missing required cookies: set EHENTAI_COOKIE or EHENTAI_COOKIE_IPB_MEMBER_ID + EHENTAI_COOKIE_IPB_PASS_HASH")
	}

	client, err := CreateHTTPClient(cookieCfg)
	if err != nil {
		log.Fatal(err)
	}

	app := &App{Client: client}

	r := gin.Default()

	r.GET("/api/gallery/:id/:token", app.handleGetGallery)
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)
	r.GET("/api/search", app.handleSearch)
	r.GET("/api/page-image", app.handlePageImage)
	r.GET("/api/gallerys", app.handleGallerys)
	r.GET("/api/watched", app.handleWatched)
	r.GET("/api/popular", app.handlePopular)

	port := os.Getenv("EHENTAI_PORT")
	if port == "" {
		port = ":8080"
	} else if port[0] != ':' {
		port = ":" + port
	}
	log.Fatal(r.Run(port))
}
