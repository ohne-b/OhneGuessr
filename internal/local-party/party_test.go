package localparty

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLocalPartyBindingName(t *testing.T) {
	typeOf := reflect.TypeOf(&LocalParty{})
	method, ok := typeOf.MethodByName("LaunchParty")
	if !ok {
		t.Fatal("LaunchParty is not exported")
	}
	got := typeOf.Elem().PkgPath() + "." + typeOf.Elem().Name() + "." + method.Name
	want := "github.com/ohne-b/OhneGuessr/internal/local-party.LocalParty.LaunchParty"
	if got != want {
		t.Fatalf("Wails binding = %q, want %q", got, want)
	}
}

func TestLocalPartyService(t *testing.T) {
	var target string
	party := New(
		fstest.MapFS{"index.html": {Data: []byte("party")}},
		func(mapID string) bool { return mapID == "map-one" },
		func(url, _ string) error { target = url; return nil },
		nil,
	)
	state, err := party.LaunchParty("map-one", "ayu-light", "#3b9ee5")
	if err != nil || state.ID == "" || target == "" || !party.Active() {
		t.Fatalf("launch = %#v, %q, %v", state, target, err)
	}
	if err := party.StopParty(state.ID); err != nil || party.Active() {
		t.Fatalf("stop = %v, active = %v", err, party.Active())
	}
}

func TestPartyLifecycle(t *testing.T) {
	party, err := newPartyServer(fstest.MapFS{
		"index.html": {Data: []byte("party")},
	}, "map-one", "ayu-light", "#3b9ee5", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = party.close() })

	ada, status := joinTestParty(t, party, "Ada", partyPalette[0], nil)
	if status != http.StatusCreated {
		t.Fatalf("join status = %d", status)
	}
	_, status = joinTestParty(t, party, "ada", partyPalette[1], nil)
	if status != http.StatusConflict {
		t.Fatalf("duplicate name status = %d", status)
	}
	_, status = joinTestParty(t, party, "Color copy", partyPalette[0], nil)
	if status != http.StatusConflict {
		t.Fatalf("duplicate color status = %d", status)
	}
	bob, status := joinTestParty(t, party, "Bob", partyPalette[1], nil)
	if status != http.StatusCreated {
		t.Fatalf("second join status = %d", status)
	}

	locked, err := party.lockRoster()
	if err != nil || len(locked.Players) != 2 || !locked.RosterLocked {
		t.Fatalf("lock roster = %#v, %v", locked, err)
	}
	_, status = joinTestParty(t, party, "Late", partyPalette[2], nil)
	if status != http.StatusConflict {
		t.Fatalf("late join status = %d", status)
	}
	if _, err := party.finish(); err == nil {
		t.Fatal("finished before starting a round")
	}
	if err := party.beginRound(0, 2, 1234, "roadmap"); err != nil {
		t.Fatal(err)
	}

	adaGuess := PartyPoint{Lat: 48.1, Lng: 11.5}
	bobGuess := PartyPoint{Lat: 47.2, Lng: 10.4}
	guessTestParty(t, party, ada, adaGuess)
	guessTestParty(t, party, ada, PartyPoint{Lat: -10, Lng: -20})
	guessTestParty(t, party, bob, bobGuess)
	guessing := party.hostState()
	if !guessing.AllLocked || !samePartyPoint(guessing.Players[0].Guess, &adaGuess) {
		t.Fatal("all players should be locked")
	}
	guestState := partyState(t, party, ada)
	if guestState.Theme != "ayu-light" || guestState.AccentColor != "#3b9ee5" {
		t.Fatalf("guest appearance = %q, %q", guestState.Theme, guestState.AccentColor)
	}
	guestJSON, _ := json.Marshal(guestState)
	if bytes.Contains(guestJSON, []byte("Ada")) || bytes.Contains(guestJSON, []byte("Bob")) ||
		bytes.Contains(guestJSON, []byte(`"actual"`)) {
		t.Fatalf("guessing state leaked private host data: %s", guestJSON)
	}
	players, err := party.closeRound(0)
	if err != nil || len(players) != 2 {
		t.Fatalf("close round = %#v, %v", players, err)
	}
	if _, err := party.finish(); err == nil {
		t.Fatal("finished before publishing a reveal")
	}
	distanceAda, distanceBob := 12.5, 45.0
	if err := party.publishReveal(PartyRoundReveal{
		Round:  0,
		Actual: PartyPoint{Lat: 48.2, Lng: 11.6},
		Results: []PartyPlayerRound{
			{PlayerID: players[0].ID, Guess: &adaGuess, Distance: &distanceAda, Points: 4900},
			{PlayerID: players[1].ID, Guess: &bobGuess, Distance: &distanceBob, Points: 4200},
		},
	}); err != nil {
		t.Fatal(err)
	}

	adaState := partyState(t, party, ada)
	if adaState.Result == nil || adaState.Result.Points != 4900 || adaState.Result.Actual.Lat != 48.2 {
		t.Fatalf("reveal state = %#v", adaState)
	}
	if _, err := party.finish(); err == nil {
		t.Fatal("finished before the last round")
	}
	if err := party.beginRound(1, 2, 0, "roadmap"); err != nil {
		t.Fatal(err)
	}
	if _, err := party.closeRound(1); err != nil {
		t.Fatal(err)
	}
	if err := party.publishReveal(PartyRoundReveal{
		Round: 1, Actual: PartyPoint{Lat: 1, Lng: 2},
		Results: []PartyPlayerRound{{PlayerID: players[0].ID}, {PlayerID: players[1].ID}},
	}); err != nil {
		t.Fatal(err)
	}
	final, err := party.finish()
	if err != nil || final.Phase != "final" || final.Players[0].Place != 1 || final.Players[1].Place != 2 {
		t.Fatalf("finish = %#v, %v", final, err)
	}
	if err := party.reset(); err != nil {
		t.Fatal(err)
	}
	reset := party.hostState()
	if reset.Phase != "lobby" || !reset.RosterLocked || reset.Players[0].Total != 0 {
		t.Fatalf("reset = %#v", reset)
	}
}

func joinTestParty(t *testing.T, party *partyServer, name, color string, cookie *http.Cookie) (*http.Cookie, int) {
	t.Helper()
	body := `{"join":"` + party.secret + `","name":"` + name + `","color":"` + color + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://party.test/api/join", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://party.test")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	party.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		return nil, response.Code
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("join cookies = %#v", cookies)
	}
	return cookies[0], response.Code
}

func guessTestParty(t *testing.T, party *partyServer, cookie *http.Cookie, point PartyPoint) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"round": 0, "lat": point.Lat, "lng": point.Lng})
	request := httptest.NewRequest(http.MethodPost, "http://party.test/api/guess", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://party.test")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	party.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("guess status = %d: %s", response.Code, response.Body.String())
	}
	var state PartyGuestState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || !state.Locked || state.Result != nil {
		t.Fatalf("guess state = %#v, %v", state, err)
	}
}

func partyState(t *testing.T, party *partyServer, cookie *http.Cookie) PartyGuestState {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://party.test/api/state", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	party.server.Handler.ServeHTTP(response, request)
	var state PartyGuestState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}
