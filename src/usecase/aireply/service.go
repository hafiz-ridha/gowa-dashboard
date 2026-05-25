// Package aireply implements the AI-powered auto-reply orchestrator. It
// glues together: per-device config, RAG retrieval, provider abstraction,
// guardrails, rate limiting, audit logging, and message dispatch.
package aireply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/aireply"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	infraAI "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/aireply"
)

// SendFn dispatches an outbound text reply. We keep it as a function literal
// type (not a named type) so it stays structurally compatible with the
// matching callback in infrastructure/whatsapp without forcing that package
// to import this one.
type SendFn = func(ctx context.Context, recipientJID, text string) (msgID string, ts time.Time, err error)

// PresenceFn updates the chat presence ("composing" → typing indicator on,
// "paused" → off). Optional; pass nil to skip the indicator.
type PresenceFn = func(state string)

// Service orchestrates the AI reply flow.
type Service struct {
	Repo         *infraAI.Repository
	Vec          *infraAI.VecStore
	ChatStorage  domainChatStorage.IChatStorageRepository
	RateLimiter  *RateLimiter
}

// NewService constructs an orchestrator.
func NewService(repo *infraAI.Repository, vec *infraAI.VecStore, chatStorage domainChatStorage.IChatStorageRepository) *Service {
	return &Service{
		Repo:        repo,
		Vec:         vec,
		ChatStorage: chatStorage,
		RateLimiter: NewRateLimiter(time.Duration(config.AIRateLimitSeconds) * time.Second),
	}
}

// HandleIncoming is the inbound entry point invoked from the whatsapp event
// handler. Returns true when AI took ownership of this message (a reply was
// sent OR the message was deliberately swallowed as out-of-scope / rate
// limited), so the caller can skip the static auto-reply.
func (s *Service) HandleIncoming(ctx context.Context, deviceID, chatJID, senderJID, text string, send SendFn, presence PresenceFn) bool {
	if !config.AIReplyEnabled || deviceID == "" || strings.TrimSpace(text) == "" || send == nil {
		return false
	}

	// Global pause: claim ownership to suppress BOTH AI and static auto-reply.
	// Useful for temporary silence (mis. user sedang meeting / liburan).
	if IsPaused() {
		return true
	}

	setting, err := s.Repo.GetChatSetting(ctx, deviceID, chatJID)
	if err != nil {
		logrus.Warnf("ai-reply: get chat setting: %v", err)
		return false
	}
	if setting == nil {
		// No opt-in row — let static WHATSAPP_AUTO_REPLY (if configured) fire.
		return false
	}
	if !setting.Enabled {
		// Explicit opt-out: user added a chat-toggle row and turned it OFF.
		// Treat as "deliberately swallowed" so the static auto-reply doesn't
		// fire either — otherwise disabling AI for a chat would silently
		// fall back to "Auto reply message", confusing user intent.
		return true
	}

	if !s.RateLimiter.Allow(deviceID + "|" + chatJID) {
		_ = s.Repo.InsertLog(ctx, &domain.ReplyLog{
			DeviceID: deviceID, ChatJID: chatJID, Query: text, Status: domain.LogStatusRateLimited,
		})
		return true // ownership claimed; static auto-reply must not fire
	}

	cfg, apiKeyEnc, embedAPIKeyEnc, err := s.Repo.GetWithSecrets(ctx, deviceID)
	if err != nil {
		logrus.Warnf("ai-reply: load config: %v", err)
		return false
	}
	if cfg == nil {
		return false
	}

	// All gates passed — light up the typing indicator. Refresh every 10s
	// because WhatsApp expires "composing" after ~25s. Stops automatically
	// when stopTyping is closed (in defer) or when "paused" is sent inside
	// processAndReply right before the actual send.
	stopTyping := make(chan struct{})
	if presence != nil {
		presence("composing")
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopTyping:
					return
				case <-ticker.C:
					presence("composing")
				}
			}
		}()
		defer func() {
			close(stopTyping)
			presence("paused")
		}()
	}

	start := time.Now()
	if err := s.processAndReply(ctx, cfg, apiKeyEnc, embedAPIKeyEnc, deviceID, chatJID, senderJID, text, send); err != nil {
		// Errors are logged but never surfaced to the user — per NFR-5.
		logrus.Warnf("ai-reply: processAndReply: %v", err)
		_ = s.Repo.InsertLog(ctx, &domain.ReplyLog{
			DeviceID: deviceID, ChatJID: chatJID, Query: text,
			Status: domain.LogStatusError, ErrorMessage: err.Error(),
			LatencyMs: int(time.Since(start).Milliseconds()),
		})
		// Still return true to skip the static fallback — user opted into AI
		// for this chat, and the static auto-reply is not a meaningful
		// substitute for a broken AI config.
		return true
	}
	return true
}

