package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"testlink/internal/model"
	"testlink/internal/probe"
	"testlink/internal/session"
	"testlink/internal/store"
	"testlink/internal/target"
)

type Handler struct {
	ch     *store.ClickHouse
	sess   *session.Service
	target *target.Service
	probe  *probe.Service
}

func NewHandler(ch *store.ClickHouse, sess *session.Service, target *target.Service, probe *probe.Service) *Handler {
	return &Handler{ch: ch, sess: sess, target: target, probe: probe}
}

// ── Public (player-facing) ──

func (h *Handler) CreateSession(c *gin.Context) {
	ip := c.ClientIP()
	ua := c.GetHeader("User-Agent")
	game := c.Query("g")
	server := c.Query("s")
	ticket := c.Query("t")

	targets, err := h.target.ForPlayers(c)
	if err != nil {
		log.Printf("[session] ERROR get targets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if len(targets) == 0 {
		log.Printf("[session] WARN no targets configured, ip=%s", ip)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no targets configured"})
		return
	}

	tStart := time.Now()
	sess, err := h.sess.CreateSession(c, ip, ua, game, server, ticket, targets)
	if err != nil {
		log.Printf("[session] ERROR create: %v ip=%s", err, ip)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	log.Printf("[session] NEW id=%s ip=%s geo=%s/%s/%s isp=%s ua=%s/%s game=%s server=%s ticket=%s targets=%d latency=%dms",
		sess.SessionID, sess.PlayerIP, sess.Country, sess.Province, sess.City, sess.ISP,
		sess.Browser, sess.OS, sess.Game, sess.Server, sess.Ticket, len(targets), time.Since(tStart).Milliseconds())

	c.JSON(http.StatusOK, model.SessionResponse{
		SessionID: sess.SessionID,
		Player: model.PlayerInfo{
			IP:       sess.PlayerIP,
			Country:  sess.Country,
			Province: sess.Province,
			City:     sess.City,
			ISP:      sess.ISP,
			ASN:      sess.ASN,
		},
		Targets: targets,
	})
}

func (h *Handler) SubmitReport(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 256*1024))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	var req model.ReportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id required"})
		return
	}
	final := c.Query("final") == "true"

	tStart := time.Now()
	resp, err := h.probe.HandleReport(c, req, final)
	if err != nil {
		log.Printf("[report] ERROR id=%s: %v", req.SessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	outcomes := map[string]int{}
	for _, r := range req.Results {
		outcomes[r.Outcome]++
	}
	log.Printf("[report] OK id=%s final=%v results=%d outcomes=%v verdict=%s latency=%dms",
		req.SessionID, final, len(req.Results), outcomes, resp.Verdict, time.Since(tStart).Milliseconds())

	c.JSON(http.StatusOK, resp)
}

// ── Admin ──

func (h *Handler) ListTargets(c *gin.Context) {
	targets, err := h.target.All(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, targets)
}

func (h *Handler) CreateTarget(c *gin.Context) {
	var t model.Target
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.target.Create(c, t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (h *Handler) UpdateTarget(c *gin.Context) {
	var t model.Target
	if err := c.ShouldBindJSON(&t); err != nil {
		log.Printf("[target] UPDATE bind error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.target.Update(c, t); err != nil {
		log.Printf("[target] UPDATE id=%d name=%q error: %v", t.ID, t.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[target] UPDATE id=%d name=%q ok", t.ID, t.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) DeleteTarget(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		log.Printf("[target] DELETE parse id: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	log.Printf("[target] DELETE id=%d", id)
	if err := h.target.Delete(c, id); err != nil {
		log.Printf("[target] DELETE id=%d error: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[target] DELETE id=%d ok", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) ListSessions(c *gin.Context) {
	ip := c.Query("ip")
	isp := c.Query("isp")
	province := c.Query("province")
	game := c.Query("game")
	ticket := c.Query("ticket")
	sinceStr := c.Query("since")
	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	since := time.Time{}
	if sinceStr != "" {
		if t, err := time.Parse("2006-01-02", sinceStr); err == nil {
			since = t
		} else if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		}
	}

	sessions, total, err := h.sess.Query(c, ip, isp, province, game, ticket, since, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetSession(c *gin.Context) {
	id := c.Param("id")
	sess, err := h.sess.Get(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	results, _ := h.probe.GetResults(c, id)
	c.JSON(http.StatusOK, gin.H{"session": sess, "results": results})
}

func (h *Handler) UpdateSessionNote(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Note    string `json:"note"`
		Symptom string `json:"symptom"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.sess.UpdateNote(c, id, req.Note, req.Symptom); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

