package discovery

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

// GetRealHome returns the canonical user home directory even if running in an isolated subprofile
func GetRealHome() string {
	if realHome := os.Getenv("REAL_HOME"); realHome != "" {
		return realHome
	}
	envHome := os.Getenv("HOME")
	if strings.Contains(envHome, ".config/agy-profiles") {
		// e.g. /home/lucas/.config/agy-profiles/account5 -> /home/lucas
		parts := strings.Split(envHome, "/.config/agy-profiles")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
		return "/home/lucas"
	}
	if envHome != "" {
		return envHome
	}
	if uHome, err := os.UserHomeDir(); err == nil && uHome != "" {
		return uHome
	}
	return "/home/lucas"
}

// ExtractEmailFromToken parses oauth token file and decodes id_token JWT payload
func ExtractEmailFromToken(tokenPath string) (string, time.Time) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", time.Time{}
	}

	var raw struct {
		IDToken string `json:"id_token"`
		Token   struct {
			Expiry string `json:"expiry"`
		} `json:"token"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", time.Time{}
	}

	var expiry time.Time
	if raw.Token.Expiry != "" {
		expiry, _ = time.Parse(time.RFC3339, raw.Token.Expiry)
	}

	if raw.IDToken == "" {
		return "", expiry
	}

	parts := strings.Split(raw.IDToken, ".")
	if len(parts) < 2 {
		return "", expiry
	}

	payloadPart := parts[1]
	// Add padding if missing
	if pad := len(payloadPart) % 4; pad != 0 {
		payloadPart += strings.Repeat("=", 4-pad)
	}

	decoded, err := base64.URLEncoding.DecodeString(payloadPart)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payloadPart)
		if err != nil {
			return "", expiry
		}
	}

	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(decoded, &claims)

	return claims.Email, expiry
}

type accountDef struct {
	id       string
	alias    string
	homeDir  string
	priority int
}

func extractNumber(s string) int {
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(s)
	if match != "" {
		n, _ := strconv.Atoi(match)
		return n
	}
	return 0
}

// DiscoverProfiles finds all configured Antigravity accounts
func DiscoverProfiles() ([]*model.Profile, error) {
	realHome := GetRealHome()

	knownPriorities := map[string]int{
		"default":   50,
		"account2":  10,
		"account3":  100,
		"account4":  90,
		"account5":  80,
		"account6":  75,
		"account7":  70,
		"account8":  65,
		"account9":  60,
		"account10": 55,
	}

	baseAccounts := []accountDef{
		{id: "default", alias: "agy-1", homeDir: realHome, priority: 50},
	}

	profilesDir := filepath.Join(realHome, ".config/agy-profiles")
	entries, _ := os.ReadDir(profilesDir)

	var accountDirs []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "default" {
			accountDirs = append(accountDirs, entry.Name())
		}
	}

	sort.Slice(accountDirs, func(i, j int) bool {
		numI := extractNumber(accountDirs[i])
		numJ := extractNumber(accountDirs[j])
		if numI != numJ {
			return numI < numJ
		}
		return accountDirs[i] < accountDirs[j]
	})

	for _, dirName := range accountDirs {
		prio := 50
		if p, ok := knownPriorities[dirName]; ok {
			prio = p
		}
		num := extractNumber(dirName)
		alias := fmt.Sprintf("agy-%d", num)
		if num == 0 {
			alias = dirName
		}
		baseAccounts = append(baseAccounts, accountDef{
			id:       dirName,
			alias:    alias,
			homeDir:  filepath.Join(profilesDir, dirName),
			priority: prio,
		})
	}

	var profiles []*model.Profile

	for _, acc := range baseAccounts {
		appDataDir := filepath.Join(acc.homeDir, ".gemini/antigravity-cli")
		if acc.id == "default" {
			appDataDir = filepath.Join(realHome, ".gemini/antigravity-cli")
		}

		tokenPath := filepath.Join(appDataDir, "antigravity-oauth-token")
		email, expiry := ExtractEmailFromToken(tokenPath)

		p := &model.Profile{
			ID:           acc.id,
			Alias:        acc.alias,
			Email:        email,
			HomeDir:      acc.homeDir,
			AppDataDir:   appDataDir,
			Status:       model.StatusIdle,
			Priority:     acc.priority,
			TokenExpiry:  expiry,
			RunningPIDs:  []int{},
		}
		profiles = append(profiles, p)
	}

	// Map running PIDs to accounts
	runningMap := FindRunningAgyProcesses(realHome)
	for _, p := range profiles {
		if pids, exists := runningMap[p.HomeDir]; exists && len(pids) > 0 {
			p.IsRunning = true
			p.RunningPIDs = pids
			p.Status = model.StatusActive
			p.StatusReason = fmt.Sprintf("Running (%d process)", len(pids))
		}
	}

	// Check if synced in ~/.cli-proxy-api/
	cliProxyDir := filepath.Join(realHome, ".cli-proxy-api")
	for _, p := range profiles {
		if p.Email != "" {
			targetFile := filepath.Join(cliProxyDir, fmt.Sprintf("antigravity-%s.json", p.Email))
			if _, err := os.Stat(targetFile); err == nil {
				p.ProxySynced = true
			}
		}
	}

	return profiles, nil
}

var pidRegex = regexp.MustCompile(`^[0-9]+$`)

// FindRunningAgyProcesses searches /proc for running agy processes and maps home dir -> PIDs
func FindRunningAgyProcesses(realHome string) map[string][]int {
	result := make(map[string][]int)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() || !pidRegex.MatchString(entry.Name()) {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Match only actual Antigravity CLI binary executable, not node/python/MCP servers
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue
		}
		exeBase := filepath.Base(exe)
		if exeBase != "agy" && exeBase != "agy-real" && exeBase != "antigravity" && !strings.HasPrefix(exeBase, "antigravity") {
			continue
		}

		// Read environ to find HOME
		environBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			continue
		}

		parts := bytes.Split(environBytes, []byte{0})
		var procHome string
		for _, part := range parts {
			s := string(part)
			if strings.HasPrefix(s, "HOME=") {
				procHome = strings.TrimPrefix(s, "HOME=")
				break
			}
		}

		if procHome == "" {
			procHome = realHome
		}

		result[procHome] = append(result[procHome], pid)
	}

	// Sort PIDs
	for k := range result {
		sort.Ints(result[k])
	}

	return result
}
