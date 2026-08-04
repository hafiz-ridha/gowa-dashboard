package cmd

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/aireply"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	aireplyUC "github.com/aldinokemal/go-whatsapp-web-multidevice/usecase/aireply"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.mau.fi/whatsmeow"
)

var (
	EmbedIndex embed.FS
	EmbedViews embed.FS

	// Whatsapp
	whatsappCli *whatsmeow.Client

	// Chat Storage
	chatStorageDB   *sql.DB
	chatStorageRepo domainChatStorage.IChatStorageRepository

	// Usecase
	appUsecase        domainApp.IAppUsecase
	chatUsecase       domainChat.IChatUsecase
	sendUsecase       domainSend.ISendUsecase
	userUsecase       domainUser.IUserUsecase
	messageUsecase    domainMessage.IMessageUsecase
	groupUsecase      domainGroup.IGroupUsecase
	newsletterUsecase domainNewsletter.INewsletterUsecase
	deviceUsecase     domainDevice.IDeviceUsecase

	// AI Reply service (nil when feature gate is off)
	aiReplyService *aireplyUC.Service
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Short: "Send free whatsapp API",
	Long: `This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice, 
you can send whatsapp over http api but your whatsapp account have to be multi device version`,
}

func init() {
	// Load environment variables first
	utils.LoadConfig(".")

	time.Local = time.UTC

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Initialize flags first, before any subcommands are added
	initFlags()

	// Then initialize other components
	cobra.OnInitialize(initEnvConfig, initApp)
}

