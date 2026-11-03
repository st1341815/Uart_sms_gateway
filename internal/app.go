package internal

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"github.com/dushixiang/uart_sms_forwarder/internal/handler"
	"github.com/dushixiang/uart_sms_forwarder/internal/middleware"
	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/dushixiang/uart_sms_forwarder/internal/repo"
	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/dushixiang/uart_sms_forwarder/internal/version"
	"github.com/dushixiang/uart_sms_forwarder/web"
	"github.com/go-orz/orz"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Handlers 所有Handler的集合
type Handlers struct {
	Auth          *handler.AuthHandler
	Credential    *handler.CredentialHandler
	OpenAPI       *handler.OpenAPIHandler
	OpenAPIAdmin  *handler.OpenAPIAdminHandler
	Property      *handler.PropertyHandler
	TextMessage   *handler.TextMessageHandler
	Serial        *handler.SerialHandler
	ScheduledTask *handler.ScheduledTaskHandler
}

func Run(configPath string) {
	err := orz.Quick(configPath, setup)
	if err != nil {
		log.Fatal(err)
	}
}

func setup(app *orz.App) error {
	logger := app.Logger()
	db := app.GetDatabase()

	// 1. 数据库迁移
	if err := autoMigrate(db); err != nil {
		logger.Error("数据库迁移失败", zap.Error(err))
		return err
	}

	// 2. 读取应用配置
	var appConfig config.AppConfig
	_config := app.GetConfig()
	if _config != nil {
		if err := _config.App.Unmarshal(&appConfig); err != nil {
			logger.Error("读取配置失败", zap.Error(err))
			return err
		}
	}

	// 3. 设置默认值
	setDefaultConfig(&appConfig, logger)

	// 3.1 确保 JWT Secret 可用：空/占位/过短时生成一次并落盘复用。
	//     绝不能随机生成——它同时是 AK/SK 密文的派生源，换一次所有 Secret 全部失效。
	if err := ensureJWTSecret(&appConfig, logger); err != nil {
		logger.Error("初始化 JWT Secret 失败", zap.Error(err))
		return err
	}

	// 4. 初始化 Repository
	textMessageRepo := repo.NewTextMessageRepo(db)

	// 5. 初始化 Service
	propertyService := service.NewPropertyService(logger, db)
	notifier := service.NewNotifier(logger)
	textMessageService := service.NewTextMessageService(logger, textMessageRepo)

	// 初始化默认配置
	ctx := context.Background()
	if err := propertyService.InitializeDefaultConfigs(ctx); err != nil {
		logger.Error("初始化默认配置失败", zap.Error(err))
	}

	// 6. 初始化设备管理器（替代原来的单设备 SerialService）
	deviceManager := service.NewDeviceManager(
		logger,
		appConfig.Serial,
		textMessageService,
		notifier,
		propertyService,
	)

	// 初始化所有设备
	if err := deviceManager.Initialize(); err != nil {
		logger.Error("初始化设备管理器失败", zap.Error(err))
		return err
	}

	// 7. 初始化定时任务服务
	schedulerService := service.NewSchedulerService(
		logger,
		db,
		deviceManager,
		textMessageService,
	)
	deviceManager.SetScheduledTaskStatusUpdater(schedulerService.UpdateLastRunStatusByMsgId)

	// 8. 初始化口令服务（数据库存储，config.yaml 的 Users 仅作首次种子）
	credentialService, err := service.NewCredentialService(logger, db, appConfig.Users)
	if err != nil {
		logger.Error("初始化口令服务失败", zap.Error(err))
		return err
	}

	// 9. 初始化 OIDC 和 Account Service
	oidcService := service.NewOIDCService(logger, &appConfig)
	accountService := service.NewAccountService(logger, oidcService, &appConfig)
	accountService.SetCredentialService(credentialService)

	// 10. 初始化 Handler
	authHandler := handler.NewAuthHandler(logger, accountService)
	credentialHandler := handler.NewCredentialHandler(logger, accountService)

	// 11. 开放接口（AK/SK 签名）
	apiKeyService, err := service.NewAPIKeyService(logger, db, appConfig.JWT.Secret)
	if err != nil {
		logger.Error("初始化开放接口鉴权失败", zap.Error(err))
		return err
	}
	templateService := service.NewTemplateService(logger, db)
	openAPIService := service.NewOpenAPIService(logger, db, deviceManager, templateService)
	openAPIHandler := handler.NewOpenAPIHandler(logger, apiKeyService, openAPIService, templateService)
	openAPIAdminHandler := handler.NewOpenAPIAdminHandler(logger, apiKeyService, templateService, openAPIService)
	propertyHandler := handler.NewPropertyHandler(logger, propertyService, notifier)
	textMessageHandler := handler.NewTextMessageHandler(logger, textMessageService, textMessageRepo)
	serialHandler := handler.NewSerialHandler(logger, deviceManager) // 改为传入 deviceManager
	scheduledTaskHandler := handler.NewScheduledTaskHandler(logger, schedulerService)

	handlers := &Handlers{
		Auth:          authHandler,
		Credential:    credentialHandler,
		OpenAPI:       openAPIHandler,
		OpenAPIAdmin:  openAPIAdminHandler,
		Property:      propertyHandler,
		TextMessage:   textMessageHandler,
		Serial:        serialHandler,
		ScheduledTask: scheduledTaskHandler,
	}

	// 10. 设置 API 路由
	setupApi(app, handlers, &appConfig, logger)

	// 11. 启动后台服务
	background := context.Background()
	// 启动所有设备服务
	go deviceManager.StartAll()

	// 启动定时任务服务
	if err := schedulerService.Start(background); err != nil {
		logger.Error("启动定时任务服务失败", zap.Error(err))
	} else {
		logger.Info("定时任务服务启动成功")
	}

	// 记录启动信息
	deviceIDs := deviceManager.GetAllDeviceIDs()
	logger.Info("应用启动完成", 
		zap.Strings("devices", deviceIDs),
		zap.Int("device_count", len(deviceIDs)))

	return nil
}

