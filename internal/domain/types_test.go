package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEnums(t *testing.T) {
	if !KindPersonal.Valid() || ContextKind("x").Valid() {
		t.Error("ContextKind.Valid")
	}
	for k, want := range map[ContextKind]bool{KindProject: true, KindMode: true, KindPersonal: false, KindSystem: false, "x": false} {
		if k.Exportable() != want {
			t.Errorf("%s.Exportable() = %v", k, !want)
		}
	}
	if !SourceDeck.Valid() || EventSource("phone").Valid() {
		t.Error("EventSource.Valid")
	}
	if !AgentActionRequired.Valid() || AgentState("busy").Valid() {
		t.Error("AgentState.Valid")
	}
	if !IdeaArchived.Valid() || IdeaStatus("x").Valid() {
		t.Error("IdeaStatus.Valid")
	}
	if !AISkipped.Valid() || AIState("x").Valid() {
		t.Error("AIState.Valid")
	}
	if !ItemRisk.Valid() || ItemType("x").Valid() {
		t.Error("ItemType.Valid")
	}
}

func TestContextID(t *testing.T) {
	for id, ok := range map[string]bool{
		"acme": true, "a": true, "9to5": true, "a_b-c": true,
		"": false, "-a": false, "_a": false, "Acme": false, "a b": false, "é": false,
		"a2345678901234567890123456789012345678901": false, "a234567890123456789012345678901234567890": true,
	} {
		if ValidContextID(id) != ok {
			t.Errorf("ValidContextID(%q) = %v", id, !ok)
		}
		if err := CheckContextID(id); (err == nil) != ok || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Errorf("CheckContextID(%q) = %v", id, err)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0m", 29 * time.Second: "0m", 30 * time.Second: "1m", 45 * time.Minute: "45m",
		time.Hour: "1h00m", 85 * time.Minute: "1h25m", 26*time.Hour + 5*time.Minute: "26h05m", -90 * time.Minute: "-1h30m",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDurationJSON(t *testing.T) {
	b, err := json.Marshal(struct {
		D Duration `json:"d"`
	}{Duration(90*time.Second + 400*time.Millisecond)})
	if err != nil || string(b) != `{"d":90}` {
		t.Fatalf("marshal: %s %v", b, err)
	}
	var v struct {
		D Duration `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"d":3600}`), &v); err != nil || v.D.D() != time.Hour {
		t.Fatalf("unmarshal: %v %v", v.D, err)
	}
	if err := json.Unmarshal([]byte(`{"d":"x"}`), &v); err == nil {
		t.Fatal("want error")
	}
}
