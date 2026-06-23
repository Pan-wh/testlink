package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"testlink/internal/geoip"
	"testlink/internal/model"
	"testlink/internal/store"
)

const base32Chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type Service struct {
	ch  *store.ClickHouse
	geo *geoip.Service
}

func New(ch *store.ClickHouse, geo *geoip.Service) *Service {
	return &Service{ch: ch, geo: geo}
}

func (s *Service) CreateSession(ctx context.Context, playerIP, ua, game, server, ticket string, targets []model.Target) (*model.Session, error) {
	gi := s.geo.Lookup(playerIP)
	sessionID := newSessionID()

	for {
		exists, err := s.ch.SessionExists(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if !exists {
			break
		}
		sessionID = newSessionID()
	}

	b, o := parseUA(ua)
	snapshot, _ := json.Marshal(targets)

	sess := &model.Session{
		SessionID:      sessionID,
		CreatedAt:      time.Now(),
		PlayerIP:       playerIP,
		Country:        gi.Country,
		Province:       gi.Province,
		City:           gi.City,
		ISP:            gi.ISP,
		ASN:            gi.ASN,
		UA:             ua,
		Browser:        b,
		OS:             o,
		Game:           game,
		Server:         server,
		Ticket:         ticket,
		ConfigSnapshot: string(snapshot),
		Version:        1,
	}
	if err := s.ch.InsertSession(ctx, *sess); err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

func (s *Service) Get(ctx context.Context, sessionID string) (*model.Session, error) {
	return s.ch.GetSession(ctx, sessionID)
}

func (s *Service) Query(ctx context.Context, playerIP, isp, province, game, ticket string, since time.Time, page, pageSize int) ([]model.Session, int, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	sessions, total, err := s.ch.QuerySessions(ctx, playerIP, isp, province, game, ticket, since, pageSize, offset)
	return sessions, int(total), err
}

func (s *Service) UpdateVerdict(ctx context.Context, sessionID, verdict, detail string) error {
	cur, err := s.ch.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	cur.Verdict = verdict
	cur.VerdictDetail = detail
	cur.Version++
	return s.ch.InsertSession(ctx, *cur)
}

func (s *Service) UpdateNote(ctx context.Context, sessionID, note, symptom string) error {
	cur, err := s.ch.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	cur.Note = note
	if symptom != "" {
		cur.Symptom = symptom
	}
	cur.Version++
	return s.ch.InsertSession(ctx, *cur)
}

// ── helpers ──

func newSessionID() string {
	prefix := time.Now().Format("0102")
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(base32Chars))))
		b[i] = base32Chars[n.Int64()]
	}
	return prefix + "-" + string(b)
}

func parseUA(ua string) (browser, os string) {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "edg"):
		browser = "Edge"
	case strings.Contains(lower, "chrome"):
		browser = "Chrome"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	default:
		browser = "Other"
	}
	switch {
	case strings.Contains(lower, "windows"):
		os = "Windows"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os"):
		os = "macOS"
	case strings.Contains(lower, "linux"):
		os = "Linux"
	case strings.Contains(lower, "android"):
		os = "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"):
		os = "iOS"
	default:
		os = "Other"
	}
	return
}
