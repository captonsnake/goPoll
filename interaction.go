package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/chris-hamper/go-slack-poll/poll"
	"github.com/nlopes/slack"
)

// interactionHandler handles interactive message response.
type interactionHandler struct {
	signingSecret []byte
}

func (h interactionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[ERROR] Invalid method: %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Only accept message from slack with valid token.
	if !validateRequest(r, h.signingSecret) {
		log.Printf("[ERROR] Message validation failed")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("[ERROR] Failed to read request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jsonStr, err := url.QueryUnescape(string(buf)[8:])
	if err != nil {
		log.Printf("[ERROR] Failed to unespace request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var message slack.AttachmentActionCallback
	if err = json.Unmarshal([]byte(jsonStr), &message); err != nil {
		log.Printf("[ERROR] Failed to decode json message from slack: %s", jsonStr)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	action := message.Actions[0]
	parts := strings.Split(action.Name, "_")
	p := poll.GetPollByID(parts[0])

	var response *slack.Msg
	switch parts[1] {
	case "delete":
		if message.User.ID == p.Owner {
			p.Delete()
		} else {
			response = &slack.Msg{
				ResponseType:    "ephemeral",
				ReplaceOriginal: false,
				Text:            "Sorry, only <@" + p.Owner + "> can delete this poll.",
			}
		}

	default:
		optionIndex, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Printf("[ERROR] Invalid action name: %s", action.Name)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		p.ToggleVote(message.User.ID, optionIndex)
		p.Save()
	}

	if response == nil {
		response = &slack.Msg{
			ResponseType:    "in_channel",
			ReplaceOriginal: true,
			Attachments:     []slack.Attachment{*p.ToSlackAttachment()},
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = encoder.Encode(&response)
	if err != nil {
		log.Println("[ERROR] JSON Encode failed:", err)
	}
}
