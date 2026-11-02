package store

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

func TestMessageSearchUsesCompleteTextAndStablePages(t *testing.T) {
	s := memory(t)
	session := protocol.Session{ID: "conversation", DeviceID: "node", ProjectID: "work"}
	appendEvent := func(device, conversation, kind string, payload any) {
		t.Helper()
		if _, err := s.Append(protocol.Event{ID: protocol.ID(), DeviceID: device, SessionID: conversation, Kind: kind, Payload: protocol.JSON(payload), CreatedAt: protocol.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	user := func(text string) any {
		return map[string]any{"input": []protocol.Input{{Type: "text", Text: text}}, "metadata": "private-metadata-only"}
	}
	answer := func(text string) any {
		return map[string]any{"item": map[string]any{"id": "answer", "type": "agentMessage", "text": text}}
	}
	appendEvent("node", session.ID, "message.user", user("A Word 中文 and literal %_['?]"))
	appendEvent("node", session.ID, "engine.item/completed", answer("old-completion-only"))
	appendEvent("node", session.ID, "engine.item/agentMessage/delta", map[string]string{"itemId": "answer", "delta": "Word"})
	appendEvent("node", session.ID, "engine.item/completed", answer(strings.Repeat("🙂", 70)+"WoRd 中文"+strings.Repeat("后", 120)))
	appendEvent("node", "private", "message.user", user("Word outside conversation"))
	appendEvent("other-node", session.ID, "message.user", user("Word wrong executor"))
	appendEvent("node", session.ID, "engine.item/completed", map[string]any{"item": map[string]string{"id": "tool", "type": "commandExecution", "command": "Word tool-only"}})
	page, err := s.SearchMessages(context.Background(), session, "word", 0, 1)
	if err != nil || len(page.Matches) != 1 || !page.HasMore || page.Matches[0].ID != "answer" || page.Matches[0].Role != "assistant" {
		t.Fatalf("complete message search: %+v %v", page, err)
	}
	excerpt := page.Matches[0].Excerpt
	if excerpt.Match != "WoRd" || !utf8.ValidString(excerpt.Before+excerpt.Match+excerpt.After) || !strings.HasPrefix(excerpt.Before, "…") || !strings.HasSuffix(excerpt.After, "…") {
		t.Fatalf("Unicode excerpt was damaged or unbounded: %+v", excerpt)
	}
	appendEvent("node", session.ID, "message.user", user("Word added after the first page"))
	older, err := s.SearchMessages(context.Background(), session, "WORD", page.Before, 1)
	if err != nil || len(older.Matches) != 1 || older.HasMore || older.Matches[0].Role != "user" || older.Matches[0].Sequence >= page.Before {
		t.Fatalf("concurrent input shifted the older search page: %+v %v", older, err)
	}
	for _, query := range []string{"old-completion-only", "private-metadata-only", "tool-only", "outside conversation", "wrong executor"} {
		found, err := s.SearchMessages(context.Background(), session, query, 0, 30)
		if err != nil || len(found.Matches) != 0 {
			t.Fatalf("search exposed stale or non-message text for %q: %+v %v", query, found, err)
		}
	}
	literal, err := s.SearchMessages(context.Background(), session, "%_['?]", 0, 30)
	if err != nil || len(literal.Matches) != 1 || literal.Matches[0].Excerpt.Match != "%_['?]" {
		t.Fatal("search text was treated as SQL or a wildcard", literal, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.SearchMessages(ctx, session, "word", 0, 30); err == nil {
		t.Fatal("cancelled search kept reading")
	}
}
