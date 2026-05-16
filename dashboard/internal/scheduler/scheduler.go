package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"

	"github.com/robfig/cron/v3"
)

// Scheduler manages all active schedules using robfig/cron for recurring jobs
// and time.Timer for one-shot jobs.
type Scheduler struct {
	store *store.Store
	wa    *wa.Client

	mu       sync.Mutex
	crons    map[int64]*cronEntry // schedule_id -> cron registration
	onceJobs map[int64]*time.Timer
}

type cronEntry struct {
	c  *cron.Cron
	id cron.EntryID
}

func New(st *store.Store, wac *wa.Client) *Scheduler {
	return &Scheduler{
		store:    st,
		wa:       wac,
		crons:    map[int64]*cronEntry{},
		onceJobs: map[int64]*time.Timer{},
	}
}

// Start loads all enabled schedules and registers them.
func (s *Scheduler) Start() error {
	list, err := s.store.ListSchedules()
	if err != nil {
		return err
	}
	for _, sc := range list {
		if !sc.Enabled {
			continue
		}
		if err := s.register(sc); err != nil {
			log.Printf("[scheduler] failed to register schedule %d: %v", sc.ID, err)
		}
	}
	log.Printf("[scheduler] started with %d schedules", len(list))
	return nil
}

// Reload re-registers a single schedule by id (used after create/update/toggle).
func (s *Scheduler) Reload(id int64) error {
	s.unregister(id)
	sc, err := s.store.GetSchedule(id)
	if err != nil {
		return err
	}
	if !sc.Enabled {
		_ = s.store.UpdateNextRun(id, nil)
		return nil
	}
	return s.register(sc)
}

// Remove fully cancels a schedule.
func (s *Scheduler) Remove(id int64) {
	s.unregister(id)
}

func (s *Scheduler) unregister(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.crons[id]; ok {
		e.c.Remove(e.id)
		e.c.Stop()
		delete(s.crons, id)
	}
	if t, ok := s.onceJobs[id]; ok {
		t.Stop()
		delete(s.onceJobs, id)
	}
}

func (s *Scheduler) register(sc *store.Schedule) error {
	loc, err := parseLocation(sc.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", sc.Timezone, err)
	}

	if sc.ScheduleType == "once" {
		if sc.RunAt == nil {
			return fmt.Errorf("schedule %d type=once but run_at is nil", sc.ID)
		}
		// Stored as UTC; interpret as wall time in target tz only if it has zone info.
		when := sc.RunAt.In(loc)
		next := when
		_ = s.store.UpdateNextRun(sc.ID, &next)

		delay := time.Until(when)
		if delay < 0 {
			// Past due — still fire after a tiny delay so user sees a "missed" run.
			delay = 0
		}
		id := sc.ID
		t := time.AfterFunc(delay, func() { s.runOnce(id, true) })
		s.mu.Lock()
		s.onceJobs[id] = t
		s.mu.Unlock()
		return nil
	}

	// Recurring path - build a cron expression
	expr, err := buildCronExpr(sc)
	if err != nil {
		return err
	}

	c := cron.New(cron.WithLocation(loc))
	id := sc.ID
	entryID, err := c.AddFunc(expr, func() { s.runOnce(id, false) })
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	c.Start()

	s.mu.Lock()
	s.crons[id] = &cronEntry{c: c, id: entryID}
	s.mu.Unlock()

	// Persist computed next_run_at for the UI
	if entry := c.Entry(entryID); !entry.Next.IsZero() {
		nx := entry.Next
		_ = s.store.UpdateNextRun(id, &nx)
	}
	return nil
}

// RunNow fires a schedule immediately (manual button in UI).
func (s *Scheduler) RunNow(id int64) error {
	go s.runOnce(id, false)
	return nil
}

func (s *Scheduler) runOnce(id int64, disableAfter bool) {
	sc, err := s.store.GetSchedule(id)
	if err != nil {
		log.Printf("[scheduler] runOnce: schedule %d not found: %v", id, err)
		return
	}

	ranAt := time.Now().UTC()
	resp, sendErr := s.dispatch(sc)

	status := "success"
	errMsg := ""
	responseStr := ""
	if sendErr != nil {
		status = "error"
		errMsg = sendErr.Error()
	}
	if resp != nil {
		b, _ := json.Marshal(resp)
		responseStr = string(b)
	}

	// Compute next run for recurring; one-shot becomes disabled.
	var nextRun *time.Time
	s.mu.Lock()
	if e, ok := s.crons[id]; ok {
		if entry := e.c.Entry(e.id); !entry.Next.IsZero() {
			n := entry.Next
			nextRun = &n
		}
	}
	s.mu.Unlock()

	if err := s.store.MarkRun(id, ranAt, status, responseStr, errMsg, nextRun, disableAfter); err != nil {
		log.Printf("[scheduler] failed to mark run for schedule %d: %v", id, err)
	}

	if disableAfter {
		// One-shot done -- remove timer from map
		s.mu.Lock()
		delete(s.onceJobs, id)
		s.mu.Unlock()
	}
}

