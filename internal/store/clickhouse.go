package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"testlink/internal/model"
)

// ClickHouse wraps the native connection and provides query helpers.
type ClickHouse struct {
	conn clickhouse.Conn
}

func New(host string, port int, database, username, password string) (*ClickHouse, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", host, port)},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		DialTimeout: 5 * time.Second,
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse connect: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return &ClickHouse{conn: conn}, nil
}

func (ch *ClickHouse) Conn() clickhouse.Conn { return ch.conn }

// Init creates tables and seeds default targets if the target table is empty.
func (ch *ClickHouse) Init(ctx context.Context) error {
	for _, ddl := range []string{ddlTarget, ddlSession, ddlProbeResult} {
		if err := ch.conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("ddl: %w", err)
		}
	}
	// Ensure new columns exist on existing tables (safe no-op if already present)
	for _, alt := range []string{
		`ALTER TABLE tl_probe_result ADD COLUMN IF NOT EXISTS resp_headers String DEFAULT ''`,
			`ALTER TABLE tl_probe_result ADD COLUMN IF NOT EXISTS resolved_geo Nullable(String)`,
		`ALTER TABLE tl_probe_result ADD COLUMN IF NOT EXISTS resp_body String DEFAULT ''`,
	} {
		if err := ch.conn.Exec(ctx, alt); err != nil {
			log.Printf("alter table (non-fatal): %v", err)
		}
	}

	// Seed default targets
	var cnt uint64
	if err := ch.conn.QueryRow(ctx, "SELECT count() FROM tl_target").Scan(&cnt); err != nil {
		return fmt.Errorf("count targets: %w", err)
	}
	if cnt == 0 {
		for _, t := range defaultTargets() {
			if err := ch.InsertTarget(ctx, t); err != nil {
				return fmt.Errorf("seed target %q: %w", t.Name, err)
			}
		}
	}
	return nil
}

// ── Target ──