func (s *Service) processAndReply(
	ctx context.Context,
	cfg *domain.AIConfig,
	apiKeyEnc, embedAPIKeyEnc []byte,
	deviceID, chatJID, senderJID, text string,
	send SendFn,
) error {
	start := time.Now()

	apiKey, err := Decrypt(apiKeyEnc)
	if err != nil {
		return fmt.Errorf("decrypt api key: %w", err)
	}
	chatProv, err := buildChatProvider(cfg, apiKey)
	if err != nil {
		return err
	}

	// Resolve embedding provider (Anthropic does not support embeddings).
	embedProv, embedModel, err := s.resolveEmbedProvider(cfg, apiKey, embedAPIKeyEnc)
	if err != nil {
		return err
	}

	// Retrieve relevant KB chunks.
	var retrieved []RetrievedChunkContent
	var retrievedIDs []int64
	var topScore float64

	if s.Vec.Available() && embedProv != nil {
		qVecs, err := embedProv.Embed(ctx, embedModel, []string{text})
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}
		if len(qVecs) > 0 {
			topK := cfg.TopK
			if topK <= 0 {
				topK = 4
			}
			hits, err := s.Vec.Search(ctx, deviceID, qVecs[0], topK)
			if err != nil {
				return fmt.Errorf("vec search: %w", err)
			}
			if len(hits) > 0 {
				topScore = hits[0].Score
				ids := make([]int64, 0, len(hits))
				for _, h := range hits {
					ids = append(ids, h.Chunk.ID)
				}
				chunks, err := s.Repo.GetChunksByIDs(ctx, deviceID, ids)
				if err != nil {
					return fmt.Errorf("load chunks: %w", err)
				}
				// Re-order chunks to match search order and pair with scores.
				byID := make(map[int64]domain.KBChunk, len(chunks))
				for _, c := range chunks {
					byID[c.ID] = c
				}
				for _, h := range hits {
					if c, ok := byID[h.Chunk.ID]; ok {
						retrieved = append(retrieved, RetrievedChunkContent{Score: h.Score, Content: c.Content})
						retrievedIDs = append(retrievedIDs, c.ID)
					}
				}
			}
		}
	}

	// Pre-LLM guardrail: bail out if nothing relevant.
	threshold := cfg.RetrievalThreshold
	if threshold <= 0 {
		threshold = 0.3
	}
	oos := cfg.OutOfScopeMessage
	if strings.TrimSpace(oos) == "" {
		oos = domain.DefaultOutOfScopeMessage
	}
	// Guardrail only makes sense when there IS a KB to ground against. If
	// the device has zero indexed documents (or embeddings are unavailable),
	// skip the guardrail entirely so the LLM can answer freely from its own
	// knowledge — otherwise users would only ever see the out-of-scope
	// template until they upload their first document.
	guardrailActive := cfg.GuardrailEnabled
	if guardrailActive && len(retrieved) == 0 {
		if docs, err := s.Repo.ListDocuments(ctx, deviceID); err == nil && len(docs) == 0 {
			guardrailActive = false
		}
	}
	if guardrailActive && (len(retrieved) == 0 || topScore < threshold) {
		if err := s.sendAndStore(ctx, deviceID, chatJID, oos, send); err != nil {
			return err
		}
		_ = s.Repo.InsertLog(ctx, &domain.ReplyLog{
			DeviceID: deviceID, ChatJID: chatJID, Query: text,
			RetrievedChunkIDs: jsonIDs(retrievedIDs),
			Response:          oos,
			Status:            domain.LogStatusOutOfScope,
			LatencyMs:         int(time.Since(start).Milliseconds()),
		})
		return nil
	}

	// Truncate context to stay within prompt budget (≈4000 tokens total).
	retrieved = truncateContext(retrieved, 4000-cfg.MaxTokens)

	// Load short chat history.
	history := s.loadHistory(ctx, deviceID, chatJID, 5)

	messages := BuildPrompt(cfg, text, retrieved, history)
	resp, err := chatProv.Chat(ctx, domain.ChatRequest{
		Messages:    messages,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	})
	if err != nil {
		return fmt.Errorf("llm chat: %w", err)
	}
	reply := strings.TrimSpace(resp.Content)
	if reply == "" {
		return errors.New("empty reply from llm")
	}

	if err := s.sendAndStore(ctx, deviceID, chatJID, reply, send); err != nil {
		return err
	}

	_ = s.Repo.InsertLog(ctx, &domain.ReplyLog{
		DeviceID:          deviceID,
		ChatJID:           chatJID,
		Query:             text,
		RetrievedChunkIDs: jsonIDs(retrievedIDs),
		Response:          reply,
		LatencyMs:         int(time.Since(start).Milliseconds()),
		TokensIn:          resp.TokensIn,
		TokensOut:         resp.TokensOut,
		Status:            domain.LogStatusSuccess,
	})
	return nil
}

