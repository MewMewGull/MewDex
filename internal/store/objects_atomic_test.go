package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestObjectCheckpointRollsBackOnSerializationFailure(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.PutObjects(Object{Bucket: "cursor", ID: "chat", Value: 100}, Object{Bucket: "outbox", ID: "reply", Value: make(chan int)})
	if err == nil {
		t.Fatal("unsupported checkpoint value accepted")
	}
	var cursor int
	if s.Get("cursor", "chat", &cursor) != ErrNotFound {
		t.Fatal("event cursor advanced without its corresponding outbox")
	}
}

func TestPendingConversationsAreFairAndKeepTheirOwnOrder(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for index := 0; index < 150; index++ {
		value := map[string]any{"bindingId": "busy", "state": "pending", "createdAt": fmt.Sprintf("%04d", index)}
		if err = s.Put("inbox", fmt.Sprintf("busy-%d", index), value); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Put("inbox", "other-chat", map[string]any{"bindingId": "other", "state": "pending", "createdAt": "9999"}); err != nil {
		t.Fatal(err)
	}
	values, err := s.PendingObjects("inbox", 2)
	if err != nil || len(values) != 2 {
		t.Fatal("a busy conversation starved other inputs")
	}
	var first, second map[string]any
	_ = json.Unmarshal(values[0], &first)
	_ = json.Unmarshal(values[1], &second)
	if first["createdAt"] != "0000" || second["bindingId"] != "other" {
		t.Fatal("conversation heads are not ordered fairly")
	}
}
