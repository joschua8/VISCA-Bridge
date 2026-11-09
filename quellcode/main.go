// VISCA-Bridge
//
// Nimmt VISCA-Befehle über TCP auf mehreren Ports an (ein Port pro Kamera),
// setzt die VISCA-Adresse passend zum Port und schickt alles über EINE
// Verbindung an einen seriellen Konverter (z. B. Waveshare RS485 TO ETH).
// Antworten der Kameras werden an den richtigen Client zurückgeleitet und
// so umgeschrieben, dass der Client die Adresse sieht, die er selbst benutzt hat.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const version = "1.0"

// ---------- Konfiguration ----------

type Camera struct {
	Name       string `json:"name"`
	ListenPort int    `json:"listen_port"`
	// VISCA-Adresse der Kamera am seriellen Bus (1–7).
	// 0 = nichts umschreiben (Durchreichen, z. B. für Companion mit eigener Adresse).
	Address int `json:"visca_address"`
}

type Config struct {
	ConverterHost string   `json:"converter_host"`
	ConverterPort int      `json:"converter_port"`
	ListenIP      string   `json:"listen_ip"`
	Cameras       []Camera `json:"cameras"`
	// Port für eine kleine Statusseite im Browser (0 = aus).
	StatusPort int  `json:"status_port"`
	LogFile    string `json:"log_file"`
	// Alle Bytes ins Log schreiben (zur Fehlersuche).
	Debug bool `json:"debug"`
}

func defaultConfig() Config {
	return Config{
		ConverterHost: "192.168.133.200",
		ConverterPort: 4196,
		ListenIP:      "0.0.0.0",
		Cameras: []Camera{
			{Name: "Kamera 1", ListenPort: 5001, Address: 1},
			{Name: "Kamera 2", ListenPort: 5002, Address: 2},
		},
		StatusPort: 8099,
		LogFile:    "visca-bridge.log",
	}
}

// ---------- Client-Verwaltung ----------

type client struct {
	id       int
	conn     net.Conn
	cam      *Camera
	mu       sync.Mutex // schützt Schreiben auf conn
	origAddr byte       // Adresse, die der Client zuletzt selbst benutzt hat (1–7)
}

func (c *client) send(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	c.conn.Write(b)
}

type bridge struct {
	cfg Config

	mu       sync.Mutex
	clients  map[int]*client
	nextID   int
	lastUser map[byte]*client // Kameraadresse am Bus -> Client, der zuletzt dorthin gesendet hat
	lastAny  *client          // Client, der zuletzt irgendetwas gesendet hat

	up      chan []byte // Pakete Richtung Konverter
	upConn  net.Conn
	upState string

	stats struct {
		toCam, fromCam uint64
	}
}

func newBridge(cfg Config) *bridge {
	return &bridge{
		cfg:      cfg,
		clients:  map[int]*client{},
		lastUser: map[byte]*client{},
		up:       make(chan []byte, 256),
		upState:  "noch nicht verbunden",
	}
}

func hexs(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, " ")
}

// splitVisca zerlegt einen Puffer in vollständige VISCA-Pakete (Ende = 0xFF).
// Gibt Pakete und den unvollständigen Rest zurück.
func splitVisca(buf []byte) (pkts [][]byte, rest []byte) {
	start := 0
	for i, b := range buf {
		if b == 0xFF {
			p := make([]byte, i+1-start)
			copy(p, buf[start:i+1])
			pkts = append(pkts, p)
			start = i + 1
		}
	}
	rest = buf[start:]
	// Schutz gegen Müll ohne Abschlussbyte
	if len(rest) > 64 {
		rest = nil
	}
	return
}

// ---------- Richtung Client -> Kamera ----------

func (br *bridge) handleClient(cam *Camera, conn net.Conn) {
	br.mu.Lock()
	br.nextID++
	c := &client{id: br.nextID, conn: conn, cam: cam, origAddr: 1}
	br.clients[c.id] = c
	br.mu.Unlock()

	log.Printf("[%s] Verbindung von %s (Client #%d)", cam.Name, conn.RemoteAddr(), c.id)

	defer func() {
		conn.Close()
		br.mu.Lock()
		delete(br.clients, c.id)
		for a, u := range br.lastUser {
			if u == c {
				delete(br.lastUser, a)
			}
		}
		if br.lastAny == c {
			br.lastAny = nil
		}
		br.mu.Unlock()
		log.Printf("[%s] Verbindung von %s getrennt (Client #%d)", cam.Name, conn.RemoteAddr(), c.id)
	}()

	r := bufio.NewReader(conn)
	tmp := make([]byte, 1024)
	var pending []byte
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			pending = append(pending, tmp[:n]...)
			var pkts [][]byte
			pkts, pending = splitVisca(pending)
			for _, p := range pkts {
				br.fromClient(c, p)
			}
		}
		if err != nil {
			return
		}
	}
}