// initEnvConfig loads configuration from environment variables
func initEnvConfig() {
	fmt.Println(viper.AllSettings())
	// Application settings
	if envPort := viper.GetString("app_port"); envPort != "" {
		config.AppPort = envPort
	}
	if envHost := viper.GetString("app_host"); envHost != "" {
		config.AppHost = envHost
	}
	if envDebug := viper.GetBool("app_debug"); envDebug {
		config.AppDebug = envDebug
	}
	if envOs := viper.GetString("app_os"); envOs != "" {
		config.AppOs = envOs
	}
	if envBasicAuth := viper.GetString("app_basic_auth"); envBasicAuth != "" {
		credential := strings.Split(envBasicAuth, ",")
		config.AppBasicAuthCredential = credential
	}
	if envBasePath := viper.GetString("app_base_path"); envBasePath != "" {
		config.AppBasePath = envBasePath
	}
	if envTrustedProxies := viper.GetString("app_trusted_proxies"); envTrustedProxies != "" {
		proxies := strings.Split(envTrustedProxies, ",")
		config.AppTrustedProxies = proxies
	}

	// Database settings
	if envDBURI := viper.GetString("db_uri"); envDBURI != "" {
		config.DBURI = envDBURI
	}
	if envDBKEYSURI := viper.GetString("db_keys_uri"); envDBKEYSURI != "" {
		config.DBKeysURI = envDBKEYSURI
	}

	// WhatsApp settings
	if envAutoReply := viper.GetString("whatsapp_auto_reply"); envAutoReply != "" {
		config.WhatsappAutoReplyMessage = envAutoReply
	}
	if viper.IsSet("whatsapp_auto_mark_read") {
		config.WhatsappAutoMarkRead = viper.GetBool("whatsapp_auto_mark_read")
	}
	if viper.IsSet("whatsapp_auto_download_media") {
		config.WhatsappAutoDownloadMedia = viper.GetBool("whatsapp_auto_download_media")
	}
	if envWebhook := viper.GetString("whatsapp_webhook"); envWebhook != "" {
		webhook := strings.Split(envWebhook, ",")
		config.WhatsappWebhook = webhook
	}
	if envWebhookSecret := viper.GetString("whatsapp_webhook_secret"); envWebhookSecret != "" {
		config.WhatsappWebhookSecret = envWebhookSecret
	}
	if viper.IsSet("whatsapp_webhook_insecure_skip_verify") {
		config.WhatsappWebhookInsecureSkipVerify = viper.GetBool("whatsapp_webhook_insecure_skip_verify")
	}
	if envWebhookEvents := viper.GetString("whatsapp_webhook_events"); envWebhookEvents != "" {
		events := strings.Split(envWebhookEvents, ",")
		config.WhatsappWebhookEvents = events
	}
	if viper.IsSet("whatsapp_account_validation") {
		config.WhatsappAccountValidation = viper.GetBool("whatsapp_account_validation")
	}
	if viper.IsSet("whatsapp_auto_reject_call") {
		config.WhatsappAutoRejectCall = viper.GetBool("whatsapp_auto_reject_call")
	}
	if envPresenceOnConnect := viper.GetString("whatsapp_presence_on_connect"); envPresenceOnConnect != "" {
		config.WhatsappPresenceOnConnect = envPresenceOnConnect
	}

	// Chatwoot settings
	if viper.IsSet("chatwoot_enabled") {
		config.ChatwootEnabled = viper.GetBool("chatwoot_enabled")
	}
	if envChatwootURL := viper.GetString("chatwoot_url"); envChatwootURL != "" {
		config.ChatwootURL = envChatwootURL
	}
	if envChatwootAPIToken := viper.GetString("chatwoot_api_token"); envChatwootAPIToken != "" {
		config.ChatwootAPIToken = envChatwootAPIToken
	}
	if viper.IsSet("chatwoot_account_id") {
		config.ChatwootAccountID = viper.GetInt("chatwoot_account_id")
	}
	if viper.IsSet("chatwoot_inbox_id") {
		config.ChatwootInboxID = viper.GetInt("chatwoot_inbox_id")
	}
	if envChatwootDeviceID := viper.GetString("chatwoot_device_id"); envChatwootDeviceID != "" {
		config.ChatwootDeviceID = envChatwootDeviceID
	}
	// Chatwoot History Sync settings
	if viper.IsSet("chatwoot_import_messages") {
		config.ChatwootImportMessages = viper.GetBool("chatwoot_import_messages")
	}
	if viper.IsSet("chatwoot_days_limit_import_messages") {
		config.ChatwootDaysLimitImportMessages = viper.GetInt("chatwoot_days_limit_import_messages")
	}

	// AI Auto-Reply settings
	if viper.IsSet("ai_reply_enabled") {
		config.AIReplyEnabled = viper.GetBool("ai_reply_enabled")
	}
	if envAIKey := viper.GetString("ai_encryption_key"); envAIKey != "" {
		config.AIEncryptionKey = envAIKey
	}
	if viper.IsSet("ai_max_kb_file_size") {
		config.AIMaxKBFileSize = viper.GetInt64("ai_max_kb_file_size")
	}
	if viper.IsSet("ai_request_timeout_sec") {
		config.AIRequestTimeoutSec = viper.GetInt("ai_request_timeout_sec")
	}
	if viper.IsSet("ai_rate_limit_seconds") {
		config.AIRateLimitSeconds = viper.GetInt("ai_rate_limit_seconds")
	}
	if viper.IsSet("ai_vector_dimension") {
		config.AIVectorDimension = viper.GetInt("ai_vector_dimension")
	}
}

