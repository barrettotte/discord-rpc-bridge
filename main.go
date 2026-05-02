package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var version = "dev"

var (
	discordApiUrl = "https://discord.com/api/v10/applications/detectable"
	scanInterval  = 15 * time.Second
	gameCacheTTL  = 7 * 24 * time.Hour
	ignoredGames  = map[string]bool{}
	// folder-name prefixes that are always Steam infrastructure, not games.
	// covers SteamLinuxRuntime{,_soldier,_sniper,_4,...} and Proton {7,8,9,Experimental,Hotfix,...}
	ignoredGamePrefixes = []string{"SteamLinuxRuntime", "Proton"}
	ignoredProcesses    = map[string]bool{
		"gamescopereaper":      true,
		"reaper":               true,
		"steam-launch-wrapper": true,
		"pressure-vessel-wrap": true,
	}
	manualMappings    = map[string]string{}
	nameToID          = make(map[string]string)
	nonAlphanumeric   = regexp.MustCompile(`[^a-z0-9]`)
	httpClient        = &http.Client{Timeout: 30 * time.Second}
	accentTransformer = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
)

type Config struct {
	ScanIntervalSeconds int               `json:"scan_interval_seconds"`
	IgnoredGames        []string          `json:"ignored_games"`
	IgnoredProcesses    []string          `json:"ignored_processes"`
	DiscordApiVersion   int               `json:"discord_api_version"`
	GameCacheTTLDays    int               `json:"game_cache_ttl_days"`
	ManualMappings      map[string]string `json:"manual_mappings"`
}

type Executable struct {
	Name string `json:"name"`
	OS   string `json:"os"`
}

type DetectableApp struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Executables []Executable `json:"executables"`
}

type ActivityAssets struct {
	LargeImage string `json:"large_image"`
	LargeText  string `json:"large_text"`
}

type Activity struct {
	Details string         `json:"details"`
	State   string         `json:"state"`
	Assets  ActivityAssets `json:"assets"`
}

type ActivityArgs struct {
	Pid      int      `json:"pid"`
	Activity Activity `json:"activity"`
}

// IPC structs

type IpcHandshake struct {
	V        int    `json:"v"`
	ClientID string `json:"client_id"`
}

type DiscordRpcPayload struct {
	Cmd   string      `json:"cmd"`
	Nonce string      `json:"nonce"`
	Args  interface{} `json:"args"`
}

// populate lookup for game client ID
func populateMap(apps []DetectableApp) {
	for _, app := range apps {
		nameToID[normalizeGameName(app.Name)] = app.ID
	}
	log.Printf("Indexed %d known games.", len(nameToID))
}

// load game JSON from cache or build cache from Discord API call
func loadGameData(cacheFile string) error {
	shouldUpdate := false
	info, err := os.Stat(cacheFile)

	if os.IsNotExist(err) {
		shouldUpdate = true // file not exist
	} else if err == nil {
		// file exists, check if stale
		if time.Since(info.ModTime()) > gameCacheTTL {
			log.Println("Game list cache expired. Refreshing...")
			shouldUpdate = true
		}
	}

	if shouldUpdate {
		if err := refreshGameCache(cacheFile); err != nil {
			log.Printf("Cache refresh failed: %v. Using existing cache if present.", err)
		}
	}

	// load from disk
	file, err := os.ReadFile(cacheFile)
	if err != nil {
		return err
	}

	var apps []DetectableApp
	if err := json.Unmarshal(file, &apps); err != nil {
		return err
	}

	populateMap(apps)
	return nil
}

// download a fresh game list from Discord and write it to cacheFile.
// validates HTTP status and a non-empty list before overwriting any
// existing cache, to avoid poisoning it with an error response body.
func refreshGameCache(cacheFile string) error {
	log.Println("Downloading game list from Discord...")
	resp, err := httpClient.Get(discordApiUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, discordApiUrl)
	}

	var apps []DetectableApp
	if err := json.NewDecoder(resp.Body).Decode(&apps); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(apps) == 0 {
		return fmt.Errorf("response contained zero apps; refusing to overwrite cache")
	}

	data, err := json.Marshal(apps)
	if err != nil {
		return fmt.Errorf("re-marshal apps: %w", err)
	}
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	log.Printf("Cache updated successfully (%d apps).", len(apps))
	return nil
}

