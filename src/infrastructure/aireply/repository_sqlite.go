// Package aireply provides SQLite-backed persistence and vector store for the
// AI auto-reply feature. The vector store is a thin wrapper around the
// sqlite-vec extension (virtual table kb_chunks_vec).
package aireply

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/aireply"
)

// Repository implements IConfigRepository, IKBRepository, IChatSettingRepository,
// and ILogRepository against a single *sql.DB.
type Repository struct {
	db *sql.DB
}

// NewRepository wires the multi-interface SQLite repo. The caller is
// responsible for migrations.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ---------- IConfigRepository ----------

func (r *Repository) Get(ctx context.Context, deviceID string) (*domain.AIConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT device_id, provider, model, base_url,
		       embed_provider, embed_model, embed_base_url,
		       system_prompt, style_preset,
		       max_tokens, temperature, top_k, retrieval_threshold,
		       guardrail_enabled, out_of_scope_message, updated_at
		FROM ai_config WHERE device_id = ?`, deviceID)
	var c domain.AIConfig
	err := row.Scan(&c.DeviceID, &c.Provider, &c.Model, &c.BaseURL,
		&c.EmbedProvider, &c.EmbedModel, &c.EmbedBaseURL,
		&c.SystemPrompt, &c.StylePreset,
		&c.MaxTokens, &c.Temperature, &c.TopK, &c.RetrievalThreshold,
		&c.GuardrailEnabled, &c.OutOfScopeMessage, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetWithSecrets returns the config plus encrypted secret blobs. Caller is
// expected to decrypt only what it needs.
func (r *Repository) GetWithSecrets(ctx context.Context, deviceID string) (*domain.AIConfig, []byte, []byte, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT device_id, provider, model, api_key_encrypted, base_url,
		       embed_provider, embed_model, embed_api_key_encrypted, embed_base_url,
		       system_prompt, style_preset,
		       max_tokens, temperature, top_k, retrieval_threshold,
		       guardrail_enabled, out_of_scope_message, updated_at
		FROM ai_config WHERE device_id = ?`, deviceID)
	var (
		c                                    domain.AIConfig
		apiKeyEnc, embedAPIKeyEnc            []byte
	)
	err := row.Scan(&c.DeviceID, &c.Provider, &c.Model, &apiKeyEnc, &c.BaseURL,
		&c.EmbedProvider, &c.EmbedModel, &embedAPIKeyEnc, &c.EmbedBaseURL,
		&c.SystemPrompt, &c.StylePreset,
		&c.MaxTokens, &c.Temperature, &c.TopK, &c.RetrievalThreshold,
		&c.GuardrailEnabled, &c.OutOfScopeMessage, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	return &c, apiKeyEnc, embedAPIKeyEnc, nil
}

// Upsert inserts or replaces config row. Caller must pre-encrypt secrets and
// pass them via UpsertWithSecrets — this convenience method stores empty secret
// blobs (intended for non-secret updates only).
func (r *Repository) Upsert(ctx context.Context, cfg *domain.AIConfig) error {
	return r.UpsertWithSecrets(ctx, cfg, nil, nil)
}

