package localparty

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	qrcode "github.com/skip2/go-qrcode"
)

type partyServer struct {
	mu           sync.Mutex
	id           string
	secret       string
	mapID        string
	frontend     fs.FS
	listener     net.Listener
	server       *http.Server
	url          string
	urls         []string
	qrCode       string
	phase        string
	rosterLocked bool
	round        int
	rounds       int
	deadline     int64
	mapStyle     string
	theme        string
	accentColor  string
	players      []*partyPlayer
	byToken      map[string]*partyPlayer
	subscribers  map[chan struct{}]struct{}
	changed      func(string)
	closed       bool
}

func newPartyServer(frontend fs.FS, mapID, theme, accentColor string, changed func(string)) (*partyServer, error) {
	id, err := partyToken(12)
	if err != nil {
		return nil, err
	}
	secret, err := partyToken(24)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:8077")
	if err != nil {
		listener, err = net.Listen("tcp4", "0.0.0.0:0")
	}
	if err != nil {
		return nil, fmt.Errorf("start local party server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	urls := partyURLs(port, secret)
	png, err := qrcode.Encode(urls[0], qrcode.Medium, 256)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("create party QR code: %w", err)
	}
	p := &partyServer{
		id:          id,
		secret:      secret,
		mapID:       mapID,
		theme:       theme,
		accentColor: accentColor,
		frontend:    frontend,
		listener:    listener,
		url:         urls[0],
		urls:        urls,
		qrCode:      "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		phase:       "lobby",
		round:       -1,
		byToken:     make(map[string]*partyPlayer),
		subscribers: make(map[chan struct{}]struct{}),
		changed:     changed,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", p.serveState)
	mux.HandleFunc("POST /api/join", p.serveJoin)
	mux.HandleFunc("GET /api/events", p.serveEvents)
	mux.HandleFunc("POST /api/guess", p.serveGuess)
	mux.HandleFunc("GET /{file...}", p.serveFrontend)
	p.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       75 * time.Second,
	}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

func partyToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create party token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func partyURLs(port int, secret string) []string {
	preferred := ""
	if connection, err := net.Dial("udp4", "192.0.2.1:80"); err == nil {
		if address, ok := connection.LocalAddr().(*net.UDPAddr); ok && address.IP.IsPrivate() {
			preferred = address.IP.String()
		}
		_ = connection.Close()
	}
	addresses := make([]string, 0)
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		items, _ := iface.Addrs()
		for _, item := range items {
			ip, _, err := net.ParseCIDR(item.String())
			if err != nil || ip.To4() == nil || (!ip.IsPrivate() && !ip.IsLinkLocalUnicast()) {
				continue
			}
			addresses = append(addresses, ip.String())
		}
	}
	sort.Strings(addresses)
	addresses = slices.Compact(addresses)
	if preferred != "" {
		ordered := []string{preferred}
		for _, address := range addresses {
			if address != preferred {
				ordered = append(ordered, address)
			}
		}
		addresses = ordered
	}
	if len(addresses) == 0 {
		addresses = []string{"127.0.0.1"}
	}
	urls := make([]string, 0, len(addresses))
	for _, address := range addresses {
		host := net.JoinHostPort(address, strconv.Itoa(port))
		urls = append(urls, "http://"+host+"/?view=party&join="+url.QueryEscape(secret))
	}
	return urls
}

func (p *partyServer) close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.phase = "closed"
	p.notifyLocked()
	p.mu.Unlock()
	forceClose := time.AfterFunc(250*time.Millisecond, func() { _ = p.server.Close() })
	defer forceClose.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.server.Shutdown(ctx); err != nil {
		if closeErr := p.server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			return closeErr
		}
	}
	return nil
}