// setDefaultConfig 设置默认配置
func setDefaultConfig(appConfig *config.AppConfig, logger *zap.Logger) {
	// JWT 默认值
	//
	// ⚠️ 这里刻意**不再**用随机 UUID 兜底：JWT Secret 同时是 AK/SK 密文的派生源
	// （service.NewAPIKeyService），每次重启换一个值会让库里所有 SecretEnc 永久解不开。
	// 空值由 ensureJWTSecret 生成一次并落到 ./data/.jwt_secret 复用。
	if appConfig.JWT.ExpiresHours == 0 {
		appConfig.JWT.ExpiresHours = 168 // 7天
	}
}

// autoMigrate 数据库迁移
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Property{},
		&models.TextMessage{},
		&models.ScheduledTask{},
		&models.UserCredential{},
		&models.APIKey{},
		&models.SMSTemplate{},
		&models.APISendLog{},
	)
}

// setupApi 设置API路由
func setupApi(app *orz.App, handlers *Handlers, appConfig *config.AppConfig, logger *zap.Logger) {
	e := app.GetEcho()

	// 请求体上限：本项目所有接口都是小 JSON（发信/模板/配置），
	// 64 KB 足够；不设限时大 body 会直接吃内存。
	e.Use(echomiddleware.BodyLimit("64K"))

	// 慢连接防护：只设读头部超时，避免影响正常的响应写出。
	if e.Server != nil {
		e.Server.ReadHeaderTimeout = 10 * time.Second
		e.Server.IdleTimeout = 60 * time.Second
	}

	e.Use(echomiddleware.StaticWithConfig(echomiddleware.StaticConfig{
		Skipper: func(c echo.Context) bool {
			// 不处理接口
			if strings.HasPrefix(c.Request().RequestURI, "/api") {
				return true
			}
			// ⚠️ 这里必须是 "/openapi/"，不能是 "/openapi"：
			// 前端有个 SPA 路由就叫 /openapi，若把裸路径也跳过静态中间件，
			// 它就拿不到 index.html 回退，直接 404（原 README 里的「已知问题」）。
			if strings.HasPrefix(c.Request().RequestURI, "/openapi/") {
				return true
			}
			if strings.HasPrefix(c.Request().RequestURI, "/health") {
				return true
			}
			return false
		},
		Index:      "index.html",
		HTML5:      true,
		Browse:     false,
		IgnoreBase: false,
		Filesystem: http.FS(web.Assets()),
	}))

	// 登录路由（不需要认证）
	e.POST("/api/login", handlers.Auth.Login)
	e.GET("/api/auth/config", handlers.Auth.GetAuthConfig)
	e.GET("/api/auth/oidc/url", handlers.Auth.GetOIDCAuthURL)
	e.POST("/api/auth/oidc/callback", handlers.Auth.OIDCCallback)

	// ===== 对外开放接口（AK/SK 签名认证，不走 JWT）=====
	openapi := e.Group("/openapi")
	openapi.POST("/v1/sms/send", handlers.OpenAPI.Send)
	openapi.GET("/v1/templates", handlers.OpenAPI.ListTemplates)

	// API 路由组（需要认证）
	api := e.Group("/api")
	api.Use(middleware.JWTMiddleware(appConfig.JWT.Secret, logger))

	// 当前用户 / 修改密码
	api.GET("/auth/me", handlers.Credential.Me)
	api.POST("/auth/password", handlers.Credential.ChangePassword)

	// 开放接口管理（Key / 模板 / 调用日志）
	api.GET("/openapi/keys", handlers.OpenAPIAdmin.ListKeys)
	api.POST("/openapi/keys", handlers.OpenAPIAdmin.CreateKey)
	api.PUT("/openapi/keys/:id", handlers.OpenAPIAdmin.UpdateKey)
	api.DELETE("/openapi/keys/:id", handlers.OpenAPIAdmin.DeleteKey)
	api.POST("/openapi/keys/:id/reset-secret", handlers.OpenAPIAdmin.ResetSecret)

	api.GET("/openapi/templates", handlers.OpenAPIAdmin.ListTemplates)
	api.POST("/openapi/templates", handlers.OpenAPIAdmin.CreateTemplate)
	api.PUT("/openapi/templates/:id", handlers.OpenAPIAdmin.UpdateTemplate)
	api.DELETE("/openapi/templates/:id", handlers.OpenAPIAdmin.DeleteTemplate)

	api.GET("/openapi/logs", handlers.OpenAPIAdmin.ListSendLogs)

	// Version
	api.GET("/version", func(c echo.Context) error {
		return c.JSON(http.StatusOK, echo.Map{
			"version": version.GetVersion(),
		})
	})

	// Property API
	api.GET("/properties/:id", handlers.Property.GetProperty)
	api.PUT("/properties/:id", handlers.Property.SetProperty)
	api.POST("/notifications/:type/test", handlers.Property.TestNotificationChannel)

	// TextMessage API
	api.GET("/messages/stats", handlers.TextMessage.GetStats)
	api.GET("/messages/conversations", handlers.TextMessage.GetConversations)
	api.GET("/messages/conversations/:peer/messages", handlers.TextMessage.GetConversationMessages)
	api.DELETE("/messages/conversations/:peer", handlers.TextMessage.DeleteConversation)
	api.DELETE("/messages/:id", handlers.TextMessage.Delete)
	api.DELETE("/messages", handlers.TextMessage.Clear)

	// Device API (新增)
	api.GET("/devices", handlers.Serial.ListDevices)
	api.PUT("/devices/:id/name", handlers.Serial.RenameDevice) // 修改 SIM 卡显示名称
	api.GET("/devices/:id/status", handlers.Serial.GetDeviceStatus)
	api.POST("/devices/:id/flymode", handlers.Serial.SetFlymode)
	api.POST("/devices/:id/reboot", handlers.Serial.RebootMcu)

	// Serial API (兼容旧API，支持 device_id 参数)
	api.POST("/serial/sms", handlers.Serial.SendSMS)
	api.GET("/serial/status", handlers.Serial.GetStatus) // 返回所有设备状态或单个（通过 device_id 参数）
	api.POST("/serial/flymode", handlers.Serial.SetFlymode)
	api.POST("/serial/reboot", handlers.Serial.RebootMcu)

	// ScheduledTask API (RESTful)
	api.GET("/scheduled-tasks", handlers.ScheduledTask.List)
	api.GET("/scheduled-tasks/:id", handlers.ScheduledTask.Get)
	api.POST("/scheduled-tasks", handlers.ScheduledTask.Create)
	api.PUT("/scheduled-tasks/:id", handlers.ScheduledTask.Update)
	api.DELETE("/scheduled-tasks/:id", handlers.ScheduledTask.Delete)
	api.POST("/scheduled-tasks/:id/trigger", handlers.ScheduledTask.Trigger)

	// 健康检查接口（无需认证）
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(200, map[string]string{
			"status": "ok",
		})
	})
}