func (r *Repository) UpsertWithSecrets(ctx context.Context, cfg *domain.AIConfig, apiKeyEnc, embedAPIKeyEnc []byte) error {
	// Bind nil as a zero-length BLOB rather than SQL NULL. The columns are
	// NOT NULL, but the UPDATE branch uses CASE length(excluded.*) > 0 to
	// decide whether to overwrite; empty bytes preserve the existing
	// secret. Without this, a "save without re-typing the key" hits the
	// NOT NULL check before the conflict resolution even runs.
	if apiKeyEnc == nil {
		apiKeyEnc = []byte{}
	}
	if embedAPIKeyEnc == nil {
		embedAPIKeyEnc = []byte{}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ai_config (
			device_id, provider, model, api_key_encrypted, base_url,
			embed_provider, embed_model, embed_api_key_encrypted, embed_base_url,
			system_prompt, style_preset,
			max_tokens, temperature, top_k, retrieval_threshold,
			guardrail_enabled, out_of_scope_message, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			api_key_encrypted = CASE WHEN length(excluded.api_key_encrypted) > 0 THEN excluded.api_key_encrypted ELSE ai_config.api_key_encrypted END,
			base_url = excluded.base_url,
			embed_provider = excluded.embed_provider,
			embed_model = excluded.embed_model,
			embed_api_key_encrypted = CASE WHEN length(excluded.embed_api_key_encrypted) > 0 THEN excluded.embed_api_key_encrypted ELSE ai_config.embed_api_key_encrypted END,
			embed_base_url = excluded.embed_base_url,
			system_prompt = excluded.system_prompt,
			style_preset = excluded.style_preset,
			max_tokens = excluded.max_tokens,
			temperature = excluded.temperature,
			top_k = excluded.top_k,
			retrieval_threshold = excluded.retrieval_threshold,
			guardrail_enabled = excluded.guardrail_enabled,
			out_of_scope_message = excluded.out_of_scope_message,
			updated_at = excluded.updated_at
	`,
		cfg.DeviceID, cfg.Provider, cfg.Model, apiKeyEnc, cfg.BaseURL,
		cfg.EmbedProvider, cfg.EmbedModel, embedAPIKeyEnc, cfg.EmbedBaseURL,
		cfg.SystemPrompt, cfg.StylePreset,
		cfg.MaxTokens, cfg.Temperature, cfg.TopK, cfg.RetrievalThreshold,
		cfg.GuardrailEnabled, cfg.OutOfScopeMessage, time.Now().UTC(),
	)
	return err
}

func (r *Repository) Delete(ctx context.Context, deviceID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ai_config WHERE device_id = ?`, deviceID)
	return err
}

// ---------- IKBRepository ----------

func (r *Repository) CreateDocument(ctx context.Context, doc *domain.KBDocument) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO kb_documents (id, device_id, filename, mime_type, file_size, chunk_count, status, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.DeviceID, doc.Filename, doc.MimeType, doc.FileSize,
		doc.ChunkCount, doc.Status, doc.ErrorMessage, doc.CreatedAt)
	return err
}

