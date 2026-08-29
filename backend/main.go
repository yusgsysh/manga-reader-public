package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func main() {
	cookieCfg := LoadCookieConfig()
	if !cookieCfg.IsValid() {
		log.Fatal("missing required cookies: set EHENTAI_COOKIE or EHENTAI_COOKIE_IPB_MEMBER_ID + EHENTAI_COOKIE_IPB_PASS_HASH")
	}

	client, err := CreateHTTPClient(cookieCfg)
	if err != nil {
		log.Fatal(err)
	}

	dbPath := os.Getenv("MANGA_READER_DB_PATH")
	if dbPath == "" {
		dbPath = "data/manga-reader.db"
	}

	db, err := NewDB(dbPath)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	defer db.Close()

	app := &App{Client: client, DB: db}

	r := gin.Default()

	r.Use(CORSMiddleware())

	r.GET("/api/gallery/:id/:token", app.handleGetGallery)
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)
	r.GET("/api/search", app.handleSearch)
	r.GET("/api/page-image", app.handlePageImage)
	r.GET("/api/gallerys", app.handleGallerys)
	r.GET("/api/watched", app.handleWatched)
	r.GET("/api/popular", app.handlePopular)

	r.GET("/api/bookshelf", app.handleBookshelfList)
	r.POST("/api/bookshelf/:id/:token", app.handleBookshelfAdd)
	r.DELETE("/api/bookshelf/:id/:token", app.handleBookshelfRemove)
	r.GET("/api/bookshelf/:id/:token/status", app.handleBookshelfStatus)
	r.GET("/api/progress/:id/:token", app.handleGetProgress)
	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)
	r.GET("/api/recently-read", app.handleRecentlyRead)

	thumbApp := &ThumbnailApp{Client: client}
	r.GET("/api/thumbnail", thumbApp.handleThumbnail)
	r.GET("/api/cached-thumbnail", thumbApp.handleCachedThumbnail)

	minioCfg := LoadMinIOConfig()
	if minioCfg.IsValid() {
		cache, err := NewMinIOCache(minioCfg)
		if err != nil {
			log.Fatalf("minio cache init failed: %v", err)
		}
		cachedApp := &CachedImageApp{Client: client, Cache: cache}
		r.GET("/api/cached-image", cachedApp.handleCachedImage)
		thumbApp.Cache = cache
		log.Println("cached-image and cached-thumbnail endpoints enabled")
	} else {
		log.Println("minio config not set, cached-image/cached-thumbnail endpoints will return 503")
	}

	port := os.Getenv("EHENTAI_PORT")
	if port == "" {
		port = ":8080"
	} else if port[0] != ':' {
		port = ":" + port
	}
	log.Fatal(r.Run(port))
}