func (s *Service) sendAndStore(ctx context.Context, deviceID, chatJID, text string, send SendFn) error {
	msgID, ts, err := send(ctx, chatJID, text)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if s.ChatStorage != nil {
		if err := s.ChatStorage.StoreSentMessageWithContext(
			ctx, msgID, deviceID, chatJID, text, ts, nil,
		); err != nil {
			logrus.Warnf("ai-reply: store sent message: %v", err)
		}
	}
	return nil
}

func (s *Service) loadHistory(ctx context.Context, deviceID, chatJID string, limit int) []HistoryMessage {
	if s.ChatStorage == nil {
		return nil
	}
	msgs, err := s.ChatStorage.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID, ChatJID: chatJID, Limit: limit,
	})
	if err != nil || len(msgs) == 0 {
		return nil
	}
	out := make([]HistoryMessage, 0, len(msgs))
	// GetMessages returns newest-first; flip to chronological order for the
	// LLM, and skip the most recent (which is the current incoming message).
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, HistoryMessage{IsFromMe: m.IsFromMe, Content: m.Content})
	}
	// Drop the trailing message that matches the current incoming one (if it
	// was already stored before this handler ran). Best-effort: keep order.
	if len(out) > 0 && !out[len(out)-1].IsFromMe {
		out = out[:len(out)-1]
	}
	return out
}

// resolveEmbedProvider returns the provider + model to use for embeddings.
// Anthropic chat configs fall back to embed_* fields; openai-compatible chat
// configs reuse their own client/model when embed_* is empty.
func (s *Service) resolveEmbedProvider(cfg *domain.AIConfig, chatAPIKey string, embedAPIKeyEnc []byte) (domain.IAIProvider, string, error) {
	// If embed_provider is explicitly configured, use it.
	if cfg.EmbedProvider != "" {
		embedKey, err := Decrypt(embedAPIKeyEnc)
		if err != nil {
			return nil, "", fmt.Errorf("decrypt embed key: %w", err)
		}
		if embedKey == "" {
			embedKey = chatAPIKey // common pattern: same key for chat + embed
		}
		switch cfg.EmbedProvider {
		case domain.ProviderOpenAICompatible:
			return NewOpenAICompatibleProvider(embedKey, cfg.EmbedBaseURL), cfg.EmbedModel, nil
		default:
			return nil, "", fmt.Errorf("embed provider %q not supported", cfg.EmbedProvider)
		}
	}
	// Implicit fallback: openai-compatible chat provider doubles as embed.
	if cfg.Provider == domain.ProviderOpenAICompatible {
		model := cfg.EmbedModel
		if model == "" {
			model = "text-embedding-3-small"
		}
		return NewOpenAICompatibleProvider(chatAPIKey, cfg.BaseURL), model, nil
	}
	return nil, "", errors.New("no embed provider configured (Anthropic chat requires embed_provider=openai_compatible + embed_api_key + embed_model)")
}

