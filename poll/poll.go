package poll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/redis"
	"github.com/nlopes/slack"
)

// expiration holds the number of seconds before polls are removed from Redis (60 days).
const expiration = 24 * 60 * 60

// Option represents on option in a poll, and holds the users who have voted for it.
type Option struct {
	Name   string
	Voters []string
	mux    sync.Mutex // Protects "Voters" from being modified in parallel.
}

// Poll holds all information related to a poll created via Slack.
type Poll struct {
	ID      string
	Owner   string
	Title   string
	Options []Option
	Deleted bool
}

var db *pool.Pool

func init() {
	var err error
	redisHost := os.Getenv("REDIS_HOST")
	_, hasAuth := os.LookupEnv("REDIS_PASSWORD")

	if hasAuth {
		db, err = pool.NewCustom("tcp", redisHost+":6379", 10, authDial)
	} else {
		db, err = pool.New("tcp", redisHost+":6379", 10)
	}
	if err != nil {
		log.Panic("Redis pool connections failed:", err)
	}
}

func authDial(network, addr string) (*redis.Client, error) {
	passwd := os.Getenv("REDIS_PASSWORD")

	client, err := redis.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	if err = client.Cmd("AUTH", passwd).Err; err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}

// CreatePoll creates a new Poll.
func CreatePoll(owner, title string, options []string) *Poll {
	id, err := db.Cmd("INCR", "next-poll").Int()
	if err != nil {
		log.Println("[ERROR] Can't get next poll ID:", err)
		return nil
	}

	poll := Poll{
		ID:      strconv.Itoa(id),
		Owner:   owner,
		Title:   title,
		Options: make([]Option, len(options) + 1),
	}
	for i, name := range options {
		poll.Options[i].Name = name
	}
    // Adds a Default no response catagory to vote on
    poll.Options[len(options)].Name = "No Response"
	log.Println("[INFO] CreatePoll:", poll)
	return &poll
}

// GetPollByID gets the Poll with the given ID from the database, or nil.
func GetPollByID(id string) *Poll {
	s, err := db.Cmd("GET", "poll:"+id).Str()
	if err != nil {
		log.Println("[ERROR] Can't get poll from Redis store:", err)
		return nil
	}

	var p Poll
	dec := json.NewDecoder(strings.NewReader(s))
	err = dec.Decode(&p)
	if err != nil {
		log.Println("[ERROR] Can't decode poll:", err)
		return nil
	}
	return &p
}

// Save stores the Poll in the database.
func (p *Poll) Save() {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	enc.Encode(p)
	s := b.String()
	log.Println("[INFO] Saving poll to Redis store:", s)

	pollKey := "poll:" + p.ID
	err := db.Cmd("SET", pollKey, s, "EX", expiration).Err
	if err != nil {
		log.Println("[ERROR] Can't save poll", pollKey, "to Redis store:", err)
	}
}

// Sets all users to No Response option initially
func (p *Poll) SetDefault() {
    api := slack.New("xoxb-220025779893-1078147596145-wjJ9XMVJNaBCR701CxApWk5m")
    params := slack.GetUsersInConversationParameters{
        ChannelID: "C0108K46K8Q",
    }

    log.Println("[INFO] Getting members of channel")
    members, _, err := api.GetUsersInConversation(&params)

    if err != nil {
        log.Println("[ERROR] Unable to get members of channel")
        return
    }
    log.Printf("[INFO] Users: %v", members)

    for _, member := range members {
        //convert member to userID?
        user, err := api.GetUserInfo(member)
        if err != nil {
            log.Printf("[ERROR] Unable to get ID: %v", member)
        } else {
		option := &p.Options[len(p.Options) - 1]
		option.mux.Lock()
		defer option.mux.Unlock()
		option.Voters = append(option.Voters, user.Name)
        }
	}
}

// ToggleVote inverts the voting status for the given user on a given option.
func (p *Poll) ToggleVote(user string, optionIndex int) {
	log.Println("[INFO] toggleVote:", user, optionIndex)

	option := &p.Options[optionIndex]

	option.mux.Lock()
	defer option.mux.Unlock()
	for i, voter := range option.Voters {
		if voter == user {
			// Remove voter from the list.
			option.Voters = append(option.Voters[:i], option.Voters[i+1:]...)
			return
		}
	}

	// User wasn't found in the list of voters, so append it.
	option.Voters = append(option.Voters, user)
}

// ToSlackAttachment renders a Poll into a Slack message Attachment.
func (p *Poll) ToSlackAttachment() *slack.Attachment {
	if p.Deleted {
		return &slack.Attachment{
			Title:      "Poll deleted.",
			Fallback:   "Please use a client that supports interactive messages to see this poll.",
			CallbackID: p.ID,
		}
	}

	numOptions := len(p.Options)
	actions := make([]slack.AttachmentAction, numOptions+1)
	fields := make([]slack.AttachmentField, numOptions)

	prefix := p.ID + "_"
	for i := range p.Options {
		option := &p.Options[i]

		actions[i] = slack.AttachmentAction{
			Name: prefix + strconv.Itoa(i),
			Text: option.Name,
			Type: "button",
		}

		var votersStr string
		if len(option.Voters) == 0 {
			votersStr = "(none)"
		} else {
			votersStr = ""
			for _, userID := range option.Voters {
                // Pads names to 32
				votersStr += fmt.Sprintf("<@%v> ", userID)
			}
		}

		fields[i] = slack.AttachmentField{
			Title: fmt.Sprintf("%v (%v)", option.Name, len(option.Voters)),
			Value: votersStr,
			Short: false,
		}
	}

	// Append "Delete Poll" action.
	actions[numOptions] = slack.AttachmentAction{
		Name:  prefix + "delete",
		Text:  "Delete Poll",
		Type:  "button",
		Style: "danger",
		Confirm: &slack.ConfirmationField{
			Title:       "Delete poll \"" + p.Title + "\"?",
			OkText:      "Delete Poll",
			DismissText: "Keep Poll",
		},
	}

	return &slack.Attachment{
		Title:      "Poll: " + p.Title,
		Fallback:   "Please use a client that supports interactive messages to see this poll.",
		CallbackID: p.ID,
		Fields:     fields,
		Actions:    actions,
	}
}

// Delete marks the poll as deleted.
func (p *Poll) Delete() {
	p.Deleted = true
	p.Save()
}