// get path to Discord IPC socket
func findDiscordSocket() (string, error) {
	uid := os.Getuid()
	candidates := []string{
		fmt.Sprintf("/run/user/%d/discord-ipc-0", uid),
		fmt.Sprintf("/run/user/%d/app/com.discordapp.Discord/discord-ipc-0", uid), // flatpak default
		fmt.Sprintf("/run/user/%d/snap.discord/discord-ipc-0", uid),
		// maybe there's more depending on distro and/or install method?
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("discord socket not found")
}

// parse and log the Discord IPC response
func readIpcResponse(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{}) // clear deadline after read

	// read header (8 bytes)
	header := make([]byte, 8)
	_, err := io.ReadFull(conn, header)
	if err != nil {
		log.Printf("ERROR: Failed to read header: %v", err)
		return
	}

	// parse length (last 4 bytes of header)
	dataLen := binary.LittleEndian.Uint32(header[4:8])

	// cap allocation to avoid OOM on a malformed/garbage header.
	// Discord IPC frames are well under 1 MB in practice.
	const maxPayload = 1 << 20
	if dataLen > maxPayload {
		log.Printf("ERROR: IPC payload length %d exceeds %d-byte cap, dropping", dataLen, maxPayload)
		return
	}

	// read the payload
	payload := make([]byte, dataLen)
	_, err = io.ReadFull(conn, payload)
	if err != nil {
		log.Printf("ERROR: Failed to read payload: %v", err)
		return
	}
	log.Printf("Discord response: %s", string(payload))
}

// send IPC packet to Discord IPC socket
func sendIPCPacket(conn net.Conn, opcode int, payload []byte) error {
	buf := new(bytes.Buffer)

	// opcode (4 bytes - little endian)
	if err := binary.Write(buf, binary.LittleEndian, int32(opcode)); err != nil {
		return err
	}

	// length (4 bytes - little endian)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(payload))); err != nil {
		return err
	}

	// send payload
	buf.Write(payload)
	_, err := conn.Write(buf.Bytes())
	return err
}

// fixup the raw Steam folder name to match Discord's JSON entries
func normalizeGameName(input string) string {

	// transliterate accents/umlauts (ex: Ragnarök -> Ragnarok)
	s, _, _ := transform.String(accentTransformer, input)

	return nonAlphanumeric.ReplaceAllString(strings.ToLower(s), "")
}

// returns true if the Steam folder name is in the ignore list or matches a known infrastructure prefix
func isIgnoredGame(name string) bool {
	if ignoredGames[name] {
		return true
	}
	for _, prefix := range ignoredGamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// find Discord client ID of provided game
func resolveClientID(name string) string {
	if id, ok := manualMappings[name]; ok {
		return id
	}
	norm := normalizeGameName(name)
	if id, ok := nameToID[norm]; ok {
		return id
	}
	return "000000000000000000" // default, but will not work (handshake fail)
}

// connect to Discord IPC socket as clientID
func connectIPC(path string, clientID string) (net.Conn, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}

	// start handshake as generic client
	handshake := IpcHandshake{V: 1, ClientID: clientID}
	payload, _ := json.Marshal(handshake)

	// opcode 0 = handshake
	if err := sendIPCPacket(conn, 0, payload); err != nil {
		conn.Close()
		return nil, err
	}

	// read response
	log.Println("Sent handshake. Waiting for reply...")
	readIpcResponse(conn)

	return conn, nil
}

