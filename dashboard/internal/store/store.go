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
