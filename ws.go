package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var jobsConnections = make(map[*websocket.Conn]*sync.Mutex)
var systemConnections = make(map[*websocket.Conn]*sync.Mutex)
var logsConnections = make(map[*websocket.Conn]*sync.Mutex)

var jobsMutex sync.RWMutex
var systemMutex sync.RWMutex
var logsMutex sync.RWMutex

func handleJobsWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		LogError("Jobs WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	jobsMutex.Lock()
	jobsConnections[conn] = &sync.Mutex{}
	jobsMutex.Unlock()

	// Send initial data
	runningData, err := GetRunningJobsWithSummary()
	if err != nil {
		runningData = map[string]interface{}{
			"jobs":           []map[string]interface{}{},
			"summaries":      []map[string]interface{}{},
			"total_jobs":     0,
			"total_running":  0,
			"completed_jobs": 0,
		}
	}

	conn.WriteJSON(map[string]interface{}{
		"type": "jobs_update",
		"data": runningData,
	})

	// Ping every 30s
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			conn.WriteMessage(websocket.PingMessage, nil)
		default:
			_, _, err := conn.ReadMessage()
			if err != nil {
				jobsMutex.Lock()
				delete(jobsConnections, conn)
				jobsMutex.Unlock()
				return
			}
		}
	}
}

func broadcastJobsUpdate() {
	runningData, err := GetRunningJobsWithSummary()
	if err != nil {
		runningData = map[string]interface{}{
			"jobs":           []map[string]interface{}{},
			"summaries":      []map[string]interface{}{},
			"total_jobs":     0,
			"total_running":  0,
			"completed_jobs": 0,
		}
	}

	message := map[string]interface{}{
		"type": "jobs_update",
		"data": runningData,
	}

	jobsMutex.RLock()
	// Create a copy of connections to avoid holding the lock while writing
	connections := make(map[*websocket.Conn]*sync.Mutex)
	for conn, mutex := range jobsConnections {
		connections[conn] = mutex
	}
	jobsMutex.RUnlock()

	// Write to each connection with its own mutex
	for conn, mutex := range connections {
		mutex.Lock()
		if err := conn.WriteJSON(message); err != nil {
			// Connection is likely closed, remove it
			jobsMutex.Lock()
			delete(jobsConnections, conn)
			jobsMutex.Unlock()
		}
		mutex.Unlock()
	}
}

func handleSystemWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		LogError("System WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	systemMutex.Lock()
	systemConnections[conn] = &sync.Mutex{}
	systemMutex.Unlock()

	// Send initial data
	metrics := getSystemMetrics()
	conn.WriteJSON(map[string]interface{}{
		"type": "system_update",
		"data": metrics,
	})

	// Ping every 30s
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			conn.WriteMessage(websocket.PingMessage, nil)
		default:
			_, _, err := conn.ReadMessage()
			if err != nil {
				systemMutex.Lock()
				delete(systemConnections, conn)
				systemMutex.Unlock()
				return
			}
		}
	}
}

func broadcastSystemUpdate() {
	metrics := getSystemMetrics()
	message := map[string]interface{}{
		"type": "system_update",
		"data": metrics,
	}

	systemMutex.RLock()
	// Create a copy of connections to avoid holding the lock while writing
	connections := make(map[*websocket.Conn]*sync.Mutex)
	for conn, mutex := range systemConnections {
		connections[conn] = mutex
	}
	systemMutex.RUnlock()

	// Write to each connection with its own mutex
	for conn, mutex := range connections {
		mutex.Lock()
		if err := conn.WriteJSON(message); err != nil {
			// Connection is likely closed, remove it
			systemMutex.Lock()
			delete(systemConnections, conn)
			systemMutex.Unlock()
		}
		mutex.Unlock()
	}
}

func handleLogsWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		LogError("Logs WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	logsMutex.Lock()
	logsConnections[conn] = &sync.Mutex{}
	logsMutex.Unlock()

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	conn.WriteJSON(map[string]interface{}{
		"type": "logs_connected",
		"date": date,
	})

	// Ping every 30s
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			conn.WriteMessage(websocket.PingMessage, nil)
		default:
			_, _, err := conn.ReadMessage()
			if err != nil {
				logsMutex.Lock()
				delete(logsConnections, conn)
				logsMutex.Unlock()
				return
			}
		}
	}
}