func (r *Repository) UpdateDocumentStatus(ctx context.Context, id, status, errMsg string, chunkCount int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE kb_documents SET status = ?, error_message = ?, chunk_count = ?
		WHERE id = ?`, status, errMsg, chunkCount, id)
	return err
}

func (r *Repository) GetDocument(ctx context.Context, deviceID, id string) (*domain.KBDocument, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, device_id, filename, mime_type, file_size, chunk_count, status, error_message, created_at
		FROM kb_documents WHERE device_id = ? AND id = ?`, deviceID, id)
	var d domain.KBDocument
	err := row.Scan(&d.ID, &d.DeviceID, &d.Filename, &d.MimeType, &d.FileSize,
		&d.ChunkCount, &d.Status, &d.ErrorMessage, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repository) ListDocuments(ctx context.Context, deviceID string) ([]domain.KBDocument, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, device_id, filename, mime_type, file_size, chunk_count, status, error_message, created_at
		FROM kb_documents WHERE device_id = ? ORDER BY created_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.KBDocument
	for rows.Next() {
		var d domain.KBDocument
		if err := rows.Scan(&d.ID, &d.DeviceID, &d.Filename, &d.MimeType, &d.FileSize,
			&d.ChunkCount, &d.Status, &d.ErrorMessage, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteDocument(ctx context.Context, deviceID, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_chunks WHERE device_id = ? AND document_id = ?`, deviceID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_documents WHERE device_id = ? AND id = ?`, deviceID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertChunks bulk-inserts chunks and returns the assigned row IDs in the
// same order as the input slice.
func (r *Repository) InsertChunks(ctx context.Context, chunks []domain.KBChunk) ([]int64, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO kb_chunks (document_id, device_id, chunk_index, content, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	ids := make([]int64, 0, len(chunks))
	now := time.Now().UTC()
	for _, c := range chunks {
		res, err := stmt.ExecContext(ctx, c.DocumentID, c.DeviceID, c.ChunkIndex, c.Content, c.TokenCount, now)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *Repository) GetChunksByDocument(ctx context.Context, deviceID, documentID string) ([]domain.KBChunk, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, document_id, device_id, chunk_index, content, token_count, created_at
		FROM kb_chunks WHERE device_id = ? AND document_id = ? ORDER BY chunk_index`, deviceID, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChunks(rows)
}

func (r *Repository) GetChunksByIDs(ctx context.Context, deviceID string, ids []int64) ([]domain.KBChunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+1)
	args = append(args, deviceID)
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(`
		SELECT id, document_id, device_id, chunk_index, content, token_count, created_at
		FROM kb_chunks WHERE device_id = ? AND id IN (%s)`, placeholders)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChunks(rows)
}

func (r *Repository) ListAllChunks(ctx context.Context, deviceID string) ([]domain.KBChunk, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, document_id, device_id, chunk_index, content, token_count, created_at
		FROM kb_chunks WHERE device_id = ? ORDER BY document_id, chunk_index`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChunks(rows)
}

func scanChunks(rows *sql.Rows) ([]domain.KBChunk, error) {
	var out []domain.KBChunk
	for rows.Next() {
		var c domain.KBChunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.DeviceID, &c.ChunkIndex,
			&c.Content, &c.TokenCount, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- IChatSettingRepository ----------

func (r *Repository) GetChatSetting(ctx context.Context, deviceID, chatJID string) (*domain.ChatSetting, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT device_id, chat_jid, enabled, updated_at FROM ai_chat_settings
		WHERE device_id = ? AND chat_jid = ?`, deviceID, chatJID)
	var s domain.ChatSetting
	err := row.Scan(&s.DeviceID, &s.ChatJID, &s.Enabled, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) ListChatSettings(ctx context.Context, deviceID string) ([]domain.ChatSetting, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT device_id, chat_jid, enabled, updated_at FROM ai_chat_settings
		WHERE device_id = ? ORDER BY updated_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ChatSetting
	for rows.Next() {
		var s domain.ChatSetting
		if err := rows.Scan(&s.DeviceID, &s.ChatJID, &s.Enabled, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertChatSetting(ctx context.Context, s *domain.ChatSetting) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ai_chat_settings (device_id, chat_jid, enabled, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, chat_jid) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		s.DeviceID, s.ChatJID, s.Enabled, time.Now().UTC())
	return err
}

func (r *Repository) DeleteChatSetting(ctx context.Context, deviceID, chatJID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ai_chat_settings WHERE device_id = ? AND chat_jid = ?`, deviceID, chatJID)
	return err
}

// ---------- ILogRepository ----------

func (r *Repository) InsertLog(ctx context.Context, log *domain.ReplyLog) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ai_reply_logs (device_id, chat_jid, query, retrieved_chunk_ids,
			response, latency_ms, tokens_in, tokens_out, status, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.DeviceID, log.ChatJID, log.Query, log.RetrievedChunkIDs,
		log.Response, log.LatencyMs, log.TokensIn, log.TokensOut,
		log.Status, log.ErrorMessage, time.Now().UTC())
	return err
}

// ---------- Global pause state ----------
// Single-row table (id=1 enforced by CHECK constraint). Backs
// usecase/aireply/pause.go's persisted pause deadline so it survives a
// process restart.

func (r *Repository) GetPauseUntil(ctx context.Context) (*time.Time, error) {
	row := r.db.QueryRowContext(ctx, `SELECT paused_until FROM ai_pause_state WHERE id = 1`)
	var until sql.NullTime
	err := row.Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !until.Valid {
		return nil, nil
	}
	t := until.Time
	return &t, nil
}

func (r *Repository) SetPauseUntil(ctx context.Context, until *time.Time) error {
	var v any
	if until != nil {
		v = *until
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ai_pause_state (id, paused_until, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			paused_until = excluded.paused_until,
			updated_at = excluded.updated_at`,
		v, time.Now().UTC())
	return err
}

func (r *Repository) ListLogs(ctx context.Context, filter domain.LogFilter) ([]domain.ReplyLog, error) {
	q := `SELECT id, device_id, chat_jid, query, retrieved_chunk_ids, response,
		latency_ms, tokens_in, tokens_out, status, error_message, created_at
		FROM ai_reply_logs WHERE device_id = ?`
	args := []any{filter.DeviceID}
	if filter.ChatJID != "" {
		q += " AND chat_jid = ?"
		args = append(args, filter.ChatJID)
	}
	if filter.Status != "" {
		q += " AND status = ?"
		args = append(args, filter.Status)
	}
	q += " ORDER BY id DESC"
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 50
	}
	q += fmt.Sprintf(" LIMIT %d", filter.Limit)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ReplyLog
	for rows.Next() {
		var l domain.ReplyLog
		if err := rows.Scan(&l.ID, &l.DeviceID, &l.ChatJID, &l.Query, &l.RetrievedChunkIDs,
			&l.Response, &l.LatencyMs, &l.TokensIn, &l.TokensOut,
			&l.Status, &l.ErrorMessage, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
