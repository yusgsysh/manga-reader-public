package main

import (
	"log"

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
	log.Fatal(r.Run(":30080"))
}