func startSystemMetricsBroadcaster() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		go broadcastSystemUpdate()
	}
}

func startJobsBroadcaster() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		go broadcastJobsUpdate()
	}
}

func getWebSocketConnectionCounts() map[string]int {
	jobsMutex.RLock()
	jobsCount := len(jobsConnections)
	jobsMutex.RUnlock()

	systemMutex.RLock()
	systemCount := len(systemConnections)
	systemMutex.RUnlock()

	logsMutex.RLock()
	logsCount := len(logsConnections)
	logsMutex.RUnlock()

	return map[string]int{
		"jobs":   jobsCount,
		"system": systemCount,
		"logs":   logsCount,
		"total":  jobsCount + systemCount + logsCount,
	}
}

func SetupLogBroadcasting() {
	broadcastLogEntry = func(level, message string) {
		logsMutex.RLock()
		if len(logsConnections) == 0 {
			logsMutex.RUnlock()
			return
		}
		logsMutex.RUnlock()

		now := time.Now()
		logEntry := map[string]interface{}{
			"level":         level,
			"timestamp":     now.Format("15:04:05"),
			"fullTimestamp": now.UnixNano(),
			"message":       message,
			"color":         getLogLevelColor(level),
			"raw":           fmt.Sprintf("%s [%s] %s", now.Format("15:04:05"), level, message),
		}

		wsMessage := map[string]interface{}{
			"type": "logs_update",
			"log":  logEntry,
			"date": now.Format("2006-01-02"),
		}

		logsMutex.RLock()
		// Create a copy of connections to avoid holding the lock while writing
		connections := make(map[*websocket.Conn]*sync.Mutex)
		for conn, mutex := range logsConnections {
			connections[conn] = mutex
		}
		logsMutex.RUnlock()

		// Write to each connection with its own mutex
		for conn, mutex := range connections {
			mutex.Lock()
			if err := conn.WriteJSON(wsMessage); err != nil {
				// Connection is likely closed, remove it
				logsMutex.Lock()
				delete(logsConnections, conn)
				logsMutex.Unlock()
			}
			mutex.Unlock()
		}
	}

	broadcastLogEntryWithTime = func(level, message string, timestamp time.Time) {
		logsMutex.RLock()
		if len(logsConnections) == 0 {
			logsMutex.RUnlock()
			return
		}
		logsMutex.RUnlock()

		logEntry := map[string]interface{}{
			"level":         level,
			"timestamp":     timestamp.Format("15:04:05"),
			"fullTimestamp": timestamp.UnixNano(),
			"message":       message,
			"color":         getLogLevelColor(level),
			"raw":           fmt.Sprintf("%s [%s] %s", timestamp.Format("15:04:05"), level, message),
		}

		wsMessage := map[string]interface{}{
			"type": "logs_update",
			"log":  logEntry,
			"date": timestamp.Format("2006-01-02"),
		}

		logsMutex.RLock()
		// Create a copy of connections to avoid holding the lock while writing
		connections := make(map[*websocket.Conn]*sync.Mutex)
		for conn, mutex := range logsConnections {
			connections[conn] = mutex
		}
		logsMutex.RUnlock()

		// Write to each connection with its own mutex
		for conn, mutex := range connections {
			mutex.Lock()
			if err := conn.WriteJSON(wsMessage); err != nil {
				// Connection is likely closed, remove it
				logsMutex.Lock()
				delete(logsConnections, conn)
				logsMutex.Unlock()
			}
			mutex.Unlock()
		}
	}
}

func getLogLevelColor(level string) string {
	switch level {
	case "DEBUG":
		return "debug"
	case "INFO":
		return "info"
	case "WARN", "WARNING":
		return "warn"
	case "ERROR", "ERR":
		return "error"
	case "FATAL":
		return "error"
	default:
		return "info"
	}
}
