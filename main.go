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
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := config.AppConfig.Server.CORSOrigin
		if origin == "*" {
			reqOrigin := c.Request.Header.Get("Origin")
			if reqOrigin != "" {
				origin = reqOrigin
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
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

	if err := startupCtrl.EnsureMigrations(); err != nil {
		log.Fatalf("Failed to ensure database migrations: %v", err)
	}

	redis.Init()

	cleanupCtrl := controllers.NewTokenCleanupController()
	cleanupCtrl.Start(1 * time.Hour)

	botCleanupCtrl := controllers.NewBotUserCleanupController()
	botCleanupCtrl.Start(24 * time.Hour)

	sessionCleanupCtrl := controllers.NewSessionCleanupController()
	sessionCleanupCtrl.Start(24 * time.Hour)

	r := gin.Default()

	r.Use(CORSMiddleware())
	r.Use(controllers.RequestIDMiddleware())

	presenceRegistry := controllers.NewPresenceRegistry()
	routeRegistry := controllers.NewRouteRegistry()
	relayRegistry := controllers.NewRelayRegistry()

	presenceCtrl := controllers.NewPresenceController(presenceRegistry)
	routeCtrl := controllers.NewRouteController(routeRegistry, presenceRegistry)
	relayCtrl := controllers.NewRelayController(relayRegistry, presenceRegistry)

	r.Use(controllers.RelayMiddleware(relayRegistry))
	r.Use(controllers.OrchestrationMiddleware(routeRegistry))

	authCtrl := controllers.NewAuthController()
	userInfoCtrl := controllers.NewUserInfoController()
	userProfileCtrl := controllers.NewUserProfileController()
	totpCtrl := controllers.NewTOTPController()
	emailCtrl := controllers.NewEmailVerificationController()
	keygenCtrl := controllers.NewKeyGenController()
	textureCtrl := controllers.NewTextureController()
	yggdrasilCtrl := controllers.NewYggdrasilController()
	captchaCtrl := controllers.NewCaptchaController()
	oauth2Ctrl := controllers.NewOAuth2Controller()

	if err := services.NewOAuth2Service().EnsureBuiltInClients(); err != nil {
		log.Fatalf("Failed to ensure OAuth2 built-in clients: %v", err)
	}

	r.GET("/status", func(c *gin.Context) {
		controllers := gin.H{
			"status": "online",
			"backend": gin.H{
				"name":        config.AppConfig.Site.Name,
				"url":         config.AppConfig.Callback.URL,
				"version":     config.AppConfig.Site.Version,
				"go_version":  "go1.26",
				"server_time": time.Now().Format("2006-01-02 15:04:05"),
			},
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "HRPAuth Backend is running.",
			"data":    controllers,
			"meta": gin.H{
				"request_id": c.GetString("request_id"),
			},
		})
	})

	api := r.Group("")
	{
		api.GET("/oauth/authorize", oauth2Ctrl.Authorize)
		api.POST("/oauth/login-ticket", oauth2Ctrl.LoginTicket)
		api.POST("/oauth/authorize/decision", oauth2Ctrl.AuthorizeDecision)
		api.POST("/oauth/token", oauth2Ctrl.Token)
		api.POST("/oauth/revoke", oauth2Ctrl.Revoke)

		api.POST("/login", authCtrl.Login)
		api.POST("/loginbymt", authCtrl.LoginByMT)
		api.POST("/register", authCtrl.Register)
		api.GET("/logout", authCtrl.Logout)
		api.POST("/user", userInfoCtrl.GetUser)
		api.POST("/user/declare-email", userInfoCtrl.DeclareEmail)
		api.POST("/user/mojang-bind-enable", userInfoCtrl.EnableMojangBind)

		api.POST("/email-verification", emailCtrl.Handle)

		api.GET("/totpgen", totpCtrl.Generate)
		api.POST("/totp/setup", totpCtrl.SetupTOTP)
		api.POST("/totp/verify", totpCtrl.VerifyTOTP)
		api.POST("/totp/toggle", totpCtrl.Toggle2FA)
		api.POST("/totp/hasbeenenabled", totpCtrl.HasBeenEnabled)

		api.POST("/change-username", userProfileCtrl.ChangeUsername)
		api.POST("/change-profile-name", userProfileCtrl.ChangeProfileName)

		api.POST("/generate-key", keygenCtrl.Generate)

		api.POST("/texture/upload", textureCtrl.UploadTexture)
		api.POST("/texture/delete", textureCtrl.DeleteTexture)
		api.POST("/texture/rewrite-callback", textureCtrl.RewriteTextureCallbacks)

		api.POST("/captcha", captchaCtrl.Generate)
		api.GET("/captcha/enabled", captchaCtrl.Status)
		api.GET("/captcha/image/:token", captchaCtrl.Image)
		api.POST("/texture/get", textureCtrl.GetTexture)

		api.POST("/services/presence", presenceCtrl.Bonjour)
		api.POST("/services/route", routeCtrl.Register)
		api.POST("/services/relay", relayCtrl.Register)
		api.DELETE("/services/relay", relayCtrl.Delete)
		api.GET("/services/relay", relayCtrl.List)
		api.GET("/services/list", presenceCtrl.ListFrontendServices)
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
			"code":    "route_not_found",
			"error":   "route_not_found",
			"meta": gin.H{
				"request_id": c.GetString("request_id"),
			},
		})
	})

	log.Printf("server listening on %s", config.AppConfig.Server.Port)

	r.Run(config.AppConfig.Server.Port)
}
