package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//var smartQuoteReplacer = strings.NewReplacer("“", "\"", "”", "\"")

func main() {
	signingSecret := []byte(strings.TrimSpace("197b7e61b78dbeec622b2e4a63190797"))

	// @todo - move to separate handler file
	http.Handle("/command", commandHandler{
		signingSecret: signingSecret,
	})

	// Register handler to receive interactive messages from slack.
	http.Handle("/interaction", interactionHandler{
		signingSecret: signingSecret,
	})

	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Println("[INFO] Server listening on port 3000")
	http.ListenAndServe(":4000", nil)
}

func validateRequest(r *http.Request, signingSecret []byte) bool {
	timestamp := r.Header["X-Slack-Request-Timestamp"][0]

	// Verify the timestamp is less than 5 minutes old, to avoid replay attacks.
	now := time.Now().Unix()
	messageTime, err := strconv.ParseInt(timestamp, 0, 64)
	if err != nil {
		log.Println("[ERROR] Invalid timestamp:", timestamp)
		return false
	}
	if math.Abs(float64(now-messageTime)) > 5*60 {
		log.Println("[ERROR] Timestamp is from > 5 minutes from now")
		return false
	}

	// Get the signature and signing version from the HTTP header.
	parts := strings.Split(r.Header["X-Slack-Signature"][0], "=")
	if parts[0] != "v0" {
		log.Println("[ERROR] Unsupported signing version:", parts[0])
		return false
	}
	signature, err := hex.DecodeString(parts[1])
	if err != nil {
		log.Println("[ERROR] Invalid message signature:", parts[1])
		return false
	}

	// Read the request body.
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println("[ERROR] Can't read request body:", err)
		return false
	}

	// Generate the HMAC hash.
	prefix := fmt.Sprintf("v0:%v:", timestamp)

	hash := hmac.New(sha256.New, []byte(signingSecret))
	hash.Write([]byte(prefix))
	hash.Write(body)

	// Reset the request body so it can be read again later.
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	// Verify our hash matches the signature.
	return hmac.Equal(hash.Sum(nil), []byte(signature))
}
