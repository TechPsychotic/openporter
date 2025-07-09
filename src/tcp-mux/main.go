
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "/opt/tunnel/var/db/tunnels.db"

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("DB Error: %v", err)
	}
	defer db.Close()

	go cleanupStaleTunnels(db)

	// Map to keep track of active listeners to avoid re-listening on the same port
	activeListeners := make(map[int]net.Listener)

	for {
		rows, err := db.Query("SELECT public_port, port, type FROM tunnels WHERE active = 1")
		if err != nil {
			log.Printf("DB Error fetching active tunnels: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		currentTunnels := make(map[int]struct{}) // public_port -> exists
		for rows.Next() {
			var publicPort, localPort int
			var tunnelType string // Read the type, though not used for basic TCP forwarding yet
			if err := rows.Scan(&publicPort, &localPort, &tunnelType); err != nil {
				log.Printf("DB Scan Error: %v", err)
				continue
			}
			currentTunnels[publicPort] = struct{}{} // Mark as active

			// Start proxy if not already active
			if _, exists := activeListeners[publicPort]; !exists {
				ln, err := net.Listen("tcp", fmt.Sprintf(":%d", publicPort))
				if err != nil {
					log.Printf("Failed to listen on port %d: %v", publicPort, err)
					continue
				}
				activeListeners[publicPort] = ln
				log.Printf("Started listening on public port %d (Type: %s), forwarding to local port %d", publicPort, tunnelType, localPort)
				go acceptConnections(ln, publicPort, localPort, db)
			}
		}
		rows.Close()

		// Close listeners for tunnels that are no longer active
		for publicPort, ln := range activeListeners {
			if _, exists := currentTunnels[publicPort]; !exists {
				log.Printf("Closing listener for public port %d (tunnel no longer active)", publicPort)
				ln.Close()
				delete(activeListeners, publicPort)
			}
		}

		time.Sleep(10 * time.Second)
	}
}

func acceptConnections(ln net.Listener, publicPort, localPort int, db *sql.DB) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener might have been closed from main loop
			log.Printf("Failed to accept connection on port %d: %v", publicPort, err)
			return
		}
		go handleConnection(conn, publicPort, localPort, db)
	}
}

func handleConnection(conn net.Conn, publicPort, localPort int, db *sql.DB) {
	defer conn.Close()

	log.Printf("New connection from %s to public port %d, forwarding to local port %d", conn.RemoteAddr(), publicPort, localPort)

	target, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		log.Printf("Failed to connect to local port %d for %s: %v", localPort, conn.RemoteAddr(), err)
		return
	}
	defer target.Close()

	// Update last_active timestamp for the tunnel
	go func() {
		_, err := db.Exec("UPDATE tunnels SET last_active = DATETIME('now') WHERE public_port = ?", publicPort)
		if err != nil {
			log.Printf("Error updating last_active for public port %d: %v", publicPort, err)
		}
	}()

	// Bidirectional copy
	done := make(chan struct{})
	go func() {
		io.Copy(target, conn)
		close(done)
	}()
	io.Copy(conn, target)
	<-done

	log.Printf("Connection from %s to public port %d closed", conn.RemoteAddr(), publicPort)
}

func cleanupStaleTunnels(db *sql.DB) {
	for {
		time.Sleep(5 * time.Minute)
		result, err := db.Exec("DELETE FROM tunnels WHERE active = 0 AND created_at < DATETIME('now', '-10 minutes')")
		if err != nil {
			log.Printf("Failed to cleanup unactivated stale tunnels: %v", err)
		} else {
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				log.Printf("Cleaned up %d unactivated stale tunnels.", rowsAffected)
			}
		}

		result, err = db.Exec("DELETE FROM tunnels WHERE active = 1 AND last_active < DATETIME('now', '-30 minutes')")
		if err != nil {
			log.Printf("Failed to cleanup activated stale tunnels: %v", err)
		} else {
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				log.Printf("Cleaned up %d activated stale tunnels.", rowsAffected)
			}
		}
	}
}
