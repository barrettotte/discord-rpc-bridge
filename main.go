package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	CacheFile  = "data/games.json"
	ConfigFile = "config.json"
)

var (
	discordApiUrl = "https://discord.com/api/v10/applications/detectable"
	scanInterval  = 5 * time.Second
	ignoredGames  = map[string]bool{
		"SteamLinuxRuntime_soldier": true,
		"SteamLinuxRuntime_sniper":  true,
		"SteamLinuxRuntime":         true,
	}
	nameToID = make(map[string]string)
)

type Config struct {
	ScanIntervalSeconds int      `json:"scan_interval_seconds"`
	IgnoredGames        []string `json:"ignored_games"`
	DiscordApiVersion   int      `json:"discord_api_version"`
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
func loadGameData() error {
	// TODO: force refresh cache after a day

	file, err := os.ReadFile(CacheFile)

	if err != nil {
		log.Println("Fetching games from Discord API...")
		resp, err := http.Get(discordApiUrl)

		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var apps []DetectableApp
		if err := json.NewDecoder(resp.Body).Decode(&apps); err != nil {
			return err
		}

		// cache data
		data, _ := json.Marshal(apps)
		os.WriteFile(CacheFile, data, 0644)

		// build map
		populateMap(apps)
		return nil
	}

	// read from cache
	var apps []DetectableApp
	if err := json.Unmarshal(file, &apps); err != nil {
		return err
	}
	populateMap(apps)
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
	// read header (8 bytes)
	header := make([]byte, 8)
	_, err := conn.Read(header)
	if err != nil {
		log.Printf("ERROR: Failed to read header: %v", err)
		return
	}

	// parse length (last 4 bytes of header)
	dataLen := binary.LittleEndian.Uint32(header[4:8])

	// read the payload
	payload := make([]byte, dataLen)
	_, err = conn.Read(payload)
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
	reg := regexp.MustCompile(`[^a-z0-9]`)
	return reg.ReplaceAllString(strings.ToLower(input), "")
}

// find Discord client ID of provided game
func resolveClientID(name string) string {
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

// given path with steamapps/common, extract the steam game folder name
func extractSteamGameName(fullPath string) string {
	const key = "steamapps/common"
	idx := strings.Index(fullPath, key)
	if idx == -1 {
		return ""
	}

	// strip everything before game name
	name := fullPath[idx+len(key):]
	name = strings.TrimPrefix(name, string(filepath.Separator))

	// extract first directory component
	parts := strings.SplitN(name, string(filepath.Separator), 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// try to find the game name from the processe's cmdline args (for proton games)
func scanCmdline(pidStr string) string {
	// /proc/<pid>/cmdline args separated by null bytes (\0)
	data, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline"))
	if err != nil {
		return ""
	}

	args := bytes.Split(data, []byte{0})
	for _, arg := range args {
		if len(arg) == 0 {
			continue
		}
		path := string(arg)
		name := extractSteamGameName(path)

		if name != "" && !ignoredGames[name] {
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
		exePath, err := os.Readlink(filepath.Join("/proc", pidStr, "exe")) // /proc/<pid>/exe
		if err == nil {
			name := extractSteamGameName(exePath)
			if name != "" && !ignoredGames[name] {
				var pid int
				fmt.Sscanf(pidStr, "%d", &pid)
				return name, pid
			}
		}

		// fallback: check command line args (for proton games)
		name := scanCmdline(pidStr)
		if name != "" && !ignoredGames[name] {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			return name, pid
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
	var activity Activity = Activity{}

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
func loadConfig() {
	file, err := os.ReadFile(ConfigFile)
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
	log.Printf("Scan interval set to %d second(s).", cfg.ScanIntervalSeconds)

	// merge ignored games
	for _, name := range cfg.IgnoredGames {
		ignoredGames[name] = true
	}
	log.Printf("Loaded %d ignored entries.", len(ignoredGames))

	// set Discord API version in URL
	if cfg.DiscordApiVersion > 0 {
		discordApiUrl = fmt.Sprintf("https://discord.com/api/v%d/applications/detectable", cfg.DiscordApiVersion)
	}
	log.Printf("Using Discord API URL: %s", discordApiUrl)
}

func main() {
	log.Println("Starting discord-rpc-bridge...")

	loadConfig()

	if err := loadGameData(); err != nil {
		log.Fatalf("Failed to load database: %v", err)
	}
	osRelease := readOSRelease()
	log.Printf("Detected OS release: %s", osRelease)

	ticker := time.NewTicker(scanInterval)
	var socketPath, _ = findDiscordSocket()
	var currentClientID string
	var ipcConn net.Conn

	// run on schedule
	log.Printf("Starting process scanner with interval of %v second(s)", scanInterval.Seconds())
	for range ticker.C {
		gameName, pid := scanProcesses()

		if gameName == "" {
			// no game running, clear status if connected
			if ipcConn != nil {
				log.Println("No game found. Closing connection.")
				ipcConn.Close()
				ipcConn = nil
				currentClientID = ""
			}
			continue
		}
		targetClientID := resolveClientID(gameName)

		// if connected, bt ID wrong, disconnect
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
					log.Printf("Connection failed: %v", err)
					continue
				}
			}
		}

		// set activity if connected
		if ipcConn != nil {
			setActivity(ipcConn, gameName, pid, osRelease)
		}
	}
}