func (br *bridge) fromClient(c *client, p []byte) {
	if len(p) < 2 {
		return
	}
	orig := p[0]
	busAddr := byte(0)

	if orig >= 0x81 && orig <= 0x87 {
		c.origAddr = orig & 0x07
		if c.cam.Address >= 1 && c.cam.Address <= 7 {
			p[0] = 0x80 | byte(c.cam.Address)
		}
		busAddr = p[0] & 0x07
	} else if orig == 0x88 {
		// Broadcast (z. B. Address-Set/IF_Clear): unverändert durchreichen
	}

	br.mu.Lock()
	if busAddr != 0 {
		br.lastUser[busAddr] = c
	}
	br.lastAny = c
	br.mu.Unlock()

	if br.cfg.Debug {
		if orig != p[0] {
			log.Printf("[%s] -> Kamera: %s (Adresse %02X -> %02X)", c.cam.Name, hexs(p), orig, p[0])
		} else {
			log.Printf("[%s] -> Kamera: %s", c.cam.Name, hexs(p))
		}
	}

	select {
	case br.up <- p:
	default:
		log.Printf("Warteschlange voll, Befehl verworfen (Konverter erreichbar?)")
	}
}

// ---------- Verbindung zum Konverter ----------

func (br *bridge) upstreamLoop() {
	addr := net.JoinHostPort(br.cfg.ConverterHost, fmt.Sprint(br.cfg.ConverterPort))
	backoff := time.Second
	for {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			br.setUpState("nicht erreichbar: " + err.Error())
			log.Printf("Konverter %s nicht erreichbar (%v), neuer Versuch in %s", addr, err, backoff)
			// Befehle, die während der Wartezeit kommen, verwerfen (sonst fährt
			// die Kamera nach dem Reconnect alte Bewegungen nach)
			drainUntil(br.up, time.Now().Add(backoff))
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(10 * time.Second)
		}
		br.mu.Lock()
		br.upConn = conn
		br.upState = "verbunden mit " + addr
		br.mu.Unlock()
		log.Printf("Verbunden mit Konverter %s", addr)

		done := make(chan struct{})
		go br.readUpstream(conn, done)

	writeLoop:
		for {
			select {
			case p := <-br.up:
				conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if _, err := conn.Write(p); err != nil {
					log.Printf("Schreibfehler zum Konverter: %v", err)
					break writeLoop
				}
				br.mu.Lock()
				br.stats.toCam++
				br.mu.Unlock()
			case <-done:
				break writeLoop
			}
		}
		conn.Close()
		br.setUpState("getrennt, verbinde neu …")
		log.Printf("Verbindung zum Konverter verloren, verbinde neu …")
		time.Sleep(time.Second)
	}
}

func drainUntil(ch chan []byte, t time.Time) {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	for {
		select {
		case <-ch:
		case <-timer.C:
			return
		}
	}
}

func (br *bridge) setUpState(s string) {
	br.mu.Lock()
	br.upState = s
	br.mu.Unlock()
}

// ---------- Richtung Kamera -> Client ----------

func (br *bridge) readUpstream(conn net.Conn, done chan struct{}) {
	defer close(done)
	tmp := make([]byte, 1024)
	var pending []byte
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			pending = append(pending, tmp[:n]...)
			var pkts [][]byte
			pkts, pending = splitVisca(pending)
			for _, p := range pkts {
				br.fromCamera(p)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("Lesefehler vom Konverter: %v", err)
			}
			return
		}
	}
}

func (br *bridge) fromCamera(p []byte) {
	if len(p) < 2 {
		return
	}
	br.mu.Lock()
	br.stats.fromCam++
	h := p[0]
	var target *client
	var targets []*client

	if h >= 0x90 && h <= 0xF0 && h&0x0F == 0 {
		// Antwort von Kamera: Header z0, z = Adresse + 8
		busAddr := (h >> 4) - 8
		target = br.lastUser[busAddr]
		if target == nil {
			for _, c := range br.clients {
				if c.cam.Address == int(busAddr) || c.cam.Address == 0 {
					targets = append(targets, c)
				}
			}
		}
	} else {
		// z. B. Address-Set-Antwort (88 30 0x FF): an den letzten Absender
		target = br.lastAny
	}
	if target != nil {
		targets = []*client{target}
	}
	br.mu.Unlock()

	for _, c := range targets {
		out := make([]byte, len(p))
		copy(out, p)
		if h >= 0x90 && h <= 0xF0 && h&0x0F == 0 && c.cam.Address != 0 {
			out[0] = (c.origAddr + 8) << 4
		}
		if br.cfg.Debug {
			log.Printf("[%s] <- Kamera: %s", c.cam.Name, hexs(out))
		}
		c.send(out)
	}
}