func initFlags() {
	// Application flags
	rootCmd.PersistentFlags().StringVarP(
		&config.AppPort,
		"port", "p",
		config.AppPort,
		"change port number with --port <number> | example: --port=8080",
	)

	rootCmd.PersistentFlags().StringVarP(
		&config.AppHost,
		"host", "H",
		config.AppHost,
		`host to bind the server --host <string> | example: --host="127.0.0.1"`,
	)

	rootCmd.PersistentFlags().BoolVarP(
		&config.AppDebug,
		"debug", "d",
		config.AppDebug,
		"hide or displaying log with --debug <true/false> | example: --debug=true",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppOs,
		"os", "",
		config.AppOs,
		`os name --os <string> | example: --os="Chrome"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.AppBasicAuthCredential,
		"basic-auth", "b",
		config.AppBasicAuthCredential,
		"basic auth credential | -b=yourUsername:yourPassword",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppBasePath,
		"base-path", "",
		config.AppBasePath,
		`base path for subpath deployment --base-path <string> | example: --base-path="/gowa"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.AppTrustedProxies,
		"trusted-proxies", "",
		config.AppTrustedProxies,
		`trusted proxy IP ranges for reverse proxy deployments --trusted-proxies <string> | example: --trusted-proxies="0.0.0.0/0" or --trusted-proxies="10.0.0.0/8,172.16.0.0/12"`,
	)

	// Database flags
	rootCmd.PersistentFlags().StringVarP(
		&config.DBURI,
		"db-uri", "",
		config.DBURI,
		`the database uri to store the connection data database uri (by default, we'll use sqlite3 under storages/whatsapp.db). database uri --db-uri <string> | example: --db-uri="file:storages/whatsapp.db?_foreign_keys=on or postgres://user:password@localhost:5432/whatsapp"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.DBKeysURI,
		"db-keys-uri", "",
		config.DBKeysURI,
		`the database uri to store the keys database uri (by default, we'll use the same database uri). database uri --db-keys-uri <string> | example: --db-keys-uri="file::memory:?cache=shared&_foreign_keys=on"`,
	)

	// WhatsApp flags
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappAutoReplyMessage,
		"autoreply", "",
		config.WhatsappAutoReplyMessage,
		`auto reply when received message --autoreply <string> | example: --autoreply="Don't reply this message"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoMarkRead,
		"auto-mark-read", "",
		config.WhatsappAutoMarkRead,
		`auto mark incoming messages as read --auto-mark-read <true/false> | example: --auto-mark-read=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoDownloadMedia,
		"auto-download-media", "",
		config.WhatsappAutoDownloadMedia,
		`auto download media from incoming messages --auto-download-media <true/false> | example: --auto-download-media=false`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.WhatsappWebhook,
		"webhook", "w",
		config.WhatsappWebhook,
		`forward event to webhook --webhook <string> | example: --webhook="https://yourcallback.com/callback"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappWebhookSecret,
		"webhook-secret", "",
		config.WhatsappWebhookSecret,
		`secure webhook request --webhook-secret <string> | example: --webhook-secret="super-secret-key"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappWebhookInsecureSkipVerify,
		"webhook-insecure-skip-verify", "",
		config.WhatsappWebhookInsecureSkipVerify,
		`skip TLS certificate verification for webhooks (INSECURE - use only for development/self-signed certs) --webhook-insecure-skip-verify <true/false> | example: --webhook-insecure-skip-verify=true`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.WhatsappWebhookEvents,
		"webhook-events", "",
		config.WhatsappWebhookEvents,
		`whitelist of events to forward to webhook (empty = all events) --webhook-events <string> | example: --webhook-events="message,message.ack,group.participants"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAccountValidation,
		"account-validation", "",
		config.WhatsappAccountValidation,
		`enable or disable account validation --account-validation <true/false> | example: --account-validation=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoRejectCall,
		"auto-reject-call", "",
		config.WhatsappAutoRejectCall,
		`auto reject incoming calls --auto-reject-call <true/false> | example: --auto-reject-call=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappPresenceOnConnect,
		"presence-on-connect", "",
		config.WhatsappPresenceOnConnect,
		`presence to send on connect: "available", "unavailable", or "none" --presence-on-connect <string> | example: --presence-on-connect="unavailable"`,
	)

	// Chatwoot flags
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootEnabled,
		"chatwoot-enabled", "",
		config.ChatwootEnabled,
		`enable Chatwoot integration --chatwoot-enabled <true/false> | example: --chatwoot-enabled=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootDeviceID,
		"chatwoot-device-id", "",
		config.ChatwootDeviceID,
		`device ID for Chatwoot outbound messages --chatwoot-device-id <string> | example: --chatwoot-device-id="my-device"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootImportMessages,
		"chatwoot-import-messages", "",
		config.ChatwootImportMessages,
		`enable message history import to Chatwoot --chatwoot-import-messages <true/false> | example: --chatwoot-import-messages=true`,
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.ChatwootDaysLimitImportMessages,
		"chatwoot-days-limit-import-messages", "",
		config.ChatwootDaysLimitImportMessages,
		`days of message history to import to Chatwoot --chatwoot-days-limit-import-messages <int> | example: --chatwoot-days-limit-import-messages=7`,
	)

	// AI Auto-Reply flags
	rootCmd.PersistentFlags().BoolVarP(
		&config.AIReplyEnabled,
		"ai-reply-enabled", "",
		config.AIReplyEnabled,
		`enable AI auto-reply with RAG knowledgebase --ai-reply-enabled <true/false> | example: --ai-reply-enabled=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AIEncryptionKey,
		"ai-encryption-key", "",
		config.AIEncryptionKey,
		`hex-encoded 32-byte key for AES-GCM encryption of stored API keys (required when ai-reply-enabled) --ai-encryption-key <string>`,
	)
	rootCmd.PersistentFlags().Int64VarP(
		&config.AIMaxKBFileSize,
		"ai-max-kb-file-size", "",
		config.AIMaxKBFileSize,
		`max knowledgebase upload size in bytes (default 10MB) --ai-max-kb-file-size <int>`,
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.AIRequestTimeoutSec,
		"ai-request-timeout-sec", "",
		config.AIRequestTimeoutSec,
		`per-call timeout for LLM/embeddings requests in seconds --ai-request-timeout-sec <int>`,
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.AIRateLimitSeconds,
		"ai-rate-limit-seconds", "",
		config.AIRateLimitSeconds,
		`min interval (seconds) between AI replies per chat --ai-rate-limit-seconds <int>`,
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.AIVectorDimension,
		"ai-vector-dimension", "",
		config.AIVectorDimension,
		`embedding vector dimension (default 1536 for text-embedding-3-small) --ai-vector-dimension <int>`,
	)
}