// given path with steamapps/common, extract the steam game folder name.
// works for both native and flatpak steam installations
func extractSteamGameName(fullPath string) string {
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	const key = "steamapps/common"
	idx := strings.Index(fullPath, key)
	if idx == -1 {
		return ""
	}

	// strip everything before game name
	name := fullPath[idx+len(key):]
	name = strings.TrimPrefix(name, "/")

	// extract first directory component
	parts := strings.SplitN(name, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// try to find the game name from the process's cmdline args (for proton games)
func scanCmdline(pidStr string) string {
	// /proc/<pid>/cmdline args separated by null bytes (\0)
	data, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline"))
	if err != nil {
		return ""
	}

	args := bytes.Split(data, []byte{0})

	// also check ignoredProcesses against argv[0] basename — handles the case
	// where /proc/<pid>/exe readlink failed in scanProcesses and the wrapper
	// process's cmdline still carries the wrapped game's path.
	if len(args) > 0 && len(args[0]) > 0 {
		if ignoredProcesses[filepath.Base(string(args[0]))] {
			return ""
		}
	}

	for _, arg := range args {
		if len(arg) == 0 {
			continue
		}
		path := string(arg)
		name := extractSteamGameName(path)

		if name != "" && !isIgnoredGame(name) {
			return name
		}
	}
	return ""
}

// scan active processes of current user for active games
func scanProcesses() (string, int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// check if PID
		pidStr := entry.Name()
		if pidStr[0] < '0' || pidStr[0] > '9' {
			continue
		}

		// check symlink for native Steam games
		var gameName string
		exePath, err := os.Readlink(filepath.Join("/proc", pidStr, "exe")) // /proc/<pid>/exe
		if err == nil {
			// skip wrapper/launcher processes that carry game paths in their cmdline
			if ignoredProcesses[filepath.Base(exePath)] {
				continue
			}
			gameName = extractSteamGameName(exePath)
		}

		// fallback: check command line args (for proton games)
		if gameName == "" {
			gameName = scanCmdline(pidStr)
		}

		if gameName != "" && !isIgnoredGame(gameName) {
			pid, _ := strconv.Atoi(pidStr)
			return gameName, pid
		}
	}
	return "", 0
}

// read /etc/os-release to display in the Discord status
func readOSRelease() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		log.Printf("ERROR: Could not open /etc/os-release: %v", err)
		return runtime.GOOS
	}
	defer file.Close()

	distroInfo := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)

		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			distroInfo[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading /etc/os-release: %v", err)
		return runtime.GOOS
	}

	if name, ok := distroInfo["PRETTY_NAME"]; ok {
		return name
	} else if name, ok := distroInfo["NAME"]; ok {
		return name
	}
	return runtime.GOOS
}

// send the IPC packet to Discord to update your activity
func setActivity(conn net.Conn, appName string, pid int, osRelease string) error {
	activity := Activity{}

	if appName != "" {
		state := fmt.Sprintf("On %s", osRelease)
		activity = Activity{
			Details: "Playing " + appName,
			State:   state,
			Assets: ActivityAssets{
				LargeImage: "default",
				LargeText:  appName,
			},
		}
	}
	payload := DiscordRpcPayload{
		Cmd:   "SET_ACTIVITY",
		Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Args: ActivityArgs{
			Pid:      pid,
			Activity: activity,
		},
	}
	data, _ := json.Marshal(payload)
	return sendIPCPacket(conn, 1, data) // opcode 1 = frame
}

// load configuration from JSON
func loadConfig(configFile string) {
	file, err := os.ReadFile(configFile)
	if err != nil {
		log.Println("No config.json found. Using defaults.")
		return
	}

	var cfg Config
	if err := json.Unmarshal(file, &cfg); err != nil {
		log.Printf("Error parsing config.json: %v. Using defaults.", err)
		return
	}

	// set interval
	if cfg.ScanIntervalSeconds > 0 {
		scanInterval = time.Duration(cfg.ScanIntervalSeconds) * time.Second
	}
	log.Printf("Scan interval set to %v.", scanInterval)

	// merge ignored games
	for _, name := range cfg.IgnoredGames {
		ignoredGames[name] = true
	}
	log.Printf("Loaded %d ignored game entries.", len(ignoredGames))

	// merge ignored processes
	for _, name := range cfg.IgnoredProcesses {
		ignoredProcesses[name] = true
	}
	log.Printf("Loaded %d ignored process entries.", len(ignoredProcesses))

	// load manual game name -> Discord client ID mappings
	for name, id := range cfg.ManualMappings {
		manualMappings[name] = id
	}
	log.Printf("Loaded %d manual game mappings.", len(manualMappings))

	// set Discord API version in URL
	if cfg.DiscordApiVersion > 0 {
		discordApiUrl = fmt.Sprintf("https://discord.com/api/v%d/applications/detectable", cfg.DiscordApiVersion)
	}
	log.Printf("Using Discord API URL: %s", discordApiUrl)

	// set game data cache TTL
	if cfg.GameCacheTTLDays > 0 {
		gameCacheTTL = time.Duration(cfg.GameCacheTTLDays*24) * time.Hour
	}
	log.Printf("Game cache TTL set to %v.", gameCacheTTL)
}

