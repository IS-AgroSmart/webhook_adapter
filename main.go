package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//const REMOTE_URL = "http://container-nodeodm:3000/task/%s/info"
var REMOTE_URL string  // http://localhost:3000/task/%s/info
var WEBHOOK_URL string // http://localhost:5000/webhook
var POLL_INTERVAL int

type TaskInfo struct {
	Status         TaskStatus `json:"status"`
	Uuid           string     `json:"uuid"`
	ProcessingTime int        `json:"processingTime"`
}

type TaskStatus struct {
	Code int `json:"code"`
}

func isComplete(info TaskInfo) bool {
	return info.Status.Code > 20
}

func register(key string) {
	log.Printf("Registering key %s", key)
	for {
		time.Sleep(time.Duration(POLL_INTERVAL) * time.Second)
		/* Response format:
				    {
		              "uuid": "a17d795b-2829-4e67-ad82-1143e4262dfa",
		              "name": "Task of 2021-03-20T17:00:59.435Z",
		              "dateCreated": 1616259659435,
		              "processingTime": 109162,
		              "status": {
		                "code": 20
		              },
		              "options": [],
		              "imagesCount": 3,
		              "progress": 54
		            }
		*/
		res, err := http.Get(fmt.Sprintf(REMOTE_URL, key))
		if err != nil {
			log.Printf("Error when polling task %s: %s\n", key, err)
			continue
		}

		var status TaskInfo
		json.NewDecoder(res.Body).Decode(&status)

		if isComplete(status) {
			for {
				// err should always be nil since we are remarshaling a simple object that actually came from JSON
				report, err := json.Marshal(status)
				_, err = http.Post(WEBHOOK_URL, "application/json", bytes.NewReader(report))
				if err == nil {
					// POST happened successfully. Bye...
					break // This will only break out of the innermost for!
				}
				// otherwise (error != nil), log error message, wait a bit and loop back (try to send webhook again)
				time.Sleep(time.Duration(POLL_INTERVAL) * time.Second)
				log.Printf("Unable to send webhook for task %s: %s\n", key, err)
			}
			err = os.Remove(fmt.Sprintf("pending/%s", key))
			if err != nil {
				// This is always a PathError
				log.Printf("Error deleting file pending/%s: %s\n", key, err)
			}

			log.Printf("Done watching task %s\n", key)
			return
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	key := strings.Split(r.URL.Path[1:], "/")[1] // URL will have form register/id, split on / and take the second element
	errored := false

	err := os.MkdirAll("pending", os.ModePerm) // Does nothing if pending already exists
	if err != nil {
		log.Printf("Error creating pending dir: %s\n", err)
		errored = true
	}
	_, err = os.Create(fmt.Sprintf("pending/%s", key))
	if err != nil {
		log.Printf("Error creating file pending/%s: %s\n", key, err)
		errored = true
	}
	// No need to open the file, we just want to specify the filename

	if errored {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "ERROR")
	} else {
		go register(key) // Kick off a separate goroutine that will loop for a loooong time
		fmt.Fprintf(w, "OK")
	}
}

func _getEnvvarOrDefault(key, defaultValue string) string {
	val, ok := os.LookupEnv(key)
	if !ok { // Use default value of 8080
		log.Printf("Using default value for %s: %s\n", key, defaultValue)
		return defaultValue
	} else {
		return val
	}
}

func _getEnvvarOrError(key string) string {
	val, ok := os.LookupEnv(key)
	if !ok { // Use default value of 8080
		log.Fatalf("Please provide a value for %s\n", key)
	}
	return val
}

func getPort() string {
	return _getEnvvarOrDefault("PORT", "8080")
}

func getRemoteUrl() string {
	return _getEnvvarOrError("REMOTE_URL")
}

func getWebhookUrl() string {
	return _getEnvvarOrError("WEBHOOK_URL")
}

func getPollInterval() int {
	interval := _getEnvvarOrError("POLL_INTERVAL")
	i, err := strconv.Atoi(interval)
	if err != nil {
		log.Fatalf("Error when parsing POLL_INTERVAL %s: %s", interval, err)
	}
	return i
}

func main() {
	matches, err := filepath.Glob("pending/*")
	fmt.Printf("Initializing pending tasks: %v\n", matches)
	if err != nil {
		fmt.Printf("Error initializing pending tasks: %s\n", err)
	} else {
		for _, filename := range matches {
			// filename has the form "pending/something", we just want the "something" part
			go register(strings.Split(filename, "/")[1]) // kick off goroutines for tasks that were pending when process was shut off
		}
	}

	REMOTE_URL = getRemoteUrl()
	WEBHOOK_URL = getWebhookUrl()
	POLL_INTERVAL = getPollInterval()

	http.HandleFunc("/register/", handler)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", getPort()), nil))
}
