package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/handler"
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
	cookieCfg := exhentai.LoadCookieConfig()
	if !cookieCfg.IsValid() {
		log.Fatal("missing required cookies: set EHENTAI_COOKIE or EHENTAI_COOKIE_IPB_MEMBER_ID + EHENTAI_COOKIE_IPB_PASS_HASH")
	}

	client, err := exhentai.CreateHTTPClient(cookieCfg)
	if err != nil {
		log.Fatal(err)
	}

	dbPath := os.Getenv("MANGA_READER_DB_PATH")
	if dbPath == "" {
		dbPath = "data/manga-reader.db"
	}

	db, err := database.NewDB(dbPath)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	defer db.Close()

	cfg := handler.Config{
		Client: client,
		DB:     db,
	}

	minioCfg := cache.LoadMinIOConfig()
	if minioCfg.IsValid() {
		minioCache, err := cache.NewMinIOCache(minioCfg)
		if err != nil {
			log.Fatalf("minio cache init failed: %v", err)
		}
		cfg.Cache = minioCache
		log.Println("cached-image and cached-thumbnail endpoints enabled")
	} else {
		log.Println("minio config not set, cached-image/cached-thumbnail endpoints will return 503")
	}

	srv := handler.New(cfg)

	r := gin.Default()
	r.Use(CORSMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	srv.RegisterRoutes(r)

	port := os.Getenv("EHENTAI_PORT")
	if port == "" {
		port = ":8080"
	} else if port[0] != ':' {
		port = ":" + port
	}
	log.Fatal(r.Run(port))
}