// Paths is the resolved location of the config file and game cache file.
type Paths struct {
	Config string
	Cache  string
}

// resolvePaths picks development paths when run from the repo (config.json
// in cwd) and otherwise falls back to the user's standard config/cache dirs
// (~/.config and ~/.cache on Linux, honoring XDG_* if set).
func resolvePaths() Paths {
	const appName = "discord-rpc-bridge"

	cwd, _ := os.Getwd()
	localConfig := filepath.Join(cwd, "config.json")
	if _, err := os.Stat(localConfig); err == nil {
		log.Println("MODE: Development (repo paths)")
		return Paths{
			Config: localConfig,
			Cache:  filepath.Join(cwd, "data", "games.json"),
		}
	}

	log.Println("MODE: Deployed (user config/cache dirs)")

	configDir, _ := os.UserConfigDir()
	appConfigDir := filepath.Join(configDir, appName)
	_ = os.MkdirAll(appConfigDir, 0755)

	cacheDir, _ := os.UserCacheDir()
	appCacheDir := filepath.Join(cacheDir, appName)
	_ = os.MkdirAll(appCacheDir, 0755)

	return Paths{
		Config: filepath.Join(appConfigDir, "config.json"),
		Cache:  filepath.Join(appCacheDir, "games.json"),
	}
}

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println(version)
		return
	}

	log.Printf("Starting discord-rpc-bridge %s...", version)

	paths := resolvePaths()
	loadConfig(paths.Config)

	if err := loadGameData(paths.Cache); err != nil {
		log.Fatalf("Failed to load database: %v", err)
	}
	osRelease := readOSRelease()
	log.Printf("Detected OS release: %s", osRelease)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	socketPath, _ := findDiscordSocket()
	var currentClientID string
	var ipcConn net.Conn

	log.Printf("Starting process scanner with interval of %v second(s)", scanInterval.Seconds())
	scan := func() {
		gameName, pid := scanProcesses()

		if gameName == "" {
			// no game running, clear status if connected
			if ipcConn != nil {
				log.Println("No game found. Closing connection.")
				ipcConn.Close()
				ipcConn = nil
				currentClientID = ""
			}
			return
		}
		targetClientID := resolveClientID(gameName)

		// if connected, but ID wrong, disconnect
		if ipcConn != nil && currentClientID != targetClientID {
			log.Printf("Switching games (%s -> %s). Reconnecting...", currentClientID, targetClientID)
			ipcConn.Close()
			ipcConn = nil
		}

		// connect if disconnected
		if ipcConn == nil {
			if socketPath == "" {
				socketPath, _ = findDiscordSocket()
			}
			if socketPath != "" {
				conn, err := connectIPC(socketPath, targetClientID)
				if err == nil {
					ipcConn = conn
					currentClientID = targetClientID
					log.Printf("Connected to game %s (ID: %s)", gameName, targetClientID)
				} else {
					// clear socketPath so next tick re-probes; covers Discord
					// being closed/relaunched in a different flavor
					// (native ↔ Flatpak ↔ Snap) at a new socket path.
					log.Printf("Connection failed: %v. Re-probing socket next tick.", err)
					socketPath = ""
					return
				}
			}
		}

		// set activity if connected
		if ipcConn != nil {
			if err := setActivity(ipcConn, gameName, pid, osRelease); err != nil {
				log.Printf("Failed to set activity: %v. Reconnecting...", err)
				ipcConn.Close()
				ipcConn = nil
				currentClientID = ""
				socketPath = ""
			}
		}
	}

	scan()
	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down. Clearing Discord activity...")
			if ipcConn != nil {
				// best-effort: ask Discord to drop our activity, then close.
				// without this, Discord shows the stale "Playing X" until it
				// notices the broken pipe (can take a while).
				_ = setActivity(ipcConn, "", 0, osRelease)
				ipcConn.Close()
			}
			return
		case <-ticker.C:
			scan()
		}
	}
}
