package main

import (
  "encoding/csv"
  "encoding/json"
	"strings"
	"log"
	"net/http"

	"github.com/chris-hamper/go-slack-poll/poll"
	"github.com/nlopes/slack"
)

type commandHandler struct {
  signingSecret []byte
}

func (h commandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[ERROR] Invalid method: %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

  if !validateRequest(r, h.signingSecret) {
    log.Printf("[ERROR] Message validation failed")
    w.WriteHeader(http.StatusUnauthorized)
    return
  }

  // @todo - sanitization?
  cmd, err := slack.SlashCommandParse(r)
  if err != nil {
    log.Println("[ERROR] SlashCommandParse failed:", err)
    w.WriteHeader(http.StatusInternalServerError)
    return
  }
  log.Println("[DEBUG] Command:", cmd)

  switch cmd.Command {
  case "/pollbot":
    // Clean up "smart quotes".
    text := strings.Map(normalizeQuotes, cmd.Text)

    // Split command text on spaces, except inside quotes.
    csv := csv.NewReader(strings.NewReader(text))
    csv.Comma = ' '
    args, err := csv.Read()
    if err != nil {
      log.Println("[ERROR] Command text split failed:", err)
      w.WriteHeader(http.StatusInternalServerError)
      return
    }

    // Create the poll.
    p := poll.CreatePoll(cmd.UserID, args[0], args[1:])
    p.Save()

    params := &slack.Msg{
      ResponseType: "in_channel",
      Attachments:  []slack.Attachment{*p.ToSlackAttachment()},
    }

    b, err := json.Marshal(params)
    if err != nil {
      log.Println("[ERROR] JSON Marshal failed:", err)
      w.WriteHeader(http.StatusInternalServerError)
      return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(b)

  default:
    w.WriteHeader(http.StatusInternalServerError)
    return
  }
}

func normalizeQuotes(r rune) rune {
	switch r {
	case '“', '”':
		return '"'
	}
	return r
}
