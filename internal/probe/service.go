package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"

	"testlink/internal/model"
	"testlink/internal/session"
	"testlink/internal/store"
	"testlink/internal/verdict"
)

type Service struct {
	ch   *store.ClickHouse
	sess *session.Service
}

func New(ch *store.ClickHouse, sess *session.Service) *Service {
	return &Service{ch: ch, sess: sess}
}

// HandleReport writes probe results. Only computes/stores verdict when final=true.
func (s *Service) HandleReport(ctx context.Context, req model.ReportRequest, final bool) (*model.ReportResponse, error) {
	sessRec, err := s.sess.Get(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	// Update net info on session if first time
	if req.NetType != "" && sessRec.NetType == "" {
		sessRec.NetType = req.NetType
		sessRec.NetDownlink = req.NetDownlink
		sessRec.Version++
		if err := s.ch.InsertSession(ctx, *sessRec); err != nil {
			return nil, fmt.Errorf("update net info: %w", err)
		}
	}

	// Decode config snapshot to resolve target metadata
	targets := decodeSnapshot(sessRec.ConfigSnapshot)

	for _, rr := range req.Results {
		t := findTarget(targets, rr.TargetID)
		if t == nil {
			continue // unknown target, skip
		}
		host, port := extractHostPort(t.URL)
		r := model.ProbeResult{
			SessionID:   req.SessionID,
			TargetID:    rr.TargetID,
			TargetName:  t.Name,
			GroupName:   t.GroupName,
			Role:        normalizeRole(t.Role),
			URL:         t.URL,
			Host:        host,
			Port:        port,
			AttemptNo:   rr.AttemptNo,
			Outcome:     rr.Outcome,
			HTTPStatus:  rr.HTTPStatus,
			TotalMS:     rr.TotalMS,
			ColdMS:      rr.ColdMS,
			DNSMS:       rr.DNSMS,
			TCPMS:       rr.TCPMS,
			TLSMS:       rr.TLSMS,
			TTFBMS:      rr.TTFBMS,
			RespHeaders: rr.RespHeaders,
			RespBody:    rr.RespBody,
			Error:       rr.Error,
			CreatedAt:   time.Now(),
			PlayerIP:    sessRec.PlayerIP,
			Country:     sessRec.Country,
			Province:    sessRec.Province,
			City:        sessRec.City,
			ISP:         sessRec.ISP,
			ASN:         sessRec.ASN,
			Game:        sessRec.Game,
			Server:      sessRec.Server,
		}
		if err := s.ch.InsertProbeResult(ctx, r); err != nil {
			return nil, fmt.Errorf("insert probe result: %w", err)
		}
	}

	resp := &model.ReportResponse{}

	// Only compute and store verdict on the final call (skip if already set)
	if final && sessRec.Verdict == "" {
		allResults, err := s.ch.GetProbeResults(ctx, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("get probe results: %w", err)
		}
		code, detail := verdict.Compute(allResults)
		resp.Verdict = code
		resp.VerdictDetail = detail
		if err := s.sess.UpdateVerdict(ctx, req.SessionID, code, detail); err != nil {
			return nil, fmt.Errorf("update verdict: %w", err)
		}
	}

	return resp, nil
}

func (s *Service) GetResults(ctx context.Context, sessionID string) ([]model.ProbeResult, error) {
	return s.ch.GetProbeResults(ctx, sessionID)
}

// ── helpers ──

func decodeSnapshot(raw string) []model.Target {
	var targets []model.Target
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &targets); err != nil {
		log.Printf("[probe] WARN decode config snapshot: %v", err)
		return nil
	}
	return targets
}

func findTarget(targets []model.Target, id uint64) *model.Target {
	for i := range targets {
		if targets[i].ID == id {
			return &targets[i]
		}
	}
	return nil
}

func normalizeRole(r string) string {
	if r == "baseline" {
		return "基线"
	}
	if r == "business" {
		return "业务"
	}
	return r
}

func extractHostPort(rawURL string) (host string, port uint16) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, 443
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "http" {
			return host, 80
		}
		return host, 443
	}
	n, _ := strconv.Atoi(p)
	return host, uint16(n)
}