func initChatStorage() (*sql.DB, error) {
	connStr := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000", config.ChatStorageURI)
	if config.ChatStorageEnableForeignKeys {
		connStr += "&_foreign_keys=on"
	}

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func initApp() {
	if config.AppDebug {
		config.WhatsappLogLevel = "DEBUG"
		logrus.SetLevel(logrus.DebugLevel)
	}

	//preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems, config.PathStorages, config.PathMedia)
	if err != nil {
		logrus.Errorln(err)
	}

	ctx := context.Background()

	chatStorageDB, err = initChatStorage()
	if err != nil {
		// Terminate the application if chat storage fails to initialize to avoid nil pointer panics later.
		logrus.Fatalf("failed to initialize chat storage: %v", err)
	}

	chatStorageRepo = chatstorage.NewStorageRepository(chatStorageDB)
	chatStorageRepo.InitializeSchema()

	whatsappDB := whatsapp.InitWaDB(ctx, config.DBURI)
	var keysDB *sqlstore.Container
	if config.DBKeysURI != "" {
		keysDB = whatsapp.InitWaDB(ctx, config.DBKeysURI)
	}

	whatsappCli = whatsapp.InitWaCLI(ctx, whatsappDB, keysDB, chatStorageRepo)

	// Initialize device manager and usecase for multi-device support
	dm := whatsapp.GetDeviceManager()
	if dm != nil {
		_ = dm.LoadExistingDevices(ctx)
	}

	// Usecase
	appUsecase = usecase.NewAppService(chatStorageRepo, dm)
	chatUsecase = usecase.NewChatService(chatStorageRepo)
	sendUsecase = usecase.NewSendService(appUsecase, chatStorageRepo)
	userUsecase = usecase.NewUserService(chatStorageRepo)
	messageUsecase = usecase.NewMessageService(chatStorageRepo)
	groupUsecase = usecase.NewGroupService()
	newsletterUsecase = usecase.NewNewsletterService()
	deviceUsecase = usecase.NewDeviceService(dm)

	// AI Reply: feature-gated. The vector store is initialised lazily on
	// first KB upload (so deployments that never enable AI don't pay the
	// sqlite-vec extension probe cost).
	if config.AIReplyEnabled {
		aiRepo := aireply.NewRepository(chatStorageDB)
		vecStore := aireply.NewVecStore(chatStorageDB)
		if err := vecStore.Init(config.AIVectorDimension); err != nil {
			logrus.Warnf("AI Reply: sqlite-vec init failed (%v); feature will run without vector search until fixed", err)
		}
		aiReplyService = aireplyUC.NewService(aiRepo, vecStore, chatStorageRepo)
		aireplyUC.SetPauseStore(aiRepo)
		if err := aireplyUC.LoadPersistedPause(ctx); err != nil {
			logrus.Warnf("AI Reply: failed to load persisted pause state (%v); starting unpaused", err)
		}
		whatsapp.RegisterAIReplyHandler(aiReplyService)
		logrus.Info("AI Reply feature enabled")
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(embedIndex embed.FS, embedViews embed.FS) {
	EmbedIndex = embedIndex
	EmbedViews = embedViews
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
