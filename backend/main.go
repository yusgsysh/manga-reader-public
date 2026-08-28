package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	cookies, err := parseCookieFile("cookie.json")
	if err != nil {
		log.Fatal(err)
	}

	client, err := createClient(cookies)
	if err != nil {
		log.Fatal(err)
	}

	app := &App{Client: client}

	r := gin.Default()
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)
	log.Fatal(r.Run(":8080"))
}
