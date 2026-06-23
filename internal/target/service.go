package target

import (
	"context"
	"fmt"
	"time"

	"testlink/internal/model"
	"testlink/internal/store"
)

type Service struct {
	ch *store.ClickHouse
}

func New(ch *store.ClickHouse) *Service {
	return &Service{ch: ch}
}

// ForPlayers returns enabled & player-visible targets.
func (s *Service) ForPlayers(ctx context.Context) ([]model.Target, error) {
	return s.ch.GetEnabledTargets(ctx)
}

// All returns all targets (for admin).
func (s *Service) All(ctx context.Context) ([]model.Target, error) {
	return s.ch.GetAllTargets(ctx)
}

// Create inserts a new target with auto-incremented ID.
func (s *Service) Create(ctx context.Context, t model.Target) error {
	maxID, err := s.ch.GetMaxTargetID(ctx)
	if err != nil {
		return err
	}
	t.ID = maxID + 1
	t.Version = 1
	t.UpdatedAt = time.Now()
	return s.ch.InsertTarget(ctx, t)
}

// Update inserts a new version of the target.
func (s *Service) Update(ctx context.Context, t model.Target) error {
	existing, err := s.ch.GetAllTargets(ctx)
	if err != nil {
		return err
	}
	var cur *model.Target
	for i := range existing {
		if existing[i].ID == t.ID {
			cur = &existing[i]
			break
		}
	}
	if cur == nil {
		return fmt.Errorf("target %d not found", t.ID)
	}
	// Preserve fields not sent by admin form
	if t.Method == "" {
		t.Method = cur.Method
	}
	if t.URL == "" {
		t.URL = cur.URL
	}
	if t.Name == "" {
		t.Name = cur.Name
	}
	if t.Mode == "" {
		t.Mode = cur.Mode
	}
	if t.TimeoutMS == 0 {
		t.TimeoutMS = cur.TimeoutMS
	}
	if t.RepeatCount == 0 {
		t.RepeatCount = cur.RepeatCount
	}
	if t.GroupName == "" {
		t.GroupName = cur.GroupName
	}
	if t.Role == "" {
		t.Role = cur.Role
	}
	if t.DisplayOrder == 0 && cur.DisplayOrder != 0 {
		t.DisplayOrder = cur.DisplayOrder
	}
	if t.Enabled == 0 {
		t.Enabled = cur.Enabled
	}
	t.Version = cur.Version + 1
	t.UpdatedAt = time.Now()
	return s.ch.InsertTarget(ctx, t)
}

// Delete disables the target (soft delete via new version with enabled=0).
func (s *Service) Delete(ctx context.Context, id uint64) error {
	existing, err := s.ch.GetAllTargets(ctx)
	if err != nil {
		return err
	}
	var cur *model.Target
	for i := range existing {
		if existing[i].ID == id {
			cur = &existing[i]
			break
		}
	}
	if cur == nil {
		return fmt.Errorf("target %d not found", id)
	}
	cur.Enabled = 0
	cur.Version++
	cur.UpdatedAt = time.Now()
	return s.ch.InsertTarget(ctx, *cur)
}