// ---------- Listener ----------

func (br *bridge) listen(cam *Camera) {
	addr := net.JoinHostPort(br.cfg.ListenIP, fmt.Sprint(cam.ListenPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[%s] Port %d kann nicht geöffnet werden: %v", cam.Name, cam.ListenPort, err)
	}
	if cam.Address == 0 {
		log.Printf("[%s] lauscht auf %s (Adresse wird nicht verändert)", cam.Name, addr)
	} else {
		log.Printf("[%s] lauscht auf %s -> VISCA-Adresse %d", cam.Name, addr, cam.Address)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[%s] Accept-Fehler: %v", cam.Name, err)
			time.Sleep(time.Second)
			continue
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		go br.handleClient(cam, conn)
	}
}

// ---------- Statusseite ----------

func (br *bridge) statusServer() {
	if br.cfg.StatusPort == 0 {
		return
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		br.mu.Lock()
		defer br.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="3">
<title>VISCA-Bridge</title><style>body{font-family:system-ui,sans-serif;margin:24px;max-width:640px}
td,th{padding:4px 10px;text-align:left}.ok{color:#18794e}.bad{color:#c4320a}</style>
<h2>VISCA-Bridge %s</h2>`, version)
		cls := "bad"
		if br.upConn != nil && strings.HasPrefix(br.upState, "verbunden") {
			cls = "ok"
		}
		fmt.Fprintf(w, `<p>Konverter: <b class="%s">%s</b></p>`, cls, br.upState)
		fmt.Fprintf(w, `<p>Befehle an Kameras: %d · Antworten: %d</p>`, br.stats.toCam, br.stats.fromCam)
		fmt.Fprint(w, `<table><tr><th>Kamera</th><th>Port</th><th>Adresse</th><th>Verbunden</th></tr>`)
		for i := range br.cfg.Cameras {
			cam := &br.cfg.Cameras[i]
			var who []string
			for _, c := range br.clients {
				if c.cam == cam {
					who = append(who, c.conn.RemoteAddr().String())
				}
			}
			sort.Strings(who)
			a := fmt.Sprint(cam.Address)
			if cam.Address == 0 {
				a = "unverändert"
			}
			fmt.Fprintf(w, `<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>`,
				cam.Name, cam.ListenPort, a, strings.Join(who, "<br>"))
		}
		fmt.Fprint(w, `</table>`)
	})
	addr := fmt.Sprintf(":%d", br.cfg.StatusPort)
	log.Printf("Statusseite: http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Printf("Statusseite konnte nicht starten: %v", err)
	}
}

// ---------- main ----------

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func loadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaultConfig()
		out, _ := json.MarshalIndent(cfg, "", "  ")
		os.WriteFile(path, out, 0644)
		return cfg
	}
	if err != nil {
		log.Fatalf("config.json kann nicht gelesen werden: %v", err)
	}
	cfg := defaultConfig()
	cfg.Cameras = nil
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Fehler in config.json: %v", err)
	}
	for _, c := range cfg.Cameras {
		if c.Address < 0 || c.Address > 7 {
			log.Fatalf("Fehler in config.json: visca_address von %q muss 0–7 sein", c.Name)
		}
	}
	return cfg
}

func main() {
	dir := exeDir()
	cfgPath := filepath.Join(dir, "config.json")
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg := loadConfig(cfgPath)

	// Log in Datei + Konsole (in der Version ohne Fenster nur Datei)
	var writers []io.Writer
	if cfg.LogFile != "" {
		lp := cfg.LogFile
		if !filepath.IsAbs(lp) {
			lp = filepath.Join(dir, lp)
		}
		// Log klein halten: bei > 2 MB neu anfangen
		if st, err := os.Stat(lp); err == nil && st.Size() > 2<<20 {
			os.Rename(lp, lp+".old")
		}
		if f, err := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			writers = append(writers, f)
		}
	}
	if consoleAvailable() {
		writers = append(writers, os.Stdout)
	}
	log.SetOutput(io.MultiWriter(writers...))
	log.SetFlags(log.Ldate | log.Ltime)

	log.Printf("VISCA-Bridge %s startet, Konfiguration: %s", version, cfgPath)

	br := newBridge(cfg)
	go br.upstreamLoop()
	go br.statusServer()
	for i := range br.cfg.Cameras {
		go br.listen(&br.cfg.Cameras[i])
	}
	select {}
}
