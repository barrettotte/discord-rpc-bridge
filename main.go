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
	APIUrl       = "https://discord.com/api/v10/applications/detectable"
	CacheFile    = "data/games.json"
	ScanInterval = 5 * time.Second
)

// folder names to ignore in steamapps/common
var ignoredGames = map[string]bool{
	"SteamLinuxRuntime_soldier": true,
	"SteamLinuxRuntime_sniper":  true,
	"SteamLinuxRuntime":         true,
	"SteamControllerConfigs":    true,
	"Proton Experimental":       true,
	"Proton 7.0":                true,
	"Proton 8.0":                true,
	"Proton 9.0":                true,
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

var nameToID = make(map[string]string)

func populateMap(apps []DetectableApp) {
	for _, app := range apps {
		nameToID[normalizeGameName(app.Name)] = app.ID
	}
	log.Printf("Indexed %d known games.", len(nameToID))
}

func loadGameData() error {
	// TODO: force refresh cache after a day

	file, err := os.ReadFile(CacheFile)

	if err != nil {
		log.Println("Fetching games from Discord API...")
		resp, err := http.Get(APIUrl)

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

func normalizeGameName(input string) string {
	reg := regexp.MustCompile(`[^a-z0-9]`)
	return reg.ReplaceAllString(strings.ToLower(input), "")
}

func resolveClientID(name string) string {
	norm := normalizeGameName(name)
	if id, ok := nameToID[norm]; ok {
		return id
	}
	return "000000000000000000" // does not seem to work
}

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

		exePath, err := os.Readlink(filepath.Join("/proc", pidStr, "exe")) // /proc/<pid>/exe
		if err != nil {
			continue
		}

		name := extractSteamGameName(exePath)
		if name != "" && !ignoredGames[name] {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			return name, pid
		}
	}
	return "", 0
}

func readOSRelease() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		fmt.Printf("ERROR: Could not open /etc/os-release: %v", err)
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
		fmt.Printf("Error reading /etc/os-release: %v", err)
		return runtime.GOOS
	}

	if name, ok := distroInfo["PRETTY_NAME"]; ok {
		return name
	} else if name, ok := distroInfo["NAME"]; ok {
		return name
	}
	return runtime.GOOS
}

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
			Pid:      os.Getpid(),
			Activity: activity,
		},
	}

	data, _ := json.Marshal(payload)
	return sendIPCPacket(conn, 1, data) // opcode 1 = frame
}

func main() {
	log.Println("Starting discord-rpc-bridge...")

	if err := loadGameData(); err != nil {
		log.Fatalf("Failed to load database: %v", err)
	}
	osRelease := readOSRelease()

	ticker := time.NewTicker(ScanInterval)
	var socketPath, _ = findDiscordSocket()
	var currentClientID string
	var ipcConn net.Conn

	// run on schedule
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