func buildChatProvider(cfg *domain.AIConfig, apiKey string) (domain.IAIProvider, error) {
	switch cfg.Provider {
	case domain.ProviderAnthropic:
		return NewAnthropicProvider(apiKey, cfg.BaseURL), nil
	case domain.ProviderOpenAICompatible:
		return NewOpenAICompatibleProvider(apiKey, cfg.BaseURL), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

func truncateContext(in []RetrievedChunkContent, maxTokens int) []RetrievedChunkContent {
	if maxTokens <= 0 {
		return in
	}
	const charsPerTok = 4
	budget := maxTokens * charsPerTok
	used := 0
	out := make([]RetrievedChunkContent, 0, len(in))
	for _, c := range in {
		if used+len(c.Content) > budget {
			remain := budget - used
			if remain > 200 {
				c.Content = c.Content[:remain]
				out = append(out, c)
			}
			break
		}
		out = append(out, c)
		used += len(c.Content)
	}
	return out
}

func jsonIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// =========================================================================
// REST-facing methods
// =========================================================================

// GetConfig returns the persisted config with the API key masked.
func (s *Service) GetConfig(ctx context.Context, deviceID string) (*domain.AIConfig, error) {
	cfg, apiKeyEnc, embedKeyEnc, err := s.Repo.GetWithSecrets(ctx, deviceID)
	if err != nil || cfg == nil {
		return cfg, err
	}
	if len(apiKeyEnc) > 0 {
		if dec, err := Decrypt(apiKeyEnc); err == nil {
			cfg.APIKey = MaskKey(dec)
		}
	}
	if len(embedKeyEnc) > 0 {
		if dec, err := Decrypt(embedKeyEnc); err == nil {
			cfg.EmbedAPIKey = MaskKey(dec)
		}
	}
	return cfg, nil
}

// SaveConfig validates + encrypts secrets + upserts the config row.
func (s *Service) SaveConfig(ctx context.Context, deviceID string, req domain.AIConfigRequest) error {
	cfg := &domain.AIConfig{
		DeviceID:           deviceID,
		Provider:           req.Provider,
		Model:              req.Model,
		BaseURL:            strings.TrimSpace(req.BaseURL),
		EmbedProvider:      req.EmbedProvider,
		EmbedModel:         req.EmbedModel,
		EmbedBaseURL:       strings.TrimSpace(req.EmbedBaseURL),
		SystemPrompt:       req.SystemPrompt,
		StylePreset:        req.StylePreset,
		MaxTokens:          req.MaxTokens,
		Temperature:        req.Temperature,
		TopK:               req.TopK,
		RetrievalThreshold: req.RetrievalThreshold,
		GuardrailEnabled:   req.GuardrailEnabled,
		OutOfScopeMessage:  req.OutOfScopeMessage,
		UpdatedAt:          time.Now().UTC(),
	}
	// Sensible defaults.
	if cfg.StylePreset == "" {
		cfg.StylePreset = domain.StyleCustomerServiceFormal
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 500
	}
	if cfg.Temperature < 0 {
		cfg.Temperature = 0.3
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 4
	}
	if cfg.RetrievalThreshold <= 0 {
		cfg.RetrievalThreshold = 0.3
	}
	if cfg.OutOfScopeMessage == "" {
		cfg.OutOfScopeMessage = domain.DefaultOutOfScopeMessage
	}

	var apiKeyEnc, embedKeyEnc []byte
	if strings.TrimSpace(req.APIKey) != "" && !looksMasked(req.APIKey) {
		enc, err := Encrypt(req.APIKey)
		if err != nil {
			return err
		}
		apiKeyEnc = enc
	}
	if strings.TrimSpace(req.EmbedAPIKey) != "" && !looksMasked(req.EmbedAPIKey) {
		enc, err := Encrypt(req.EmbedAPIKey)
		if err != nil {
			return err
		}
		embedKeyEnc = enc
	}
	return s.Repo.UpsertWithSecrets(ctx, cfg, apiKeyEnc, embedKeyEnc)
}

// looksMasked detects the placeholder string returned by GetConfig so we don't
// re-save the masked form as the actual key.
func looksMasked(s string) bool {
	return strings.Contains(s, "****")
}

// TestConfig issues a tiny test call against the configured chat provider.
func (s *Service) TestConfig(ctx context.Context, deviceID string) (int, string, error) {
	cfg, apiKeyEnc, _, err := s.Repo.GetWithSecrets(ctx, deviceID)
	if err != nil || cfg == nil {
		return 0, "", errors.New("config not set")
	}
	apiKey, err := Decrypt(apiKeyEnc)
	if err != nil {
		return 0, "", err
	}
	prov, err := buildChatProvider(cfg, apiKey)
	if err != nil {
		return 0, "", err
	}
	start := time.Now()
	resp, err := prov.Chat(ctx, domain.ChatRequest{
		Model: cfg.Model,
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "Reply only with the word: pong"},
		},
		MaxTokens:   16,
		Temperature: 0,
	})
	if err != nil {
		return 0, "", err
	}
	return int(time.Since(start).Milliseconds()), resp.Content, nil
}