func (ch *ClickHouse) InsertTarget(ctx context.Context, t model.Target) error {
	return ch.conn.Exec(ctx,
		`INSERT INTO tl_target (id,name,group_name,role,url,method,mode,timeout_ms,repeat_count,cache_bust,extract_rule,player_visible,display_order,enabled,note,updated_at,version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.GroupName, t.Role, t.URL, t.Method, t.Mode,
		t.TimeoutMS, t.RepeatCount, t.CacheBust, t.ExtractRule,
		t.PlayerVisible, t.DisplayOrder, t.Enabled, t.Note, t.UpdatedAt, t.Version,
	)
}

func (ch *ClickHouse) GetEnabledTargets(ctx context.Context) ([]model.Target, error) {
	rows, err := ch.conn.Query(ctx,
		`SELECT id,name,group_name,role,url,method,mode,timeout_ms,repeat_count,cache_bust,extract_rule,player_visible,display_order,enabled,note,updated_at,version
		 FROM tl_target ORDER BY id, version DESC`)
	if err != nil {
		return nil, err
	}
	seen := make(map[uint64]bool)
	var targets []model.Target
	for rows.Next() {
		var t model.Target
		if err := rows.Scan(&t.ID, &t.Name, &t.GroupName, &t.Role, &t.URL, &t.Method, &t.Mode,
			&t.TimeoutMS, &t.RepeatCount, &t.CacheBust, &t.ExtractRule,
			&t.PlayerVisible, &t.DisplayOrder, &t.Enabled, &t.Note, &t.UpdatedAt, &t.Version); err != nil {
			return nil, err
		}
		if !seen[t.ID] {
			seen[t.ID] = true
			if t.Enabled == 1 && t.PlayerVisible == 1 {
				targets = append(targets, t)
			}
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].DisplayOrder < targets[j].DisplayOrder })
	return targets, nil
}

func (ch *ClickHouse) GetAllTargets(ctx context.Context) ([]model.Target, error) {
	// Order by id, version DESC so latest version appears first per id
	rows, err := ch.conn.Query(ctx,
		`SELECT id,name,group_name,role,url,method,mode,timeout_ms,repeat_count,cache_bust,extract_rule,player_visible,display_order,enabled,note,updated_at,version
		 FROM tl_target ORDER BY id, version DESC`)
	if err != nil {
		return nil, err
	}
	// Deduplicate in Go: keep only first (latest version) for each id
	seen := make(map[uint64]bool)
	var targets []model.Target
	for rows.Next() {
		var t model.Target
		if err := rows.Scan(&t.ID, &t.Name, &t.GroupName, &t.Role, &t.URL, &t.Method, &t.Mode,
			&t.TimeoutMS, &t.RepeatCount, &t.CacheBust, &t.ExtractRule,
			&t.PlayerVisible, &t.DisplayOrder, &t.Enabled, &t.Note, &t.UpdatedAt, &t.Version); err != nil {
			return nil, err
		}
		if !seen[t.ID] {
			seen[t.ID] = true
			targets = append(targets, t)
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	// Sort by display_order
	sort.Slice(targets, func(i, j int) bool { return targets[i].DisplayOrder < targets[j].DisplayOrder })
	return targets, nil
}

func (ch *ClickHouse) GetMaxTargetID(ctx context.Context) (uint64, error) {
	var maxID uint64
	err := ch.conn.QueryRow(ctx, "SELECT max(id) FROM tl_target").Scan(&maxID)
	return maxID, err
}

// ── Session ──

func (ch *ClickHouse) InsertSession(ctx context.Context, s model.Session) error {
	return ch.conn.Exec(ctx,
		`INSERT INTO tl_session (session_id,created_at,player_ip,country,province,city,isp,asn,ua,browser,os,net_type,net_downlink,game,server,ticket,symptom,note,verdict,verdict_detail,config_snapshot,version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.SessionID, s.CreatedAt, s.PlayerIP, s.Country, s.Province, s.City, s.ISP, s.ASN,
		s.UA, s.Browser, s.OS, s.NetType, s.NetDownlink,
		s.Game, s.Server, s.Ticket, s.Symptom, s.Note, s.Verdict, s.VerdictDetail, s.ConfigSnapshot, s.Version,
	)
}

func (ch *ClickHouse) GetSession(ctx context.Context, sessionID string) (*model.Session, error) {
	var s model.Session
	err := ch.conn.QueryRow(ctx,
		`SELECT session_id,created_at,player_ip,country,province,city,isp,asn,ua,browser,os,net_type,net_downlink,game,server,ticket,symptom,note,verdict,verdict_detail,config_snapshot,version
		 FROM tl_session WHERE session_id=? ORDER BY version DESC LIMIT 1`, sessionID,
	).Scan(&s.SessionID, &s.CreatedAt, &s.PlayerIP, &s.Country, &s.Province, &s.City, &s.ISP, &s.ASN,
		&s.UA, &s.Browser, &s.OS, &s.NetType, &s.NetDownlink,
		&s.Game, &s.Server, &s.Ticket, &s.Symptom, &s.Note, &s.Verdict, &s.VerdictDetail, &s.ConfigSnapshot, &s.Version)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (ch *ClickHouse) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	var cnt uint64
	if err := ch.conn.QueryRow(ctx, "SELECT count() FROM tl_session WHERE session_id=?", sessionID).Scan(&cnt); err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (ch *ClickHouse) QuerySessions(ctx context.Context, playerIP, isp, province, game, ticket string, since time.Time, limit, offset int) ([]model.Session, uint64, error) {
	whereClause := `WHERE 1=1
	   AND (player_ip=? OR ?='')
	   AND (isp=? OR ?='')
	   AND (province=? OR ?='')
	   AND (game=? OR ?='')
	   AND (ticket=? OR ?='')
	   AND (created_at>=? OR ?=toDateTime(0))`
	args := []any{playerIP, playerIP, isp, isp, province, province, game, game, ticket, ticket, since, since}

	// Total count
	var total uint64
	if err := ch.conn.QueryRow(ctx,
		`SELECT count(DISTINCT session_id) FROM tl_session `+whereClause, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := ch.conn.Query(ctx,
		`SELECT session_id,argMax(created_at,version) ct,argMax(player_ip,version) ip,
		        argMax(country,version),argMax(province,version),argMax(city,version),
		        argMax(isp,version),argMax(asn,version),
		        argMax(ua,version),argMax(browser,version),argMax(os,version),
		        argMax(net_type,version),argMax(net_downlink,version),
		        argMax(game,version),argMax(server,version),argMax(ticket,version),
		        argMax(symptom,version),argMax(note,version),
		        argMax(verdict,version),argMax(verdict_detail,version),
		        argMax(config_snapshot,version),max(version)
		 FROM tl_session `+whereClause+`
		 GROUP BY session_id
		 ORDER BY max(created_at) DESC
		 LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	var sessions []model.Session
	for rows.Next() {
		var s model.Session
		if err := rows.Scan(&s.SessionID, &s.CreatedAt, &s.PlayerIP, &s.Country, &s.Province, &s.City, &s.ISP, &s.ASN,
			&s.UA, &s.Browser, &s.OS, &s.NetType, &s.NetDownlink,
			&s.Game, &s.Server, &s.Ticket, &s.Symptom, &s.Note, &s.Verdict, &s.VerdictDetail, &s.ConfigSnapshot, &s.Version); err != nil {
			return nil, 0, err
		}
		sessions = append(sessions, s)
	}
	return sessions, total, rows.Err()
}

// ── ProbeResult ──

func (ch *ClickHouse) InsertProbeResult(ctx context.Context, r model.ProbeResult) error {
	return ch.conn.Exec(ctx,
		`INSERT INTO tl_probe_result (session_id,target_id,target_name,group_name,role,url,host,port,attempt_no,outcome,http_status,total_ms,cold_ms,dns_ms,tcp_ms,tls_ms,ttfb_ms,resolved_ip,resp_headers,resp_body,resolved_geo,error,created_at,player_ip,country,province,city,isp,asn,game,server)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.SessionID, r.TargetID, r.TargetName, r.GroupName, r.Role, r.URL, r.Host, r.Port,
		r.AttemptNo, r.Outcome, r.HTTPStatus, r.TotalMS, r.ColdMS, r.DNSMS, r.TCPMS, r.TLSMS, r.TTFBMS, r.ResolvedIP,
		r.RespHeaders, r.RespBody, r.ResolvedGeo,
		r.Error, r.CreatedAt,
		r.PlayerIP, r.Country, r.Province, r.City, r.ISP, r.ASN, r.Game, r.Server,
	)
}

func (ch *ClickHouse) GetProbeResults(ctx context.Context, sessionID string) ([]model.ProbeResult, error) {
	rows, err := ch.conn.Query(ctx,
		`SELECT session_id,target_id,target_name,group_name,role,url,host,port,attempt_no,outcome,http_status,total_ms,cold_ms,dns_ms,tcp_ms,tls_ms,ttfb_ms,resolved_ip,resp_headers,resp_body,resolved_geo,error,created_at,player_ip,country,province,city,isp,asn,game,server
		 FROM tl_probe_result WHERE session_id=? ORDER BY target_id, attempt_no`, sessionID)
	if err != nil {
		return nil, err
	}
	var results []model.ProbeResult
	for rows.Next() {
		var r model.ProbeResult
		if err := rows.Scan(&r.SessionID, &r.TargetID, &r.TargetName, &r.GroupName, &r.Role, &r.URL, &r.Host, &r.Port,
			&r.AttemptNo, &r.Outcome, &r.HTTPStatus, &r.TotalMS, &r.ColdMS, &r.DNSMS, &r.TCPMS, &r.TLSMS, &r.TTFBMS, &r.ResolvedIP,
			&r.RespHeaders, &r.RespBody, &r.ResolvedGeo,
			&r.Error, &r.CreatedAt,
			&r.PlayerIP, &r.Country, &r.Province, &r.City, &r.ISP, &r.ASN, &r.Game, &r.Server); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	// Sort by config display_order from session snapshot
	var snap string
	_ = ch.conn.QueryRow(ctx,
		`SELECT config_snapshot FROM tl_session WHERE session_id=? ORDER BY version DESC LIMIT 1`, sessionID,
	).Scan(&snap)
	if snap != "" {
		var targets []model.Target
		if json.Unmarshal([]byte(snap), &targets) == nil {
			order := make(map[uint64]uint32)
			for _, t := range targets {
				order[t.ID] = t.DisplayOrder
			}
			sort.Slice(results, func(i, j int) bool {
				oi, oj := order[results[i].TargetID], order[results[j].TargetID]
				if oi != oj {
					return oi < oj
				}
				return results[i].AttemptNo < results[j].AttemptNo
			})
		}
	}

	return results, nil
}
// ── Stats ──

func (ch *ClickHouse) GetStats(ctx context.Context, since time.Time, groupBy string) (*StatsResult, error) {
	// groupBy: "target" | "isp" | "province"
	sql := fmt.Sprintf(`
		SELECT %s as dim, count() total, countIf(outcome='reachable') ok,
		       quantile(0.5)(total_ms) p50, quantile(0.95)(total_ms) p95,
		       countIf(outcome='timeout') timeouts, countIf(outcome='fast_fail') fastfails
		FROM tl_probe_result
		WHERE created_at >= ?
		GROUP BY dim ORDER BY total DESC`, groupBy)

	rows, err := ch.conn.Query(ctx, sql, since)
	if err != nil {
		return nil, err
	}
	result := &StatsResult{GroupBy: groupBy}
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.Dimension, &r.Total, &r.OK, &r.P50, &r.P95, &r.Timeouts, &r.FastFails); err != nil {
			return nil, err
		}
		result.Rows = append(result.Rows, r)
	}
	return result, rows.Err()
}

type StatsResult struct {
	GroupBy string    `json:"group_by"`
	Rows    []StatRow `json:"rows"`
}

type StatRow struct {
	Dimension string  `json:"dimension"`
	Total     uint64  `json:"total"`
	OK        uint64  `json:"ok"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	Timeouts  uint64  `json:"timeouts"`
	FastFails uint64  `json:"fast_fails"`
}

func (r StatRow) OKRate() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.OK) / float64(r.Total) * 100
}

// ── DDL ──

const ddlTarget = `
CREATE TABLE IF NOT EXISTS tl_target (
    id              UInt64,
    name            String,
    group_name      String,
    role            String,
    url             String,
    method          String DEFAULT 'GET',
    mode            String DEFAULT 'no-cors',
    timeout_ms      UInt32 DEFAULT 5000,
    repeat_count    UInt8 DEFAULT 4,
    cache_bust      UInt8 DEFAULT 0,
    extract_rule    String DEFAULT '',
    player_visible  UInt8 DEFAULT 1,
    display_order   UInt32 DEFAULT 0,
    enabled         UInt8 DEFAULT 1,
    note            String DEFAULT '',
    updated_at      DateTime DEFAULT now(),
    version         UInt64
) ENGINE = ReplacingMergeTree(version) ORDER BY id`

const ddlSession = `
CREATE TABLE IF NOT EXISTS tl_session (
    session_id      String,
    created_at      DateTime DEFAULT now(),
    player_ip       String,
    country         String DEFAULT '',
    province        String DEFAULT '',
    city            String DEFAULT '',
    isp             String DEFAULT '',
    asn             String DEFAULT '',
    ua              String DEFAULT '',
    browser         String DEFAULT '',
    os              String DEFAULT '',
    net_type        String DEFAULT '',
    net_downlink    String DEFAULT '',
    game            String DEFAULT '',
    server          String DEFAULT '',
    ticket          String DEFAULT '',
    symptom         String DEFAULT '',
    note            String DEFAULT '',
    verdict         String DEFAULT '',
    verdict_detail  String DEFAULT '',
    config_snapshot String DEFAULT '',
    version         UInt64
) ENGINE = ReplacingMergeTree(version) ORDER BY session_id`

const ddlProbeResult = `
CREATE TABLE IF NOT EXISTS tl_probe_result (
    session_id      String,
    target_id       UInt64,
    target_name     String,
    group_name      String,
    role            String,
    url             String,
    host            String DEFAULT '',
    port            UInt16 DEFAULT 443,
    attempt_no      UInt8,
    outcome         String,
    http_status     Nullable(UInt16),
    total_ms        UInt16 DEFAULT 0,
    cold_ms         Nullable(UInt16),
    dns_ms          Nullable(UInt16),
    tcp_ms          Nullable(UInt16),
    tls_ms          Nullable(UInt16),
    ttfb_ms         Nullable(UInt16),
    resolved_ip     Nullable(String),
    resp_headers    String DEFAULT '',
    resp_body       String DEFAULT '',
    resolved_geo    Nullable(String),
    error           String DEFAULT '',
    created_at      DateTime DEFAULT now(),
    player_ip       String,
    country         String DEFAULT '',
    province        String DEFAULT '',
    city            String DEFAULT '',
    isp             String DEFAULT '',
    asn             String DEFAULT '',
    game            String DEFAULT '',
    server          String DEFAULT ''
) ENGINE = ReplacingMergeTree(created_at)
  ORDER BY (session_id, target_id, attempt_no)`

func defaultTargets() []model.Target {
	now := time.Now()
	return []model.Target{
		{ID: 1, Name: "Google", GroupName: "基线-国际", Role: "基线", URL: "https://www.google.com/generate_204", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 2, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 0, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 2, Name: "Cloudflare", GroupName: "基线-国际", Role: "基线", URL: "https://www.cloudflare.com/cdn-cgi/trace", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 2, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 1, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 3, Name: "ipinfo", GroupName: "基线-国际", Role: "基线", URL: "https://ipinfo.io/json", Method: "GET", Mode: "cors", TimeoutMS: 5000, RepeatCount: 2, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 2, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 4, Name: "SDK", GroupName: "SDK", Role: "业务", URL: "https://api.example.com/v1/ip", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 4, CacheBust: 0, ExtractRule: "", PlayerVisible: 1, DisplayOrder: 3, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 5, Name: "CDN资源更新", GroupName: "CDN", Role: "业务", URL: "https://cdn.example.com/versionmap.txt", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 4, CacheBust: 1, PlayerVisible: 1, DisplayOrder: 4, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 6, Name: "区服列表", GroupName: "区服", Role: "业务", URL: "https://game.example.com:9001/api/server/list", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 4, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 5, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 7, Name: "ip.sb", GroupName: "基线-国际", Role: "基线", URL: "https://api.ip.sb/geoip", Method: "GET", Mode: "cors", TimeoutMS: 5000, RepeatCount: 2, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 6, Enabled: 1, Version: 1, UpdatedAt: now},
		{ID: 8, Name: "百度", GroupName: "基线-国内", Role: "基线", URL: "https://www.baidu.com", Method: "GET", Mode: "no-cors", TimeoutMS: 5000, RepeatCount: 2, CacheBust: 0, PlayerVisible: 1, DisplayOrder: 7, Enabled: 1, Version: 1, UpdatedAt: now},
	}
}
