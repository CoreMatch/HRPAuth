package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/controllers"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/redis"
)

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", config.AppConfig.Server.CORSOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func main() {
	// Initialize startup controller to check/create config file
	startupCtrl := controllers.NewStartupController()
	if err := startupCtrl.InitializeConfig(); err != nil {
		log.Fatalf("Failed to initialize config: %v", err)
	}

	config.Load()
	database.Init()
	redis.Init()

	cleanupCtrl := controllers.NewTokenCleanupController()
	cleanupCtrl.Start(1 * time.Hour)

	r := gin.Default()

	r.Use(CORSMiddleware())

	authCtrl := controllers.NewAuthController()
	userInfoCtrl := controllers.NewUserInfoController()
	userProfileCtrl := controllers.NewUserProfileController()
	totpCtrl := controllers.NewTOTPController()
	emailCtrl := controllers.NewEmailVerificationController()
	keygenCtrl := controllers.NewKeyGenController()
	textureCtrl := controllers.NewTextureController()
	yggdrasilCtrl := controllers.NewYggdrasilController()
	captchaCtrl := controllers.NewCaptchaController()

	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "online",
			"backend": gin.H{
				"name":        config.AppConfig.Site.Name,
				"url":         config.AppConfig.Callback.URL,
				"version":     config.AppConfig.Site.Version,
				"go_version":  "go1.26",
				"server_time": time.Now().Format("2006-01-02 15:04:05"),
			},
			"message": "HRPAuth Backend is running.",
		})
	})

	api := r.Group("")
	{
		api.POST("/login", authCtrl.Login)
		api.POST("/register", authCtrl.Register)
		api.GET("/logout", authCtrl.Logout)
		api.POST("/user", userInfoCtrl.GetUser)
		api.POST("/user/mojang-bind-enable", userInfoCtrl.EnableMojangBind)

		api.POST("/email-verification", emailCtrl.Handle)

		api.GET("/totpgen", totpCtrl.Generate)
		api.POST("/totp/setup", totpCtrl.SetupTOTP)
		api.POST("/totp/verify", totpCtrl.VerifyTOTP)
		api.POST("/totp/hasbeenenabled", totpCtrl.HasBeenEnabled)

		api.POST("/change-username", userProfileCtrl.ChangeUsername)
		api.POST("/change-profile-name", userProfileCtrl.ChangeProfileName)

		api.POST("/generate-key", keygenCtrl.Generate)

		api.POST("/texture/upload", textureCtrl.UploadTexture)
		api.POST("/texture/delete", textureCtrl.DeleteTexture)

		api.POST("/captcha", captchaCtrl.Generate)
		api.GET("/captcha/enabled", captchaCtrl.Status)
		api.GET("/captcha/image/:token", captchaCtrl.Image)
		api.POST("/texture/get", textureCtrl.GetTexture)
	}

	yggdrasil := r.Group("")
	{
		yggdrasil.GET("/", yggdrasilCtrl.Meta)

		yggdrasil.POST("/authserver/authenticate", yggdrasilCtrl.Authenticate)
		yggdrasil.POST("/authserver/refresh", yggdrasilCtrl.Refresh)
		yggdrasil.POST("/authserver/validate", yggdrasilCtrl.Validate)
		yggdrasil.POST("/authserver/invalidate", yggdrasilCtrl.Invalidate)
		yggdrasil.POST("/authserver/signout", yggdrasilCtrl.Signout)

		yggdrasil.POST("/sessionserver/session/minecraft/join", yggdrasilCtrl.Join)
		yggdrasil.GET("/sessionserver/session/minecraft/hasJoined", yggdrasilCtrl.HasJoined)
		yggdrasil.GET("/sessionserver/session/minecraft/hasjoined", yggdrasilCtrl.HasJoined)
		yggdrasil.GET("/sessionserver/session/minecraft/profile/:uuid", yggdrasilCtrl.ProfileQuery)

		yggdrasil.POST("/api/profiles/minecraft", yggdrasilCtrl.BatchProfiles)

		yggdrasil.PUT("/api/user/profile/:uuid/:textureType", yggdrasilCtrl.UploadTexture)
		yggdrasil.DELETE("/api/user/profile/:uuid/:textureType", yggdrasilCtrl.DeleteTexture)

		yggdrasil.GET("/textures/:hash", yggdrasilCtrl.DownloadTexture)
	}

	r.NoRoute(func(c *gin.Context) {
		path := strings.ToLower(c.Request.URL.Path)
		if strings.Contains(path, "authserver") ||
			strings.Contains(path, "sessionserver") ||
			strings.Contains(path, "/api/") ||
			strings.Contains(path, "/textures/") {
			c.JSON(http.StatusNotFound, gin.H{
				"error":        "Not Found",
				"errorMessage": "The requested endpoint does not exist.",
				"cause":        nil,
			})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Not Found",
		})
	})

	r.Run(config.AppConfig.Server.Port)
}