func (s *Scheduler) dispatch(sc *store.Schedule) (*wa.Response, error) {
	switch strings.ToLower(sc.MessageType) {
	case "text":
		return s.wa.SendText(sc.DeviceID, wa.SendTextRequest{
			Phone:   sc.Recipient,
			Message: sc.Message,
		})
	case "image", "video", "file", "document", "audio":
		if sc.MediaURL == "" {
			return nil, fmt.Errorf("media_url required for message_type=%s", sc.MessageType)
		}
		return s.wa.SendMediaURL(sc.DeviceID, sc.MessageType, sc.Recipient, sc.MediaURL, sc.Caption)
	case "location":
		return s.wa.SendLocation(sc.DeviceID, wa.SendLocationRequest{
			Phone:     sc.Recipient,
			Latitude:  sc.Latitude,
			Longitude: sc.Longitude,
		})
	case "link":
		return s.wa.SendLink(sc.DeviceID, wa.SendLinkRequest{
			Phone:   sc.Recipient,
			Link:    sc.LinkURL,
			Caption: sc.Caption,
		})
	}
	return nil, fmt.Errorf("unsupported message_type %q", sc.MessageType)
}

// --- helpers --------------------------------------------------------------

func parseLocation(tz string) (*time.Location, error) {
	if tz == "" || strings.EqualFold(tz, "Local") {
		return time.Local, nil
	}
	if strings.EqualFold(tz, "UTC") {
		return time.UTC, nil
	}
	return time.LoadLocation(tz)
}

// buildCronExpr builds a standard 5-field cron expression for daily/weekly/monthly/yearly,
// or returns the user-provided expr for type=cron.
//
// Convention for sc.CronExpr in built modes is unused; we use these fields:
//   - sc.RunAt:    used as time-of-day source (hour:minute). Date portion is used for
//                  yearly (month + day-of-month) and monthly (day-of-month). For weekly,
//                  use sc.CronExpr to pass days-of-week as CSV "0,1,5" (0=Sunday)
//                  OR fall back to weekday of RunAt.
func buildCronExpr(sc *store.Schedule) (string, error) {
	if sc.ScheduleType == "cron" {
		if sc.CronExpr == "" {
			return "", fmt.Errorf("cron_expr required for schedule_type=cron")
		}
		return sc.CronExpr, nil
	}
	if sc.RunAt == nil {
		return "", fmt.Errorf("run_at required for schedule_type=%s (used for time-of-day)", sc.ScheduleType)
	}

	loc, _ := parseLocation(sc.Timezone)
	t := sc.RunAt.In(loc)
	hour, min := t.Hour(), t.Minute()

	switch sc.ScheduleType {
	case "daily":
		return fmt.Sprintf("%d %d * * *", min, hour), nil
	case "weekly":
		days := strings.TrimSpace(sc.CronExpr)
		if days == "" {
			days = fmt.Sprintf("%d", int(t.Weekday()))
		}
		// validate basic format: csv of 0-6
		if !validDaysCSV(days) {
			return "", fmt.Errorf("weekly days must be CSV of 0-6, got %q", days)
		}
		return fmt.Sprintf("%d %d * * %s", min, hour, days), nil
	case "monthly":
		return fmt.Sprintf("%d %d %d * *", min, hour, t.Day()), nil
	case "yearly":
		return fmt.Sprintf("%d %d %d %d *", min, hour, t.Day(), int(t.Month())), nil
	}
	return "", fmt.Errorf("unknown schedule_type %q", sc.ScheduleType)
}

func validDaysCSV(s string) bool {
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if len(p) != 1 || p[0] < '0' || p[0] > '6' {
			return false
		}
	}
	return true
}

// PreviewNext returns the next fire times for an unsaved schedule (used by UI preview).
func PreviewNext(sc *store.Schedule, count int) ([]time.Time, error) {
	loc, err := parseLocation(sc.Timezone)
	if err != nil {
		return nil, err
	}
	if sc.ScheduleType == "once" {
		if sc.RunAt == nil {
			return nil, fmt.Errorf("run_at required")
		}
		return []time.Time{sc.RunAt.In(loc)}, nil
	}
	expr, err := buildCronExpr(sc)
	if err != nil {
		return nil, err
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, count)
	t := time.Now().In(loc)
	for i := 0; i < count; i++ {
		t = sched.Next(t)
		out = append(out, t)
	}
	return out, nil
}
