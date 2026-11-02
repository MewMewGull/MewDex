package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

// A few explicit, one-shot file signals exercise real transport and native
// notifications without pretending that a model completed a real task. Only
// this labelled fixture session can receive these synthetic events.
func fixtureAlerts(s *store.Store, root, instance, device, session string) error {
	for number := 1; number <= 4; number++ {
		name := fmt.Sprintf("notify-completed-%s-%d", instance, number)
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != 0 {
			return errors.New("notification fixture expects an empty regular signal file")
		}
		intent, err := os.OpenFile(path+".armed", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			continue // An uncertain result is observed, never automatically replayed.
		}
		if err != nil {
			return err
		}
		if err = intent.Sync(); err != nil {
			intent.Close()
			return err
		}
		if err = intent.Close(); err != nil {
			return err
		}
		event, err := s.Append(protocol.Event{ID: eventID(name), DeviceID: device, SessionID: session,
			Kind: "engine.turn/completed", CreatedAt: protocol.Now(),
			Payload: protocol.JSON(map[string]any{"turn": map[string]string{"id": name, "status": "completed"}, "synthetic": true, "source": "labelled no-model UI fixture"})})
		if err != nil {
			return err
		}
		receipt, err := os.OpenFile(path+".receipt.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = json.NewEncoder(receipt).Encode(map[string]any{"state": "appended", "eventId": event.ID, "sequence": event.Sequence, "synthetic": true, "modelCalls": 0})
		closeErr := receipt.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
