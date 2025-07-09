package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "/opt/tunnel/var/db/tunnels.db"
const maxCreationAttempts = 5 // Max tunnel creation attempts per minute
const creationWindow = 1 * time.Minute

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if len(os.Args) < 3 {
		log.Println("Usage: alias-manager <get|create|activate|get-by-alias> <token|token:alias|token:public_port>")
		os.Exit(1)
	}

	cmd := os.Args[1]
	arg := os.Args[2]

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("DB Error: %v", err)
	}
	defer db.Close()

	switch cmd {
	case "get":
		token := arg
		row := db.QueryRow("SELECT alias, port, public_port FROM tunnels WHERE token = ? AND active = 1", token)
		var alias string
		var port, publicPort int
		if err := row.Scan(&alias, &port, &publicPort); err == nil {
			fmt.Printf("%s:%d:%d", alias, port, publicPort)
		} else if err != sql.ErrNoRows {
			log.Printf("Error getting tunnel for token %s: %v", token, err)
		}
	case "create":
		parts := strings.Split(arg, ":")
		token := parts[0]
		requestedAlias := ""
		if len(parts) == 2 {
			requestedAlias = parts[1]
		}

		if !canCreateTunnel(db, token) {
			log.Printf("Rate limit or tunnel limit exceeded for token: %s", token)
			os.Exit(1)
		}

		alias := requestedAlias
		if alias == "" {
			alias = generateAlias()
			for isReserved("alias", alias, db) {
				alias = generateAlias()
			}
		} else if isReserved("alias", alias, db) {
			log.Printf("Requested alias %s is reserved.", alias)
			os.Exit(1)
		}

		port := findFreePort(db)
		publicPort := port // For now, public port is same as local port

		_, err := db.Exec(`INSERT INTO tunnels 
						(token, alias, port, public_port, active, created_at, last_active) 
						VALUES (?, ?, ?, ?, 0, DATETIME('now'), DATETIME('now'))`,
			token, alias, port, publicPort)
		if err != nil {
			log.Printf("Error creating tunnel for token %s: %v", token, err)
			os.Exit(1)
		}
		fmt.Printf("%s:%d:%d", alias, port, publicPort)
		log.Printf("Tunnel created for token %s: %s:%d:%d", token, alias, port, publicPort)

	case "activate":
		parts := strings.Split(arg, ":")
		if len(parts) != 2 {
			log.Println("Usage: alias-manager activate <token>:<public_port>")
			os.Exit(1)
		}
		token := parts[0]
		publicPort, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Printf("Invalid public port: %v", err)
			os.Exit(1)
		}

		result, err := db.Exec("UPDATE tunnels SET active = 1, last_active = DATETIME('now') WHERE token = ? AND public_port = ?", token, publicPort)
		if err != nil {
			log.Printf("Error activating tunnel for token %s, port %d: %v", token, publicPort, err)
			os.Exit(1)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("No tunnel found or activated for token %s, port %d", token, publicPort)
			os.Exit(1)
		}
		log.Printf("Tunnel activated for token %s, port %d", token, publicPort)

	case "get-by-alias":
		alias := arg
		row := db.QueryRow("SELECT port, public_port FROM tunnels WHERE alias = ? AND active = 1", alias)
		var port, publicPort int
		if err := row.Scan(&port, &publicPort); err == nil {
			fmt.Printf("%d:%d", port, publicPort)
		} else if err != sql.ErrNoRows {
			log.Printf("Error getting tunnel by alias %s: %v", alias, err)
		}
	default:
		log.Println("Unknown command:", cmd)
		os.Exit(1)
	}
}

func generateAlias() string {
	adjectives := []string{"blue", "fast", "quiet", "red", "happy", "green"}
	animals := []string{"fox", "wolf", "bison", "hawk", "lion", "crab"}
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%s-%s", adjectives[rand.Intn(len(adjectives))], animals[rand.Intn(len(animals))])
}

func findFreePort(db *sql.DB) int {
	rand.Seed(time.Now().UnixNano())
	for {
		port := rand.Intn(60000-30000) + 30000 // Ports from 30000 to 59999
		var exists int
		db.QueryRow("SELECT COUNT(*) FROM tunnels WHERE public_port = ?", port).Scan(&exists)
		if exists == 0 && !isReserved("port", strconv.Itoa(port), db) {
			return port
		}
		time.Sleep(1 * time.Millisecond) // Avoid busy-waiting
	}
}

func isReserved(typ string, val string, db *sql.DB) bool {
	row := db.QueryRow("SELECT COUNT(*) FROM reserved WHERE alias_or_port = ? AND type = ?", val, typ)
	var count int
	row.Scan(&count)
	return count > 0
}

func canCreateTunnel(db *sql.DB, token string) bool {
	// Initialize user_stats if not exists
	_, err := db.Exec(`INSERT OR IGNORE INTO user_stats (token) VALUES (?)`, token)
	if err != nil {
		log.Printf("Error ensuring user_stats for token %s: %v", token, err)
		return false
	}

	var lastAttemptStr string
	var creationCount int
	var maxTunnels int

	row := db.QueryRow("SELECT last_creation_attempt, creation_count, max_tunnels FROM user_stats WHERE token = ?", token)
	if err := row.Scan(&lastAttemptStr, &creationCount, &maxTunnels); err != nil {
		log.Printf("Error reading user_stats for token %s: %v", token, err)
		return false
	}

	lastAttempt, err := time.Parse("2006-01-02 15:04:05", lastAttemptStr)
	// Handle potential fractional seconds from SQLite's DATETIME('now')
	if err != nil {
		lastAttempt, err = time.Parse("2006-01-02 15:04:05.000", lastAttemptStr)
		if err != nil {
			log.Printf("Error parsing last_creation_attempt for token %s: %v", token, err)
			return false
		}
	}

	// Check rate limit
	now := time.Now()
	if now.Sub(lastAttempt) < creationWindow {
		if creationCount >= maxCreationAttempts {
			log.Printf("Rate limit exceeded for token %s. Attempts: %d", token, creationCount)
			return false
		}
		creationCount++
	} else {
		creationCount = 1 // Reset count if outside window
	}

	// Check active tunnel limit
	var activeTunnels int
	row = db.QueryRow("SELECT COUNT(*) FROM tunnels WHERE token = ? AND active = 1", token)
	if err := row.Scan(&activeTunnels); err != nil {
		log.Printf("Error counting active tunnels for token %s: %v", token, err)
		return false
	}

	if activeTunnels >= maxTunnels {
		log.Printf("Active tunnel limit exceeded for token %s. Active: %d, Max: %d", token, activeTunnels, maxTunnels)
		return false
	}

	// Update user_stats
	_, err = db.Exec(`UPDATE user_stats SET last_creation_attempt = DATETIME('now'), creation_count = ? WHERE token = ?`,
		creationCount, token)
	if err != nil {
		log.Printf("Error updating user_stats for token %s: %v", token, err)
		return false
	}

	return true
}