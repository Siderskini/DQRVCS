package gossip

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pendingPushesFileName = "pending_pushes.json"

	PendingPushStatusPending   = "pending"
	PendingPushStatusFailed    = "failed"
	PendingPushStatusCompleted = "completed"
)

// PendingPush tracks a push waiting for certification.
type PendingPush struct {
	ProposalID  string   `json:"proposal_id"`
	Remote      string   `json:"remote"`
	SourceRef   string   `json:"source_ref"`
	TargetRef   string   `json:"target_ref"`
	NewOID      string   `json:"new_oid"`
	GitArgs     []string `json:"git_args"`
	Status      string   `json:"status"`
	Attempts    int      `json:"attempts"`
	LastError   string   `json:"last_error,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	LastTriedAt string   `json:"last_tried_at,omitempty"`
	CompletedAt string   `json:"completed_at,omitempty"`
}

type pendingPushFile struct {
	Pushes []PendingPush `json:"pushes"`
}

func (s *Store) pendingPushesPath() string {
	return filepath.Join(s.dir, pendingPushesFileName)
}

func normalizePendingPush(push PendingPush, forWrite bool) (PendingPush, error) {
	push.ProposalID = strings.TrimSpace(push.ProposalID)
	push.Remote = strings.TrimSpace(push.Remote)
	push.SourceRef = strings.TrimSpace(push.SourceRef)
	push.TargetRef = strings.TrimSpace(push.TargetRef)
	push.NewOID = strings.TrimSpace(push.NewOID)
	push.LastError = strings.TrimSpace(push.LastError)

	if push.ProposalID == "" {
		return push, errors.New("pending push proposal ID is required")
	}
	if push.TargetRef == "" {
		return push, errors.New("pending push target ref is required")
	}
	if push.Status == "" {
		push.Status = PendingPushStatusPending
	}
	switch push.Status {
	case PendingPushStatusPending, PendingPushStatusFailed, PendingPushStatusCompleted:
	default:
		return push, fmt.Errorf("invalid pending push status %q", push.Status)
	}
	if push.GitArgs == nil {
		push.GitArgs = []string{}
	}
	if forWrite {
		if push.CreatedAt == "" {
			push.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		push.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return push, nil
}

func (s *Store) loadPendingPushesLocked() ([]PendingPush, error) {
	data, err := os.ReadFile(s.pendingPushesPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []PendingPush{}, nil
		}
		return nil, fmt.Errorf("read pending pushes: %w", err)
	}

	var file pendingPushFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode pending pushes: %w", err)
	}
	pushes := make([]PendingPush, 0, len(file.Pushes))
	for _, push := range file.Pushes {
		normalized, err := normalizePendingPush(push, false)
		if err != nil {
			return nil, err
		}
		pushes = append(pushes, normalized)
	}
	sort.SliceStable(pushes, func(i, j int) bool {
		if pushes[i].CreatedAt != pushes[j].CreatedAt {
			return pushes[i].CreatedAt < pushes[j].CreatedAt
		}
		return pushes[i].ProposalID < pushes[j].ProposalID
	})
	return pushes, nil
}

func (s *Store) savePendingPushesLocked(pushes []PendingPush) error {
	return writeJSONAtomic(s.pendingPushesPath(), pendingPushFile{Pushes: pushes}, 0o644)
}

// ListPendingPushes returns pending push records in creation order.
func (s *Store) ListPendingPushes() ([]PendingPush, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pushes, err := s.loadPendingPushesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]PendingPush, len(pushes))
	copy(out, pushes)
	return out, nil
}

// UpsertPendingPush inserts or updates a pending push by proposal ID.
func (s *Store) UpsertPendingPush(push PendingPush) (PendingPush, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	push, err := normalizePendingPush(push, true)
	if err != nil {
		return PendingPush{}, err
	}

	pushes, err := s.loadPendingPushesLocked()
	if err != nil {
		return PendingPush{}, err
	}

	replaced := false
	for i, existing := range pushes {
		if existing.ProposalID != push.ProposalID {
			continue
		}
		push.CreatedAt = existing.CreatedAt
		if push.Attempts < existing.Attempts {
			push.Attempts = existing.Attempts
		}
		pushes[i] = push
		replaced = true
		break
	}
	if !replaced {
		pushes = append(pushes, push)
	}
	if err := s.savePendingPushesLocked(pushes); err != nil {
		return PendingPush{}, err
	}
	return push, nil
}

func (s *Store) updatePendingPushStatus(proposalID string, status string, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return errors.New("proposal ID is required")
	}

	pushes, err := s.loadPendingPushesLocked()
	if err != nil {
		return err
	}

	updated := false
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, push := range pushes {
		if push.ProposalID != proposalID {
			continue
		}
		push.Status = status
		push.LastError = strings.TrimSpace(lastError)
		push.UpdatedAt = now
		push.LastTriedAt = now
		push.Attempts++
		if status == PendingPushStatusCompleted {
			push.CompletedAt = now
			push.LastError = ""
		}
		pushes[i] = push
		updated = true
		break
	}
	if !updated {
		return fmt.Errorf("pending push for proposal %q not found", proposalID)
	}
	return s.savePendingPushesLocked(pushes)
}

// MarkPendingPushPending marks a pending push as waiting for quorum.
func (s *Store) MarkPendingPushPending(proposalID string, message string) error {
	return s.updatePendingPushStatus(proposalID, PendingPushStatusPending, message)
}

// MarkPendingPushFailed marks a pending push as failed.
func (s *Store) MarkPendingPushFailed(proposalID string, message string) error {
	return s.updatePendingPushStatus(proposalID, PendingPushStatusFailed, message)
}

// MarkPendingPushCompleted marks a pending push as successfully applied.
func (s *Store) MarkPendingPushCompleted(proposalID string) error {
	return s.updatePendingPushStatus(proposalID, PendingPushStatusCompleted, "")
}
