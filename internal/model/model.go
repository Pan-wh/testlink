package model

import "time"

// ── Target ──

type Target struct {
	ID            uint64    `json:"id"`
	Name          string    `json:"name"`
	GroupName     string    `json:"group_name"`
	Role          string    `json:"role"`
	URL           string    `json:"url"`
	Method        string    `json:"method"`
	Mode          string    `json:"mode"`
	TimeoutMS     uint32    `json:"timeout_ms"`
	RepeatCount   uint8     `json:"repeat_count"`
	CacheBust     uint8     `json:"cache_bust"`
	ExtractRule   string    `json:"extract_rule,omitempty"`
	PlayerVisible uint8     `json:"player_visible"`
	DisplayOrder  uint32    `json:"display_order"`
	Enabled       uint8     `json:"enabled"`
	Note          string    `json:"note,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	Version       uint64    `json:"version"`
}

// ── Session ──

type Session struct {
	SessionID      string    `json:"session_id"`
	CreatedAt      time.Time `json:"created_at"`
	PlayerIP       string    `json:"player_ip"`
	Country        string    `json:"country"`
	Province       string    `json:"province"`
	City           string    `json:"city"`
	ISP            string    `json:"isp"`
	ASN            string    `json:"asn"`
	UA             string    `json:"ua"`
	Browser        string    `json:"browser"`
	OS             string    `json:"os"`
	NetType        string    `json:"net_type"`
	NetDownlink    string    `json:"net_downlink"`
	Game           string    `json:"game"`
	Server         string    `json:"server"`
	Ticket         string    `json:"ticket"`
	Symptom        string    `json:"symptom"`
	Note           string    `json:"note"`
	Verdict        string    `json:"verdict"`
	VerdictDetail  string    `json:"verdict_detail"`
	ConfigSnapshot string    `json:"config_snapshot"`
	Version        uint64    `json:"version"`
}

// ── ProbeResult ──

type ProbeResult struct {
	SessionID   string    `json:"session_id"`
	TargetID    uint64    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	GroupName   string    `json:"group_name"`
	Role        string    `json:"role"`
	URL         string    `json:"url"`
	Host        string    `json:"host"`
	Port        uint16    `json:"port"`
	AttemptNo   uint8     `json:"attempt_no"`
	Outcome     string    `json:"outcome"`
	HTTPStatus  *uint16   `json:"http_status,omitempty"`
	TotalMS     uint16    `json:"total_ms"`
	ColdMS      *uint16   `json:"cold_ms,omitempty"`
	DNSMS       *uint16   `json:"dns_ms,omitempty"`
	TCPMS       *uint16   `json:"tcp_ms,omitempty"`
	TLSMS       *uint16   `json:"tls_ms,omitempty"`
	TTFBMS      *uint16   `json:"ttfb_ms,omitempty"`
	ResolvedIP  *string   `json:"resolved_ip,omitempty"`
	ResolvedGeo *string   `json:"resolved_geo,omitempty"`
	RespHeaders string    `json:"resp_headers,omitempty"`
	RespBody    string    `json:"resp_body,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	PlayerIP    string    `json:"player_ip"`
	Country     string    `json:"country"`
	Province    string    `json:"province"`
	City        string    `json:"city"`
	ISP         string    `json:"isp"`
	ASN         string    `json:"asn"`
	Game        string    `json:"game"`
	Server      string    `json:"server"`
}

// ── API DTOs ──

type SessionResponse struct {
	SessionID string     `json:"session_id"`
	Player    PlayerInfo `json:"player"`
	Targets   []Target   `json:"targets"`
}

type PlayerInfo struct {
	IP       string `json:"ip"`
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	ISP      string `json:"isp"`
	ASN      string `json:"asn"`
}

type ReportRequest struct {
	SessionID   string         `json:"session_id"`
	NetType     string         `json:"net_type"`
	NetDownlink string         `json:"net_downlink"`
	Results     []ReportResult `json:"results"`
}

type ReportResult struct {
	TargetID    uint64  `json:"target_id"`
	AttemptNo   uint8   `json:"attempt_no"`
	Outcome     string  `json:"outcome"`
	HTTPStatus  *uint16 `json:"http_status,omitempty"`
	TotalMS     uint16  `json:"total_ms"`
	ColdMS      *uint16 `json:"cold_ms,omitempty"`
	DNSMS       *uint16 `json:"dns_ms,omitempty"`
	TCPMS       *uint16 `json:"tcp_ms,omitempty"`
	TLSMS       *uint16 `json:"tls_ms,omitempty"`
	TTFBMS      *uint16 `json:"ttfb_ms,omitempty"`
	ResolvedIP  *string `json:"resolved_ip,omitempty"`
	RespHeaders string  `json:"resp_headers,omitempty"`
	RespBody    string  `json:"resp_body,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type ReportResponse struct {
	Verdict       string `json:"verdict"`
	VerdictDetail string `json:"verdict_detail"`
}

// ── Geo ──

type GeoInfo struct {
	Country  string
	Province string
	City     string
	ISP      string
	ASN      string
}
