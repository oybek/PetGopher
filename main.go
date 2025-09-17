package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

// Config holds app configuration
type Config struct {
	TelegramToken string
}

func loadConfig() *Config {
	return &Config{
		TelegramToken: os.Getenv("TELEGRAM_TOKEN"),
	}
}

// Go Playground execute request/response
type playgroundRequest struct {
	Version int    `json:"version"`
	Body    string `json:"body"`
}

type PlaygroundEvent struct {
	Message string `json:"Message"`
	Kind    string `json:"Kind"` // stdout, stderr, etc.
	Delay   int    `json:"Delay"`
}

type PlaygroundResponse struct {
	Errors      string            `json:"Errors"`
	Events      []PlaygroundEvent `json:"Events"`
	Status      int               `json:"Status"`
	IsTest      bool              `json:"IsTest"`
	TestsFailed int               `json:"TestsFailed"`
}

func executeGoCode(code string) (string, error) {
	reqBody := playgroundRequest{
		Version: 2,
		Body:    code,
	}
	data, _ := json.Marshal(reqBody)

	resp, err := http.Post("https://play.golang.org/compile", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result PlaygroundResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.Errors != "" {
		return result.Errors, nil
	}

	output := ""
	for _, e := range result.Events {
		if e.Kind == "stdout" {
			output += e.Message
		}
	}
	return output, nil
}

func main() {
	cfg := loadConfig()

	// Init Telegram bot
	bot, err := gotgbot.NewBot(cfg.TelegramToken, nil)
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	// Set up updater
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	dispatcher.AddHandler(
		handlers.NewMessage(message.Text, func(b *gotgbot.Bot, ctx *ext.Context) error {
			chat := ctx.EffectiveChat
			code := ctx.Message.Text
			if strings.HasPrefix(code, "/run") {
				code = strings.TrimPrefix(code, "/run")
				output, err := executeGoCode(code)
				if err != nil {
					_, err := bot.SendMessage(chat.Id, fmt.Sprintf("Error: %v", err), nil)
					return err
				}
				_, err = bot.SendMessage(chat.Id, output, nil)
				return err
			}
			_, err = bot.SendMessage(chat.Id, "/run <your_code>", nil)
			return err
		}),
	)

	updater := ext.NewUpdater(dispatcher, nil)
	// Start polling
	err = updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to start polling: %v", err)
	}
	log.Printf("Bot started as @%s", bot.User.Username)

	// Block forever
	updater.Idle()
}
