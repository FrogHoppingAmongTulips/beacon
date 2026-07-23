package xray

import "testing"

func TestParseStats(t *testing.T) {
	in := []byte(`{"stat":[
		{"name":"user>>>abc>>>traffic>>>uplink","value":"100"},
		{"name":"user>>>abc>>>traffic>>>downlink","value":"250"},
		{"name":"user>>>xyz>>>traffic>>>uplink","value":"7"},
		{"name":"inbound>>>api>>>traffic>>>downlink","value":"5"}
	]}`)
	got, err := parseStats(in)
	if err != nil {
		t.Fatal(err)
	}
	if got["abc"].Up != 100 || got["abc"].Down != 250 {
		t.Fatalf("abc = %+v, ждали {100 250}", got["abc"])
	}
	if got["xyz"].Up != 7 || got["xyz"].Down != 0 {
		t.Fatalf("xyz = %+v", got["xyz"])
	}
	if _, ok := got["api"]; ok {
		t.Fatal("inbound-статистика не должна попадать в per-user")
	}
}

func TestDelta(t *testing.T) {
	if got := delta(100, 40); got != 60 {
		t.Fatalf("рост: %d, ждали 60", got)
	}
	if got := delta(30, 100); got != 30 {
		t.Fatalf("сброс счётчика: %d, ждали 30 (берём текущее)", got)
	}
	if got := delta(50, 50); got != 0 {
		t.Fatalf("без изменений: %d, ждали 0", got)
	}
}
