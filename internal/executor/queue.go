package executor

import (
	"context"
	"errors"
	"sort"
	"time"

	"mewdex/internal/policy"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (x *Executor) recordQueue(item *protocol.QueueItem) error {
	stored, event, err := x.store.PutQueueItem(*item)
	if err != nil {
		return err
	}
	*item = stored
	x.publish(event)
	return nil
}

func (x *Executor) enqueue(s protocol.Session, c protocol.Command) (protocol.QueueItem, error) {
	if err := x.validateInput(s.ProjectID, c.Input); err != nil {
		return protocol.QueueItem{}, err
	}
	if c.Policy != nil {
		if c.ExpectedPolicyVersion != "" && c.ExpectedPolicyVersion != s.PolicyVersion {
			return protocol.QueueItem{}, errors.New("policy changed on another input; refresh before enqueueing")
		}
		p, err := policy.Normalize(*c.Policy)
		if err != nil {
			return protocol.QueueItem{}, err
		}
		if string(protocol.JSON(p)) != string(protocol.JSON(s.Policy)) {
			setPolicy(&s, p)
		}
	}
	item := protocol.QueueItem{ID: c.ID, DeviceID: s.DeviceID, SessionID: s.ID, ProjectID: s.ProjectID, SourceID: c.SourceID, Revision: 1, State: "queued", Input: c.Input, Policy: s.Policy, PolicyVersion: s.PolicyVersion, PolicyInstructions: s.PolicyInstructions, CreatedAt: protocol.Now(), UpdatedAt: protocol.Now()}
	err := x.recordQueue(&item)
	return item, err
}

func (x *Executor) cancelQueued(s protocol.Session, c protocol.Command) (protocol.QueueItem, error) {
	var item protocol.QueueItem
	if err := x.store.Get("queue_items", c.RequestID, &item); err != nil || item.SessionID != s.ID {
		return item, errors.New("queue item not found in this conversation")
	}
	if c.ExpectedRevision < 1 || c.ExpectedRevision != item.Revision {
		return item, errors.New("queue item changed; refresh before cancelling")
	}
	if item.State != "queued" && item.State != "failed" {
		return item, errors.New("only waiting or failed entries can be cancelled; stop an active task first")
	}
	item.State, item.UpdatedAt, item.Revision = "cancelled", protocol.Now(), item.Revision+1
	err := x.recordQueue(&item)
	return item, err
}

func (x *Executor) recoverQueue(sessions []protocol.Session) error {
	// Migrate the early development queue once, preserving its input. Those
	// records did not capture effective instructions, so freeze before dispatch.
	legacy, err := store.List[protocol.Command](x.store, "queue")
	if err != nil {
		return err
	}
	sort.Slice(legacy, func(i, j int) bool {
		if legacy[i].CreatedAt == legacy[j].CreatedAt {
			return legacy[i].ID < legacy[j].ID
		}
		return legacy[i].CreatedAt < legacy[j].CreatedAt
	})
	for _, c := range legacy {
		var existing protocol.QueueItem
		if err = x.store.Get("queue_items", c.ID, &existing); errors.Is(err, store.ErrNotFound) {
			var s protocol.Session
			if err = x.store.Get("sessions", c.SessionID, &s); err != nil {
				return err
			}
			// Preserve even missing attachments. Validation belongs at dispatch,
			// not at recovery where it could make the entire executor unbootable.
			item := protocol.QueueItem{ID: c.ID, DeviceID: s.DeviceID, SessionID: s.ID, ProjectID: s.ProjectID, SourceID: c.SourceID, Revision: 1, State: "queued", Input: c.Input, Policy: s.Policy, PolicyVersion: s.PolicyVersion, PolicyInstructions: s.PolicyInstructions, CreatedAt: protocol.Now(), UpdatedAt: protocol.Now()}
			if err = x.recordQueue(&item); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err = x.store.Delete("queue", c.SessionID+":"+c.ID); err != nil {
			return err
		}
	}
	for _, s := range sessions {
		items, err := x.store.QueueItems(s.ID)
		if err != nil {
			return err
		}
		unfinished := false
		for _, item := range items {
			if item.State == "dispatching" || item.State == "running" {
				item.State, item.Error = "unknown", "executor restarted after dispatch; no automatic replay"
				item.Revision++
				item.UpdatedAt = protocol.Now()
			}
			unfinished = unfinished || item.State == "queued" || item.State == "unknown"
			if err = x.recordQueue(&item); err != nil {
				return err
			}
		}
		if unfinished {
			// Re-read startup's normalized state rather than replacing it with
			// an older snapshot from the caller.
			if err = x.store.Get("sessions", s.ID, &s); err != nil {
				return err
			}
			s.Frozen = true
			for _, item := range items {
				if item.State == "dispatching" || item.State == "running" || item.State == "unknown" {
					s.State, s.StopStatus = "unknown", "unconfirmed"
					if s.ExecutionRecovery == nil || s.ExecutionRecovery.State != "retired" {
						x.restoreProjectClaim(s)
					}
				}
			}
			if err = x.save(&s); err != nil {
				return err
			}
		}
	}
	return nil
}

func (x *Executor) observeQueue(session, turn, state, detail string) error {
	if turn == "" {
		return nil
	}
	items, err := x.store.QueueItems(session)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.State != "running" && item.State != "unknown" && item.State != "dispatching" {
			continue
		}
		if item.TurnID != "" && item.TurnID != turn {
			continue
		}
		if state != "running" && item.TurnID == "" {
			continue // An unbound completion cannot resolve an uncertain dispatch.
		}
		if item.State == state && item.TurnID == turn {
			return nil
		}
		item.State, item.TurnID, item.Error = state, turn, detail
		item.Revision++
		item.UpdatedAt = protocol.Now()
		return x.recordQueue(&item)
	}
	return nil
}

func (x *Executor) advance(session string) {
	x.mu.Lock()
	if x.closing {
		x.mu.Unlock()
		return
	}
	x.active.Add(1)
	x.mu.Unlock()
	defer x.active.Done()
	l := x.lock(session)
	l.Lock()
	defer l.Unlock()
	var s protocol.Session
	if x.store.Get("sessions", session, &s) != nil || s.Frozen || s.State != "idle" || s.StopStatus == "background-running" || s.StopStatus == "processes-unchecked" {
		return
	}
	items, err := x.store.QueueItems(session)
	if err != nil {
		return
	}
	for _, item := range items {
		if item.State == "dispatching" || item.State == "running" || item.State == "unknown" {
			return
		}
	}
	for _, item := range items {
		if item.State != "queued" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		ctx, finish, operationErr := x.operation(ctx, session)
		if operationErr != nil {
			cancel()
			return
		}
		defer cancel()
		defer finish()
		item.State, item.UpdatedAt, item.Revision = "dispatching", protocol.Now(), item.Revision+1
		if err = x.recordQueue(&item); err != nil {
			return
		}
		s.Policy, s.PolicyVersion, s.PolicyInstructions = item.Policy, item.PolicyVersion, item.PolicyInstructions
		c := protocol.Command{ID: item.ID, DeviceID: item.DeviceID, SessionID: session, SourceID: item.SourceID, Kind: "start", Input: item.Input, CreatedAt: item.CreatedAt}
		err = x.start(ctx, &s, c)
		if err != nil {
			item.State, item.Error = "failed", err.Error()
			if s.State == "unknown" || s.State == "running" {
				item.State = "unknown"
			}
			s.Frozen = true
			if item.State == "unknown" {
				s.State, s.StopStatus = "unknown", "unconfirmed"
			}
			_ = x.save(&s)
		} else {
			item.State, item.TurnID = "running", s.TurnID
		}
		item.Revision++
		item.UpdatedAt = protocol.Now()
		if e := x.recordQueue(&item); e != nil {
			// Dispatch already happened. Keep the pre-dispatch record and freeze
			// instead of advancing another input on incomplete durable evidence.
			s.State, s.Frozen, s.StopStatus = "unknown", true, "unconfirmed"
			_ = x.save(&s)
		}
		return
	}
}