// UploadDocument persists the document row, then parses + chunks + embeds the
// content. Embedding happens in the background so the HTTP request can return
// fast with status=processing.
func (s *Service) UploadDocument(ctx context.Context, deviceID, filename, mime string, data []byte) (*domain.KBDocument, error) {
	doc := &domain.KBDocument{
		ID:        uuid.New().String(),
		DeviceID:  deviceID,
		Filename:  filename,
		MimeType:  mime,
		FileSize:  int64(len(data)),
		Status:    domain.DocStatusProcessing,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Repo.CreateDocument(ctx, doc); err != nil {
		return nil, err
	}
	go s.ingestDocument(deviceID, doc.ID, filename, mime, data)
	return doc, nil
}

// ingestDocument runs the parse → chunk → embed → store pipeline. Failures
// flip the document status to "failed" with an error message.
func (s *Service) ingestDocument(deviceID, docID, filename, mime string, data []byte) {
	ctx := context.Background()
	fail := func(msg string) {
		_ = s.Repo.UpdateDocumentStatus(ctx, docID, domain.DocStatusFailed, msg, 0)
		logrus.Warnf("ai-reply: ingest %s failed: %s", docID, msg)
	}

	text, err := ExtractText(data, filename, mime)
	if err != nil {
		fail(err.Error())
		return
	}
	chunks := Chunk(text, 500, 50)
	if len(chunks) == 0 {
		fail("no chunks produced from document")
		return
	}

	cfg, apiKeyEnc, embedKeyEnc, err := s.Repo.GetWithSecrets(ctx, deviceID)
	if err != nil || cfg == nil {
		fail("AI config not set for this device — save config first, then re-upload")
		return
	}
	apiKey, err := Decrypt(apiKeyEnc)
	if err != nil {
		fail("decrypt api key: " + err.Error())
		return
	}
	embedProv, embedModel, err := s.resolveEmbedProvider(cfg, apiKey, embedKeyEnc)
	if err != nil {
		fail(err.Error())
		return
	}

	if err := s.Vec.Init(config.AIVectorDimension); err != nil {
		fail("vector store init: " + err.Error())
		return
	}

	// Insert chunks into kb_chunks first to get their row IDs, then pair
	// embeddings 1:1 in the vec table.
	rows := make([]domain.KBChunk, 0, len(chunks))
	for _, c := range chunks {
		rows = append(rows, domain.KBChunk{
			DocumentID: docID,
			DeviceID:   deviceID,
			ChunkIndex: c.Index,
			Content:    c.Content,
			TokenCount: c.TokenCount,
		})
	}
	ids, err := s.Repo.InsertChunks(ctx, rows)
	if err != nil {
		fail("insert chunks: " + err.Error())
		return
	}

	// Embed in batches (most providers cap at 100 inputs per request).
	const batch = 64
	for i := 0; i < len(rows); i += batch {
		end := i + batch
		if end > len(rows) {
			end = len(rows)
		}
		inputs := make([]string, 0, end-i)
		for _, r := range rows[i:end] {
			inputs = append(inputs, r.Content)
		}
		vecs, err := embedProv.Embed(ctx, embedModel, inputs)
		if err != nil {
			fail("embed: " + err.Error())
			return
		}
		for j, v := range vecs {
			if err := s.Vec.Insert(ctx, deviceID, ids[i+j], v); err != nil {
				fail("vec insert: " + err.Error())
				return
			}
		}
	}

	_ = s.Repo.UpdateDocumentStatus(ctx, docID, domain.DocStatusReady, "", len(chunks))
}

// ListDocuments lists KB docs for a device.
func (s *Service) ListDocuments(ctx context.Context, deviceID string) ([]domain.KBDocument, error) {
	return s.Repo.ListDocuments(ctx, deviceID)
}

// DeleteDocument removes a doc, its chunks, and its vector rows.
func (s *Service) DeleteDocument(ctx context.Context, deviceID, id string) error {
	chunks, err := s.Repo.GetChunksByDocument(ctx, deviceID, id)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(chunks))
	for _, c := range chunks {
		ids = append(ids, c.ID)
	}
	if err := s.Vec.DeleteByChunkIDs(ctx, ids); err != nil {
		logrus.Warnf("ai-reply: vec delete: %v", err)
	}
	return s.Repo.DeleteDocument(ctx, deviceID, id)
}

