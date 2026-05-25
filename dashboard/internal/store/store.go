package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Schedule struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	DeviceID     string     `json:"device_id"`
	Recipient    string     `json:"recipient"`
	MessageType  string     `json:"message_type"` // text | image | video | file | audio | location | link
	Message      string     `json:"message"`
	MediaURL     string     `json:"media_url"`
	Caption      string     `json:"caption"`
	Latitude     string     `json:"latitude"`
	Longitude    string     `json:"longitude"`
	LinkURL      string     `json:"link_url"`
	ScheduleType string     `json:"schedule_type"` // once | daily | weekly | monthly | yearly | cron
	RunAt        *time.Time `json:"run_at,omitempty"`
	CronExpr     string     `json:"cron_expr"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	LastStatus   string     `json:"last_status"`
	LastError    string     `json:"last_error"`
	RunCount     int64      `json:"run_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ScheduleLog struct {
	ID         int64     `json:"id"`
	ScheduleID int64     `json:"schedule_id"`
	RanAt      time.Time `json:"ran_at"`
	Status     string    `json:"status"`
	Response   string    `json:"response"`
	Error      string    `json:"error"`
}

// Broadcast adalah job pengiriman pesan ke banyak nomor sekaligus dengan
// strategi anti-spam (random delay, batch break, dst).
type Broadcast struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DeviceID    string `json:"device_id"`
	MessageType string `json:"message_type"` // text | image | video | file | audio
	Message     string `json:"message"`      // body text atau caption. Boleh pakai spintax {a|b|c}
	MediaURL    string `json:"media_url"`

	// Anti-spam knobs — semua dalam millisecond untuk presisi tinggi.
	DelayMinMs      int  `json:"delay_min_ms"`        // jeda minimal antar pesan
	DelayMaxMs      int  `json:"delay_max_ms"`        // jeda maksimal antar pesan
	BatchSize       int  `json:"batch_size"`          // 0 = disabled, >0 = istirahat tiap N pesan
	BatchPauseMinMs int  `json:"batch_pause_min_ms"`  // jeda min saat batch break
	BatchPauseMaxMs int  `json:"batch_pause_max_ms"`  // jeda max saat batch break
	SimulateTyping  bool `json:"simulate_typing"`     // (placeholder utk future) presence typing sebelum kirim
	ShuffleOrder    bool `json:"shuffle_order"`       // urutan acak supaya tidak alphabet/squence

	Status      string     `json:"status"` // pending | running | completed | cancelled | failed
	Total       int        `json:"total"`
	SentCount   int        `json:"sent_count"`
	FailedCount int        `json:"failed_count"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// BroadcastRecipient — satu baris per nomor tujuan di sebuah broadcast.
type BroadcastRecipient struct {
	ID          int64      `json:"id"`
	BroadcastID int64      `json:"broadcast_id"`
	Recipient   string     `json:"recipient"` // normalized JID (628xxx@s.whatsapp.net)
	RawInput    string     `json:"raw_input"` // input asli user (utk troubleshoot)
	Status      string     `json:"status"`    // pending | sent | failed | skipped
	Error       string     `json:"error"`
	Response    string     `json:"response"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
}

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS schedules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    device_id     TEXT NOT NULL,
    recipient     TEXT NOT NULL,
    message_type  TEXT NOT NULL,
    message       TEXT NOT NULL DEFAULT '',
    media_url     TEXT NOT NULL DEFAULT '',
    caption       TEXT NOT NULL DEFAULT '',
    latitude      TEXT NOT NULL DEFAULT '',
    longitude     TEXT NOT NULL DEFAULT '',
    link_url      TEXT NOT NULL DEFAULT '',
    schedule_type TEXT NOT NULL,
    run_at        DATETIME,
    cron_expr     TEXT NOT NULL DEFAULT '',
    timezone      TEXT NOT NULL DEFAULT 'Local',
    enabled       INTEGER NOT NULL DEFAULT 1,
    last_run_at   DATETIME,
    next_run_at   DATETIME,
    last_status   TEXT NOT NULL DEFAULT '',
    last_error    TEXT NOT NULL DEFAULT '',
    run_count     INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS schedule_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_id INTEGER NOT NULL,
    ran_at      DATETIME NOT NULL,
    status      TEXT NOT NULL,
    response    TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(schedule_id) REFERENCES schedules(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_schedules_enabled ON schedules(enabled);
CREATE INDEX IF NOT EXISTS idx_schedule_logs_schedule ON schedule_logs(schedule_id, ran_at DESC);

CREATE TABLE IF NOT EXISTS broadcasts (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL DEFAULT '',
    device_id           TEXT    NOT NULL,
    message_type        TEXT    NOT NULL,
    message             TEXT    NOT NULL DEFAULT '',
    media_url           TEXT    NOT NULL DEFAULT '',
    delay_min_ms        INTEGER NOT NULL DEFAULT 8000,
    delay_max_ms        INTEGER NOT NULL DEFAULT 25000,
    batch_size          INTEGER NOT NULL DEFAULT 0,
    batch_pause_min_ms  INTEGER NOT NULL DEFAULT 0,
    batch_pause_max_ms  INTEGER NOT NULL DEFAULT 0,
    simulate_typing     INTEGER NOT NULL DEFAULT 0,
    shuffle_order       INTEGER NOT NULL DEFAULT 0,
    status              TEXT    NOT NULL DEFAULT 'pending',
    total               INTEGER NOT NULL DEFAULT 0,
    sent_count          INTEGER NOT NULL DEFAULT 0,
    failed_count        INTEGER NOT NULL DEFAULT 0,
    started_at          DATETIME,
    finished_at         DATETIME,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS broadcast_recipients (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    broadcast_id INTEGER NOT NULL,
    recipient    TEXT    NOT NULL,
    raw_input    TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending',
    error        TEXT    NOT NULL DEFAULT '',
    response     TEXT    NOT NULL DEFAULT '',
    sent_at      DATETIME,
    FOREIGN KEY(broadcast_id) REFERENCES broadcasts(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_broadcast_recipients_bc_status ON broadcast_recipients(broadcast_id, status);
CREATE INDEX IF NOT EXISTS idx_broadcasts_status ON broadcasts(status);
`)
	return err
}

func (s *Store) Close() error { return s.DB.Close() }

// --- Schedule CRUD --------------------------------------------------------

func (s *Store) ListSchedules() ([]*Schedule, error) {
	rows, err := s.DB.Query(`SELECT id,name,device_id,recipient,message_type,message,media_url,caption,latitude,longitude,link_url,schedule_type,run_at,cron_expr,timezone,enabled,last_run_at,next_run_at,last_status,last_error,run_count,created_at,updated_at FROM schedules ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		sch, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sch)
	}
	return out, rows.Err()
}

func (s *Store) GetSchedule(id int64) (*Schedule, error) {
	row := s.DB.QueryRow(`SELECT id,name,device_id,recipient,message_type,message,media_url,caption,latitude,longitude,link_url,schedule_type,run_at,cron_expr,timezone,enabled,last_run_at,next_run_at,last_status,last_error,run_count,created_at,updated_at FROM schedules WHERE id=?`, id)
	return scanSchedule(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSchedule(r scanner) (*Schedule, error) {
	var s Schedule
	var runAt, lastRun, nextRun sql.NullTime
	var enabled int
	err := r.Scan(&s.ID, &s.Name, &s.DeviceID, &s.Recipient, &s.MessageType, &s.Message, &s.MediaURL, &s.Caption, &s.Latitude, &s.Longitude, &s.LinkURL, &s.ScheduleType, &runAt, &s.CronExpr, &s.Timezone, &enabled, &lastRun, &nextRun, &s.LastStatus, &s.LastError, &s.RunCount, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if runAt.Valid {
		t := runAt.Time
		s.RunAt = &t
	}
	if lastRun.Valid {
		t := lastRun.Time
		s.LastRunAt = &t
	}
	if nextRun.Valid {
		t := nextRun.Time
		s.NextRunAt = &t
	}
	s.Enabled = enabled != 0
	return &s, nil
}

func (s *Store) CreateSchedule(sc *Schedule) (int64, error) {
	now := time.Now().UTC()
	sc.CreatedAt = now
	sc.UpdatedAt = now
	res, err := s.DB.Exec(`INSERT INTO schedules (name,device_id,recipient,message_type,message,media_url,caption,latitude,longitude,link_url,schedule_type,run_at,cron_expr,timezone,enabled,next_run_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.Name, sc.DeviceID, sc.Recipient, sc.MessageType, sc.Message, sc.MediaURL, sc.Caption, sc.Latitude, sc.Longitude, sc.LinkURL, sc.ScheduleType, nullTime(sc.RunAt), sc.CronExpr, sc.Timezone, boolToInt(sc.Enabled), nullTime(sc.NextRunAt), sc.CreatedAt, sc.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSchedule(sc *Schedule) error {
	sc.UpdatedAt = time.Now().UTC()
	_, err := s.DB.Exec(`UPDATE schedules SET name=?,device_id=?,recipient=?,message_type=?,message=?,media_url=?,caption=?,latitude=?,longitude=?,link_url=?,schedule_type=?,run_at=?,cron_expr=?,timezone=?,enabled=?,next_run_at=?,updated_at=? WHERE id=?`,
		sc.Name, sc.DeviceID, sc.Recipient, sc.MessageType, sc.Message, sc.MediaURL, sc.Caption, sc.Latitude, sc.Longitude, sc.LinkURL, sc.ScheduleType, nullTime(sc.RunAt), sc.CronExpr, sc.Timezone, boolToInt(sc.Enabled), nullTime(sc.NextRunAt), sc.UpdatedAt, sc.ID)
	return err
}

func (s *Store) DeleteSchedule(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM schedules WHERE id=?`, id)
	return err
}

func (s *Store) SetEnabled(id int64, enabled bool) error {
	_, err := s.DB.Exec(`UPDATE schedules SET enabled=?, updated_at=? WHERE id=?`, boolToInt(enabled), time.Now().UTC(), id)
	return err
}

// MarkRun records a run result and updates aggregate fields. nextRun may be nil for one-shot.
func (s *Store) MarkRun(id int64, ranAt time.Time, status, response, errMsg string, nextRun *time.Time, disableAfter bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO schedule_logs (schedule_id, ran_at, status, response, error) VALUES (?,?,?,?,?)`, id, ranAt, status, response, errMsg); err != nil {
		return err
	}

	enabledClause := ""
	if disableAfter {
		enabledClause = ", enabled=0"
	}
	q := fmt.Sprintf(`UPDATE schedules SET last_run_at=?, last_status=?, last_error=?, run_count=run_count+1, next_run_at=?, updated_at=?%s WHERE id=?`, enabledClause)
	if _, err := tx.Exec(q, ranAt, status, errMsg, nullTime(nextRun), time.Now().UTC(), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateNextRun(id int64, next *time.Time) error {
	_, err := s.DB.Exec(`UPDATE schedules SET next_run_at=?, updated_at=? WHERE id=?`, nullTime(next), time.Now().UTC(), id)
	return err
}

func (s *Store) ListLogs(scheduleID int64, limit int) ([]*ScheduleLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.Query(`SELECT id, schedule_id, ran_at, status, response, error FROM schedule_logs WHERE schedule_id=? ORDER BY ran_at DESC LIMIT ?`, scheduleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ScheduleLog
	for rows.Next() {
		var l ScheduleLog
		if err := rows.Scan(&l.ID, &l.ScheduleID, &l.RanAt, &l.Status, &l.Response, &l.Error); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentLogs(limit int) ([]*ScheduleLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id, schedule_id, ran_at, status, response, error FROM schedule_logs ORDER BY ran_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ScheduleLog
	for rows.Next() {
		var l ScheduleLog
		if err := rows.Scan(&l.ID, &l.ScheduleID, &l.RanAt, &l.Status, &l.Response, &l.Error); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// --- Broadcast CRUD -------------------------------------------------------

// CreateBroadcast inserts the broadcast header + all recipients in one tx.
// total is auto-set from len(recipients).
func (s *Store) CreateBroadcast(b *Broadcast, recipients []BroadcastRecipient) (int64, error) {
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	b.Total = len(recipients)
	if b.Status == "" {
		b.Status = "pending"
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO broadcasts
		(name,device_id,message_type,message,media_url,
		 delay_min_ms,delay_max_ms,batch_size,batch_pause_min_ms,batch_pause_max_ms,
		 simulate_typing,shuffle_order,status,total,created_at,updated_at)
		VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?)`,
		b.Name, b.DeviceID, b.MessageType, b.Message, b.MediaURL,
		b.DelayMinMs, b.DelayMaxMs, b.BatchSize, b.BatchPauseMinMs, b.BatchPauseMaxMs,
		boolToInt(b.SimulateTyping), boolToInt(b.ShuffleOrder), b.Status, b.Total, b.CreatedAt, b.UpdatedAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	b.ID = id

	stmt, err := tx.Prepare(`INSERT INTO broadcast_recipients (broadcast_id,recipient,raw_input,status) VALUES (?,?,?,'pending')`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, r := range recipients {
		if _, err := stmt.Exec(id, r.Recipient, r.RawInput); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (s *Store) GetBroadcast(id int64) (*Broadcast, error) {
	row := s.DB.QueryRow(`SELECT id,name,device_id,message_type,message,media_url,
		delay_min_ms,delay_max_ms,batch_size,batch_pause_min_ms,batch_pause_max_ms,
		simulate_typing,shuffle_order,status,total,sent_count,failed_count,
		started_at,finished_at,created_at,updated_at FROM broadcasts WHERE id=?`, id)
	return scanBroadcast(row)
}

func (s *Store) ListBroadcasts(limit int) ([]*Broadcast, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id,name,device_id,message_type,message,media_url,
		delay_min_ms,delay_max_ms,batch_size,batch_pause_min_ms,batch_pause_max_ms,
		simulate_typing,shuffle_order,status,total,sent_count,failed_count,
		started_at,finished_at,created_at,updated_at
		FROM broadcasts ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Broadcast
	for rows.Next() {
		b, err := scanBroadcast(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanBroadcast(r scanner) (*Broadcast, error) {
	var b Broadcast
	var simTyping, shuffle int
	var startedAt, finishedAt sql.NullTime
	err := r.Scan(&b.ID, &b.Name, &b.DeviceID, &b.MessageType, &b.Message, &b.MediaURL,
		&b.DelayMinMs, &b.DelayMaxMs, &b.BatchSize, &b.BatchPauseMinMs, &b.BatchPauseMaxMs,
		&simTyping, &shuffle, &b.Status, &b.Total, &b.SentCount, &b.FailedCount,
		&startedAt, &finishedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	b.SimulateTyping = simTyping != 0
	b.ShuffleOrder = shuffle != 0
	if startedAt.Valid {
		t := startedAt.Time
		b.StartedAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time
		b.FinishedAt = &t
	}
	return &b, nil
}

// ListBroadcastRecipients returns recipients filtered by status. Pass "" for all.
func (s *Store) ListBroadcastRecipients(broadcastID int64, status string) ([]*BroadcastRecipient, error) {
	var rows *sql.Rows
	var err error
	if status == "" {
		rows, err = s.DB.Query(`SELECT id,broadcast_id,recipient,raw_input,status,error,response,sent_at FROM broadcast_recipients WHERE broadcast_id=? ORDER BY id ASC`, broadcastID)
	} else {
		rows, err = s.DB.Query(`SELECT id,broadcast_id,recipient,raw_input,status,error,response,sent_at FROM broadcast_recipients WHERE broadcast_id=? AND status=? ORDER BY id ASC`, broadcastID, status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BroadcastRecipient
	for rows.Next() {
		var r BroadcastRecipient
		var sentAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.BroadcastID, &r.Recipient, &r.RawInput, &r.Status, &r.Error, &r.Response, &sentAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			t := sentAt.Time
			r.SentAt = &t
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// UpdateBroadcastRecipient — dipanggil per kirim. Pakai counter increment terpisah
// karena race-free (UPDATE atomic di SQLite).
func (s *Store) UpdateBroadcastRecipient(id int64, status, errMsg, response string, sentAt *time.Time) error {
	_, err := s.DB.Exec(`UPDATE broadcast_recipients SET status=?, error=?, response=?, sent_at=? WHERE id=?`,
		status, errMsg, response, nullTime(sentAt), id)
	return err
}

// IncrementBroadcastCounter — increment sent_count atau failed_count secara atomic.
func (s *Store) IncrementBroadcastCounter(broadcastID int64, kind string) error {
	col := "sent_count"
	if kind == "failed" {
		col = "failed_count"
	}
	_, err := s.DB.Exec(`UPDATE broadcasts SET `+col+`=`+col+`+1, updated_at=? WHERE id=?`, time.Now().UTC(), broadcastID)
	return err
}

func (s *Store) UpdateBroadcastStatus(b *Broadcast) error {
	b.UpdatedAt = time.Now().UTC()
	_, err := s.DB.Exec(`UPDATE broadcasts SET status=?, started_at=?, updated_at=? WHERE id=?`,
		b.Status, nullTime(b.StartedAt), b.UpdatedAt, b.ID)
	return err
}

func (s *Store) MarkBroadcastFinished(id int64, status string, finishedAt *time.Time) error {
	_, err := s.DB.Exec(`UPDATE broadcasts SET status=?, finished_at=?, updated_at=? WHERE id=?`,
		status, nullTime(finishedAt), time.Now().UTC(), id)
	return err
}

func (s *Store) DeleteBroadcast(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM broadcasts WHERE id=?`, id)
	return err
}

// ResumeRunningBroadcasts — saat dashboard start, broadcast yg statusnya
// "running" itu pasti tertinggal dari proses sebelumnya yg crash/restart.
// Tandai cancelled supaya tidak misleading di UI.
func (s *Store) MarkOrphanedRunningBroadcastsAsCancelled() error {
	_, err := s.DB.Exec(`UPDATE broadcasts SET status='cancelled', finished_at=?, updated_at=? WHERE status='running'`,
		time.Now().UTC(), time.Now().UTC())
	return err
}

// --- Maintenance / log retention -----------------------------------------

// CleanupStats merangkum hasil sekali cleanup. Field nol kalau belum ada
// row yang melewati cutoff.
type CleanupStats struct {
	DeletedScheduleLogs       int64 `json:"deleted_schedule_logs"`
	DeletedBroadcasts         int64 `json:"deleted_broadcasts"`
	DeletedBroadcastRecipients int64 `json:"deleted_broadcast_recipients"`
	CutoffUTC                 time.Time `json:"cutoff_utc"`
}

// CleanupOldLogs hapus data lama supaya dashboard.db tidak terus membesar:
//
//   - schedule_logs.ran_at < cutoff
//   - broadcast_recipients yang induknya broadcasts.finished_at < cutoff
//     (FK ON DELETE CASCADE menangani delete recipients secara otomatis)
//   - broadcasts yg sudah selesai (finished_at IS NOT NULL) dan finished_at < cutoff
//
// Broadcasts yang masih running / pending TIDAK pernah di-cleanup tanpa
// peduli berapa hari, supaya kerja aktif tidak terganggu.
//
// retentionDays <= 0 = disabled (no-op, return zeros).
func (s *Store) CleanupOldLogs(retentionDays int) (CleanupStats, error) {
	var stats CleanupStats
	if retentionDays <= 0 {
		return stats, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	stats.CutoffUTC = cutoff

	tx, err := s.DB.Begin()
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()

	// schedule_logs
	res, err := tx.Exec(`DELETE FROM schedule_logs WHERE ran_at < ?`, cutoff)
	if err != nil {
		return stats, err
	}
	stats.DeletedScheduleLogs, _ = res.RowsAffected()

	// broadcast_recipients dari broadcast yg akan dihapus — hitung dulu utk reporting,
	// FK cascade akan menghapus saat broadcasts row terhapus.
	row := tx.QueryRow(`SELECT COUNT(*) FROM broadcast_recipients br
		JOIN broadcasts b ON br.broadcast_id = b.id
		WHERE b.finished_at IS NOT NULL AND b.finished_at < ?`, cutoff)
	_ = row.Scan(&stats.DeletedBroadcastRecipients)

	// broadcasts (cascade ke broadcast_recipients via FK)
	res, err = tx.Exec(`DELETE FROM broadcasts WHERE finished_at IS NOT NULL AND finished_at < ?`, cutoff)
	if err != nil {
		return stats, err
	}
	stats.DeletedBroadcasts, _ = res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

// VacuumIfFreePagesHigh menjalankan VACUUM hanya kalau auto_vacuum belum
// reclaim space cukup. Best-effort — error di-swallow.
func (s *Store) VacuumIfNeeded() error {
	_, err := s.DB.Exec(`VACUUM`)
	return err
}

// CountRows return jumlah row per tabel untuk dashboard health/stats.
func (s *Store) CountRows() (map[string]int64, error) {
	out := map[string]int64{}
	tables := []string{"schedules", "schedule_logs", "broadcasts", "broadcast_recipients"}
	for _, t := range tables {
		var n int64
		row := s.DB.QueryRow(`SELECT COUNT(*) FROM ` + t)
		if err := row.Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}

// helpers
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var ErrNotFound = errors.New("not found")