func (p *partyServer) serveFrontend(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.PathValue("file")), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	contents, err := fs.ReadFile(p.frontend, name)
	if err != nil {
		name = "index.html"
		contents, err = fs.ReadFile(p.frontend, name)
	}
	if err != nil {
		http.Error(w, "guest app unavailable", http.StatusNotFound)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if path.Ext(name) == ".js" {
		contentType = "text/javascript; charset=utf-8"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	_, _ = w.Write(contents)
}

func (p *partyServer) serveState(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	player := p.playerFromRequestLocked(r)
	if player == nil && r.URL.Query().Get("join") != p.secret {
		p.mu.Unlock()
		partyError(w, http.StatusForbidden, "invalid party link")
		return
	}
	state := p.guestStateLocked(player)
	p.mu.Unlock()
	httpjson.Write(w, http.StatusOK, state)
}

func (p *partyServer) serveJoin(w http.ResponseWriter, r *http.Request) {
	if !partySameOrigin(r) {
		partyError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	body, err := httpjson.DecodeLimit[struct {
		Join  string `json:"join"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}](r, partyBodyLimit)
	if err != nil {
		partyError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := cleanPartyName(body.Name)
	if err != nil {
		partyError(w, http.StatusBadRequest, err.Error())
		return
	}
	color := strings.ToLower(strings.TrimSpace(body.Color))

	p.mu.Lock()
	if existing := p.playerFromRequestLocked(r); existing != nil {
		state := p.guestStateLocked(existing)
		p.mu.Unlock()
		httpjson.Write(w, http.StatusOK, state)
		return
	}
	if body.Join != p.secret {
		p.mu.Unlock()
		partyError(w, http.StatusForbidden, "invalid party link")
		return
	}
	if p.closed || p.rosterLocked || p.phase != "lobby" {
		p.mu.Unlock()
		partyError(w, http.StatusConflict, "the party roster is locked")
		return
	}
	if len(p.players) >= partyCapacity {
		p.mu.Unlock()
		partyError(w, http.StatusConflict, "the party is full")
		return
	}
	if !partyColor(color) || p.colorUsedLocked(color) {
		p.mu.Unlock()
		partyError(w, http.StatusConflict, "that color is unavailable")
		return
	}
	for _, player := range p.players {
		if strings.EqualFold(player.name, name) {
			p.mu.Unlock()
			partyError(w, http.StatusConflict, "that username is already taken")
			return
		}
	}
	token, tokenErr := partyToken(24)
	id, idErr := partyToken(9)
	if tokenErr != nil || idErr != nil {
		p.mu.Unlock()
		partyError(w, http.StatusInternalServerError, "could not join the party")
		return
	}
	player := &partyPlayer{id: id, name: name, color: color, token: token}
	p.players = append(p.players, player)
	p.byToken[token] = player
	p.notifyLocked()
	state := p.guestStateLocked(player)
	p.mu.Unlock()
	p.emitChanged()
	http.SetCookie(w, &http.Cookie{
		Name:     partyCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	httpjson.Write(w, http.StatusCreated, state)
}

func (p *partyServer) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		partyError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	p.mu.Lock()
	player := p.playerFromRequestLocked(r)
	if player == nil {
		p.mu.Unlock()
		partyError(w, http.StatusUnauthorized, "join the party first")
		return
	}
	updates := make(chan struct{}, 1)
	p.subscribers[updates] = struct{}{}
	state := p.guestStateLocked(player)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.subscribers, updates)
		p.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writePartyEvent(w, state)
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			p.mu.Lock()
			state = p.guestStateLocked(player)
			p.mu.Unlock()
			writePartyEvent(w, state)
			flusher.Flush()
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (p *partyServer) serveGuess(w http.ResponseWriter, r *http.Request) {
	if !partySameOrigin(r) {
		partyError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	body, err := httpjson.DecodeLimit[struct {
		Round int     `json:"round"`
		Lat   float64 `json:"lat"`
		Lng   float64 `json:"lng"`
	}](r, partyBodyLimit)
	if err != nil {
		partyError(w, http.StatusBadRequest, err.Error())
		return
	}
	guess := PartyPoint{Lat: body.Lat, Lng: body.Lng}
	if !validPartyPoint(guess) {
		partyError(w, http.StatusBadRequest, "invalid guess coordinates")
		return
	}

	p.mu.Lock()
	player := p.playerFromRequestLocked(r)
	if player == nil {
		p.mu.Unlock()
		partyError(w, http.StatusUnauthorized, "join the party first")
		return
	}
	if p.phase != "guessing" || body.Round != p.round {
		p.mu.Unlock()
		partyError(w, http.StatusConflict, "that round is no longer accepting guesses")
		return
	}
	if !player.locked {
		player.guess = &guess
		player.locked = true
	}
	p.notifyLocked()
	state := p.guestStateLocked(player)
	p.mu.Unlock()
	p.emitChanged()
	httpjson.Write(w, http.StatusOK, state)
}

func (p *partyServer) playerFromRequestLocked(r *http.Request) *partyPlayer {
	cookie, err := r.Cookie(partyCookieName)
	if err != nil {
		return nil
	}
	return p.byToken[cookie.Value]
}

func (p *partyServer) notifyLocked() {
	for subscriber := range p.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

func (p *partyServer) emitChanged() {
	if p.changed != nil {
		p.changed(p.id)
	}
}

func partySameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host
}

func writePartyEvent(w io.Writer, state PartyGuestState) {
	data, _ := json.Marshal(state)
	_, _ = fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
}

func partyError(w http.ResponseWriter, status int, message string) {
	httpjson.Write(w, status, map[string]string{"error": message})
}