// ReindexAll rebuilds vectors for every chunk of a device. Useful after
// changing the embed model.
func (s *Service) ReindexAll(ctx context.Context, deviceID string) error {
	cfg, apiKeyEnc, embedKeyEnc, err := s.Repo.GetWithSecrets(ctx, deviceID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("config not set")
	}
	apiKey, err := Decrypt(apiKeyEnc)
	if err != nil {
		return err
	}
	embedProv, embedModel, err := s.resolveEmbedProvider(cfg, apiKey, embedKeyEnc)
	if err != nil {
		return err
	}
	if err := s.Vec.Init(config.AIVectorDimension); err != nil {
		return err
	}
	if err := s.Vec.DeleteByDevice(ctx, deviceID); err != nil {
		return err
	}
	chunks, err := s.Repo.ListAllChunks(ctx, deviceID)
	if err != nil {
		return err
	}
	const batch = 64
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		inputs := make([]string, 0, end-i)
		for _, c := range chunks[i:end] {
			inputs = append(inputs, c.Content)
		}
		vecs, err := embedProv.Embed(ctx, embedModel, inputs)
		if err != nil {
			return err
		}
		for j, v := range vecs {
			if err := s.Vec.Insert(ctx, deviceID, chunks[i+j].ID, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListChatSettings exposes the per-chat toggle table.
func (s *Service) ListChatSettings(ctx context.Context, deviceID string) ([]domain.ChatSetting, error) {
	return s.Repo.ListChatSettings(ctx, deviceID)
}

// SetChatEnabled upserts the per-chat toggle.
func (s *Service) SetChatEnabled(ctx context.Context, deviceID, chatJID string, enabled bool) error {
	return s.Repo.UpsertChatSetting(ctx, &domain.ChatSetting{
		DeviceID: deviceID, ChatJID: chatJID, Enabled: enabled,
	})
}

// ListLogs returns audit log entries.
func (s *Service) ListLogs(ctx context.Context, filter domain.LogFilter) ([]domain.ReplyLog, error) {
	return s.Repo.ListLogs(ctx, filter)
}
