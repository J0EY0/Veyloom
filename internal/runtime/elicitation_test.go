package runtime

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestElicitation_ContentAndAction(t *testing.T) {
	filled := Decision{Allow: true, Answer: json.RawMessage(`{"content":{"name":"alice"}}`)}
	if string(filled.Content()) != `{"name":"alice"}` {
		t.Errorf("Content = %s", filled.Content())
	}
	for _, d := range []Decision{
		{Allow: false, Answer: json.RawMessage(`{"content":{"name":"alice"}}`)},
		{Allow: true},
		{Allow: true, Answer: json.RawMessage(`{"content":null}`)},
		{Allow: true, Answer: json.RawMessage(`not json`)},
	} {
		if d.Content() != nil {
			t.Errorf("%+v: Content = %s, want none", d, d.Content())
		}
	}
	if got := elicitationAction(filled, nil); got != ElicitAccept {
		t.Errorf("filled: %s", got)
	}
	if got := elicitationAction(Decision{Allow: false}, nil); got != ElicitDecline {
		t.Errorf("declined: %s", got)
	}
	if got := elicitationAction(Decision{}, errors.New("turn over")); got != ElicitCancel {
		t.Errorf("nobody decided: %s", got)
	}
}
